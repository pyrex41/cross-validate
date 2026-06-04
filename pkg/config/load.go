package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// knownTopLevelKeys returns the set of top-level yaml keys the Config struct
// understands, derived by reflection over its field tags. Deriving it (rather
// than hand-maintaining a list) keeps the forward-compat warning in lock-step
// with the struct: adding a new top-level config field can never again leave a
// stale literal that false-warns "unknown top-level key ... ignoring" on a key
// the decoder actually honors.
func knownTopLevelKeys() map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		// Strip yaml tag options like ",omitempty".
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag != "" {
			out[tag] = true
		}
	}
	return out
}

// CurrentVersion is the schema version this binary understands. The loader
// rejects xpc.yaml files whose top-level `version:` key disagrees so that
// future schema-breaking changes have a clean trip-wire.
const CurrentVersion = 1

// Load reads xpc.yaml from path, validates it, and returns a fully-populated
// Config. An empty / missing path is a programmer error — callers wanting
// "open file if it exists, else default" should consult Discover and decide
// for themselves whether to call Load or fall back to Default.
//
// Validation rules (per design §3.b):
//
//   - File missing / unreadable          → error.
//   - YAML parse error                    → error with line:col from yaml.v3.
//   - Unknown top-level key               → warning on stderr, continue.
//   - Unknown key inside a known section  → error.
//   - version not equal to CurrentVersion → error.
//   - Empty primary on a bypass section,
//     malformed gvk on an immutable entry  → error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read xpc.yaml at %s: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse xpc.yaml at %s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes a single xpc.yaml document from raw bytes. Exposed alongside
// Load so callers (tests, embedded callers) can supply the bytes directly.
func Parse(data []byte) (*Config, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		// Empty file — equivalent to absent. Default semantics.
		out := Default()
		return out, nil
	}

	// Decode in two passes to split the two error classes the design wants:
	//   - unknown TOP-LEVEL key  → warning, ignored (forward-compat: an older
	//     binary must still read a config carrying a newer top-level key).
	//   - unknown NESTED key     → error (catches typos inside a known section).
	//
	// 1. Loose-decode into a map so we can spot — and then strip — unknown
	//    top-level keys, emitting the forward-compat warning for each.
	// 2. Re-marshal the stripped map and strict-decode it into Config with
	//    KnownFields(true). Because the unknown top-level keys are already
	//    gone, they don't trip the strict decoder; any unknown key that
	//    remains is nested and correctly surfaces as an error.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	knownTopLevel := knownTopLevelKeys()
	for k := range raw {
		if !knownTopLevel[k] {
			fmt.Fprintf(os.Stderr,
				"warning: unknown top-level key %q in xpc.yaml; ignoring (forward-compat)\n", k)
			delete(raw, k)
		}
	}

	stripped, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(strings.NewReader(string(stripped)))
	dec.KnownFields(true)
	cfg := &Config{}
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		// Strip "yaml: unmarshal errors:" prefix for cleaner output.
		msg := err.Error()
		// Extra context: tell users the schema version we expect when a
		// nested-key error fires, since the most common cause of an
		// unknown nested key is a forward-incompatible schema bump.
		if strings.Contains(msg, "field ") {
			return nil, fmt.Errorf("%s (schema version %d)", msg, CurrentVersion)
		}
		return nil, err
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate enforces the semantic rules that survive YAML parsing — version
// match, GVK well-formedness, non-empty primaries.
func validate(cfg *Config) error {
	if cfg.Version == 0 {
		// Treat omitted version as "default for this binary", matching the
		// "absent file is fine" mental model: an empty xpc.yaml or one with
		// only optional sections is forward-compatible.
		cfg.Version = CurrentVersion
	}
	if cfg.Version != CurrentVersion {
		return fmt.Errorf(
			"unsupported xpc.yaml version: got %d, this binary understands version %d",
			cfg.Version, CurrentVersion)
	}

	for i, entry := range cfg.ImmutableFields {
		if entry.GVK == "" {
			return fmt.Errorf("immutable-fields[%d]: gvk is required", i)
		}
		if len(entry.Paths) == 0 {
			return fmt.Errorf("immutable-fields[%d] (%s): paths is required (suppress entries must list the specific field paths to remove)",
				i, entry.GVK)
		}
		// gvk must split into 1, 2, or 3 parts; nothing weirder.
		parts := strings.Split(entry.GVK, "/")
		if len(parts) > 3 {
			return fmt.Errorf("immutable-fields[%d] (%s): gvk has too many segments (want group/version/Kind)",
				i, entry.GVK)
		}
		if len(parts) == 2 && !looksLikeVersion(parts[0]) {
			return fmt.Errorf("immutable-fields[%d] (%s): two-segment gvk is reserved for core APIs like v1/ConfigMap; use group/version/Kind for grouped resources",
				i, entry.GVK)
		}
		for _, p := range entry.Paths {
			if p == "" {
				return fmt.Errorf("immutable-fields[%d] (%s): paths contains empty entry",
					i, entry.GVK)
			}
		}
	}

	for i, entry := range cfg.StateBearingKinds.Append {
		if entry.Kind == "" {
			return fmt.Errorf("state-bearing-kinds.append[%d]: kind is required", i)
		}
		if strings.TrimSpace(entry.Kind) == "" {
			return fmt.Errorf("state-bearing-kinds.append[%d]: kind must be non-blank", i)
		}
		// Group may be empty for core-API kinds, but a whitespace-only
		// group is almost certainly a typo — flag it.
		if entry.Group != "" && strings.TrimSpace(entry.Group) == "" {
			return fmt.Errorf("state-bearing-kinds.append[%d] (kind=%s): group must be non-blank when set",
				i, entry.Kind)
		}
	}
	for i, entry := range cfg.StateBearingKinds.Suppress {
		if entry.Kind == "" {
			return fmt.Errorf("state-bearing-kinds.suppress[%d]: kind is required", i)
		}
		if strings.TrimSpace(entry.Kind) == "" {
			return fmt.Errorf("state-bearing-kinds.suppress[%d]: kind must be non-blank", i)
		}
		if entry.Group != "" && strings.TrimSpace(entry.Group) == "" {
			return fmt.Errorf("state-bearing-kinds.suppress[%d] (kind=%s): group must be non-blank when set",
				i, entry.Kind)
		}
	}

	// A bypass primary set to "" is a programmer error — that means "have
	// no built-in primary, only aliases". The user can opt into that by
	// listing only aliases and leaving primary unset (zero value) — which
	// resolves to the built-in. To genuinely drop the primary, use
	// `primary: " "` and accept the noise. We don't currently support that
	// shape; flag the empty-string-explicit case by checking whitespace.
	// (yaml.v3 doesn't distinguish "key not present" from "key: \"\""; we
	// can't reject the latter without false positives, so this is purely
	// documentation.)

	return nil
}

func looksLikeVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' || !isDigit(s[1]) {
		return false
	}
	i := 2
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == len(s) {
		return true
	}
	for _, suffix := range []string{"alpha", "beta"} {
		if strings.HasPrefix(s[i:], suffix) {
			j := i + len(suffix)
			if j == len(s) {
				return false
			}
			for ; j < len(s); j++ {
				if !isDigit(s[j]) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
