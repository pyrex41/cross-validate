// Package waiver implements xpc's accepted-risk register: the
// .xpc-waivers.yaml file that lets a team mark specific findings as
// knowingly-accepted so they drop out of the failing set while everything
// else stays enforced.
//
// A waiver matches a finding on (rule, file, kind, name); `reason` and a
// mandatory `expires_at` keep the register honest — an expired waiver stops
// suppressing (the finding re-fires) and emits a warning, so accepted-risk
// never silently becomes permanent. The format is the one documented in the
// embedded xpc skill (.xpc-waivers.yaml).
package waiver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// CanonicalFileName is the waiver file xpc discovers by upward search.
const CanonicalFileName = ".xpc-waivers.yaml"

// expiryLayout is the date format for added_at / expires_at (date-only).
const expiryLayout = "2006-01-02"

// Diagnostic codes for the meta-findings this package emits.
const (
	CodeWaiverExpired = "XPC.W.waiver-expired"
	CodeWaiverUnused  = "XPC.W.waiver-unused"
)

// Waiver is one accepted-risk entry. A finding is waived when its code equals
// Rule, its source file matches File (when set), and its message contains
// Name / Kind (when set). At least one of File or Name must be present so a
// waiver can never blanket-suppress an entire rule by accident.
type Waiver struct {
	Rule      string `yaml:"rule"`
	File      string `yaml:"file,omitempty"`
	Kind      string `yaml:"kind,omitempty"`
	Name      string `yaml:"name,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
	Reason    string `yaml:"reason"`
	AddedBy   string `yaml:"added_by,omitempty"`
	AddedAt   string `yaml:"added_at,omitempty"`
	ExpiresAt string `yaml:"expires_at"`
}

// Set is the parsed .xpc-waivers.yaml plus the path it came from (for messages).
type Set struct {
	Waivers []Waiver `yaml:"waivers"`
	Source  string   `yaml:"-"`
}

// Result is the outcome of applying a Set to a diagnostic list.
type Result struct {
	// Active is the diagnostics that survive (reported and drive the exit
	// code): every unwaived finding, every finding whose waiver has expired
	// (re-fired), plus the synthetic waiver-expired warnings and
	// waiver-unused infos.
	Active []types.Diagnostic
	// Waived is the diagnostics suppressed by a live waiver. Hidden from the
	// report unless the caller opts in (--show-waived), but always counted.
	Waived []types.Diagnostic

	ExpiredCount int // waivers that matched a finding but have expired
	UnusedCount  int // waivers that matched no finding
}

// Resolve loads waivers from flagPath when set, otherwise by upward search for
// .xpc-waivers.yaml from start. An absent file (no flag, none discovered) is
// not an error: it returns an empty Set.
func Resolve(flagPath, start string) (*Set, error) {
	if flagPath != "" {
		return Load(flagPath)
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return &Set{}, nil
	}
	if p, ok := searchUpward(abs); ok {
		return Load(p)
	}
	return &Set{}, nil
}

// Load reads and parses a specific waiver file.
func Load(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var s Set
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	s.Source = path
	return &s, nil
}

// searchUpward walks from start to the filesystem root looking for
// .xpc-waivers.yaml. Returns the first hit.
func searchUpward(start string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, CanonicalFileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Validate enforces the discipline: every waiver needs a rule, a non-empty
// reason, a parseable expires_at, and at least one of file/name (so it cannot
// silence a whole rule). Returns the first problem found, addressed by index
// so the operator can find the offending entry.
func (s *Set) Validate() error {
	for i, w := range s.Waivers {
		where := fmt.Sprintf("waiver #%d (rule %q)", i+1, w.Rule)
		if strings.TrimSpace(w.Rule) == "" {
			return fmt.Errorf("%s: missing required field `rule`", where)
		}
		if w.File == "" && w.Name == "" {
			return fmt.Errorf("%s: needs at least one of `file` or `name` (a waiver may not blanket-suppress a whole rule)", where)
		}
		if strings.TrimSpace(w.Reason) == "" {
			return fmt.Errorf("%s: missing required field `reason` (explain why this finding is accepted)", where)
		}
		if w.ExpiresAt == "" {
			return fmt.Errorf("%s: missing required field `expires_at` (accepted-risk must have a re-review date)", where)
		}
		if _, err := time.Parse(expiryLayout, w.ExpiresAt); err != nil {
			return fmt.Errorf("%s: expires_at %q is not a YYYY-MM-DD date", where, w.ExpiresAt)
		}
	}
	return nil
}

// Apply partitions diags against the waiver set as of `now`.
func (s *Set) Apply(diags []types.Diagnostic, now time.Time) Result {
	used := make([]bool, len(s.Waivers))
	expiredMatched := make([]bool, len(s.Waivers))

	var res Result
	for _, d := range diags {
		wi := s.matchIndex(d)
		if wi < 0 {
			res.Active = append(res.Active, d)
			continue
		}
		used[wi] = true
		if s.Waivers[wi].expired(now) {
			// Expired waiver does NOT suppress — the finding re-fires.
			res.Active = append(res.Active, d)
			expiredMatched[wi] = true
		} else {
			res.Waived = append(res.Waived, d)
		}
	}

	for i, w := range s.Waivers {
		if expiredMatched[i] {
			res.ExpiredCount++
			res.Active = append(res.Active, types.Diagnostic{
				Code:     CodeWaiverExpired,
				Severity: types.SeverityWarning,
				Message:  fmt.Sprintf("waiver for %s (%s) expired on %s — re-justify or fix the finding", w.Rule, w.identity(), w.ExpiresAt),
				Source:   types.SourceLocation{File: s.Source},
			})
		}
		if !used[i] {
			res.UnusedCount++
			res.Active = append(res.Active, types.Diagnostic{
				Code:     CodeWaiverUnused,
				Severity: types.SeverityInfo,
				Message:  fmt.Sprintf("waiver for %s (%s) matched no finding — remove it from %s", w.Rule, w.identity(), filepath.Base(s.Source)),
				Source:   types.SourceLocation{File: s.Source},
			})
		}
	}
	return res
}

// matchIndex returns the index of the first waiver matching d, or -1.
func (s *Set) matchIndex(d types.Diagnostic) int {
	for i, w := range s.Waivers {
		if w.matches(d) {
			return i
		}
	}
	return -1
}

// matches reports whether this waiver covers the diagnostic. The diagnostic
// carries no structured kind/name (they live in the message), so kind/name are
// matched as substrings of the message — combined with the exact rule and the
// file path this is precise in practice.
func (w Waiver) matches(d types.Diagnostic) bool {
	if d.Code != w.Rule {
		return false
	}
	if !fileMatches(d.Source.File, w.File) {
		return false
	}
	if w.Name != "" && !strings.Contains(d.Message, w.Name) {
		return false
	}
	if w.Kind != "" && !strings.Contains(d.Message, w.Kind) {
		return false
	}
	return true
}

// fileMatches reports whether a diagnostic's source file matches the waiver's
// file constraint. The waiver path is repo-relative (or a bare filename) while
// the diagnostic path is whatever was passed on the command line (often
// absolute), so we match by path suffix / basename. Empty constraint matches.
func fileMatches(diagFile, waiverFile string) bool {
	if waiverFile == "" {
		return true
	}
	d := filepath.ToSlash(filepath.Clean(diagFile))
	w := filepath.ToSlash(filepath.Clean(waiverFile))
	if d == w || strings.HasSuffix(d, "/"+w) {
		return true
	}
	return filepath.Base(d) == w
}

// expired reports whether the waiver's expires_at date has passed as of now.
// The expiry date is inclusive: a waiver expiring 2026-06-01 is valid through
// all of that day.
func (w Waiver) expired(now time.Time) bool {
	exp, err := time.Parse(expiryLayout, w.ExpiresAt)
	if err != nil {
		return true
	}
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return nowDay.After(exp)
}

// identity is a short human label for a waiver in messages.
func (w Waiver) identity() string {
	switch {
	case w.Kind != "" && w.Name != "":
		return w.Kind + "/" + w.Name
	case w.Name != "":
		return w.Name
	default:
		return w.File
	}
}
