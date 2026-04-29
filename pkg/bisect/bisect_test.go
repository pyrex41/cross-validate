package bisect

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRuleCode(t *testing.T) {
	cases := []struct {
		code    string
		wantErr bool
	}{
		{"XPC002", false},
		{"XPC123", false},
		{"XPC.A.resource-field-valid", false},
		{"XPC.E.ssa-managementpolicies-strict", false},
		{"XPC.H.helm-renders", false},
		{"", true},
		{"XPC.foo", true},        // missing category letter
		{"XPC2", true},           // too few digits
		{"xpc002", true},         // wrong case
		{"XPC.a.foo", true},      // lowercase category
		{"FOO.A.bar", true},      // wrong prefix
		{"XPC.A.", true},         // empty name
		{"XPC.A.foo bar", true},  // space
	}
	for _, tc := range cases {
		err := ValidateRuleCode(tc.code)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateRuleCode(%q): expected error, got nil", tc.code)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateRuleCode(%q): unexpected error: %v", tc.code, err)
		}
	}
}

func TestScanDiagnostics(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		ruleCode string
		want     bool
		wantErr  bool
	}{
		{"empty array, rule absent", "[]", "XPC002", false, false},
		{"hit by code", `[{"code":"XPC002","severity":"error","message":"x"}]`, "XPC002", true, false},
		{"miss by code", `[{"code":"XPC003","severity":"error","message":"x"}]`, "XPC002", false, false},
		{
			name:     "hit among many",
			body:     `[{"code":"XPC.A.x"},{"code":"XPC.B.y"},{"code":"XPC.A.target"},{"code":"XPC.C.z"}]`,
			ruleCode: "XPC.A.target",
			want:     true,
		},
		{"empty bytes", "", "XPC002", false, false},
		{"malformed JSON", "not json", "XPC002", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ScanDiagnostics([]byte(tc.body), tc.ruleCode)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got fires=%v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunBisect_SyntheticHistory drives the full Run() loop end-to-end against
// a real (but synthetic) git repository. We construct a chain of N commits
// where commits 0..k-1 do NOT have a marker file and commits k..N-1 DO have
// it. The CheckRule stub fires iff the marker file is present at the
// materialized worktree, so the bisect should pinpoint commit k as the
// first-firing commit.
func TestRunBisect_SyntheticHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	repo := t.TempDir()
	gitBin := "git"

	mustGit(t, gitBin, repo, "init", "--quiet", "-b", "main")
	mustGit(t, gitBin, repo, "config", "user.email", "test@example.com")
	mustGit(t, gitBin, repo, "config", "user.name", "Test")
	mustGit(t, gitBin, repo, "config", "commit.gpgsign", "false")

	// Commit 0: empty. We treat this as the "good" boundary. The marker file
	// is "rule.yaml" — its presence triggers the synthetic rule.
	writeFile(t, filepath.Join(repo, "README.md"), "init\n")
	mustGit(t, gitBin, repo, "add", "README.md")
	mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "init")

	// Pre-transition commits (no marker).
	for i := 1; i < 4; i++ {
		writeFile(t, filepath.Join(repo, "padding.txt"), strings.Repeat("x", i))
		mustGit(t, gitBin, repo, "add", "padding.txt")
		mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "pad "+itoa(i))
	}

	// The transition commit — marker file appears here.
	writeFile(t, filepath.Join(repo, "rule.yaml"), "trigger: true\n")
	mustGit(t, gitBin, repo, "add", "rule.yaml")
	mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "introduce rule trigger")
	transitionSHA := mustGitOutput(t, gitBin, repo, "rev-parse", "HEAD")

	// Post-transition commits (marker still present).
	for i := 5; i < 8; i++ {
		writeFile(t, filepath.Join(repo, "padding.txt"), strings.Repeat("y", i))
		mustGit(t, gitBin, repo, "add", "padding.txt")
		mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "post "+itoa(i))
	}

	goodSHA := mustGitOutput(t, gitBin, repo, "rev-parse", "main~7") // 8 commits total -> ~7 = first
	badSHA := mustGitOutput(t, gitBin, repo, "rev-parse", "HEAD")

	// Synthetic rule: fires iff rule.yaml exists in the worktree.
	checkRule := func(workdir string) (bool, error) {
		_, err := os.Stat(filepath.Join(workdir, "rule.yaml"))
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	res, err := Run(Options{
		RuleCode:  "XPC.X.synthetic",
		GoodRef:   goodSHA,
		BadRef:    badSHA,
		RepoRoot:  repo,
		CheckRule: checkRule,
		GitBin:    gitBin,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.Commit != transitionSHA {
		t.Errorf("transition commit: got %s, want %s", res.Commit, transitionSHA)
	}
	if res.Direction != StartedFiring {
		t.Errorf("direction: got %s, want %s", res.Direction, StartedFiring)
	}
	if res.Subject != "introduce rule trigger" {
		t.Errorf("subject: got %q, want %q", res.Subject, "introduce rule trigger")
	}
	if res.Steps < 1 {
		t.Errorf("expected at least 1 bisect step, got %d", res.Steps)
	}
}

// TestRunBisect_RuleNeverChanged covers the case where the rule fires (or
// doesn't fire) at both boundaries. We expect ErrRuleNeverChanged.
func TestRunBisect_RuleNeverChanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	gitBin := "git"
	mustGit(t, gitBin, repo, "init", "--quiet", "-b", "main")
	mustGit(t, gitBin, repo, "config", "user.email", "test@example.com")
	mustGit(t, gitBin, repo, "config", "user.name", "Test")
	mustGit(t, gitBin, repo, "config", "commit.gpgsign", "false")

	writeFile(t, filepath.Join(repo, "a.txt"), "1\n")
	mustGit(t, gitBin, repo, "add", "a.txt")
	mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "c1")
	first := mustGitOutput(t, gitBin, repo, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(repo, "a.txt"), "2\n")
	mustGit(t, gitBin, repo, "add", "a.txt")
	mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "c2")
	second := mustGitOutput(t, gitBin, repo, "rev-parse", "HEAD")

	// Rule never fires at any commit.
	neverFires := func(string) (bool, error) { return false, nil }

	_, err := Run(Options{
		RuleCode:  "XPC002",
		GoodRef:   first,
		BadRef:    second,
		RepoRoot:  repo,
		CheckRule: neverFires,
		GitBin:    gitBin,
	})
	if !errors.Is(err, ErrRuleNeverChanged) {
		t.Fatalf("expected ErrRuleNeverChanged, got %v", err)
	}
}

// TestRunBisect_SameCommit covers --good == --bad.
func TestRunBisect_SameCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	gitBin := "git"
	mustGit(t, gitBin, repo, "init", "--quiet", "-b", "main")
	mustGit(t, gitBin, repo, "config", "user.email", "test@example.com")
	mustGit(t, gitBin, repo, "config", "user.name", "Test")
	mustGit(t, gitBin, repo, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(repo, "a.txt"), "1\n")
	mustGit(t, gitBin, repo, "add", "a.txt")
	mustGit(t, gitBin, repo, "commit", "--quiet", "-m", "c1")
	sha := mustGitOutput(t, gitBin, repo, "rev-parse", "HEAD")

	_, err := Run(Options{
		RuleCode:  "XPC002",
		GoodRef:   sha,
		BadRef:    sha,
		RepoRoot:  repo,
		CheckRule: func(string) (bool, error) { return false, nil },
		GitBin:    gitBin,
	})
	if !errors.Is(err, ErrSameCommit) {
		t.Fatalf("expected ErrSameCommit, got %v", err)
	}
}

// TestRunBisect_MalformedRuleCode covers the upfront validation.
func TestRunBisect_MalformedRuleCode(t *testing.T) {
	_, err := Run(Options{
		RuleCode:  "XPC.foo", // missing category letter
		GoodRef:   "irrelevant",
		BadRef:    "irrelevant",
		RepoRoot:  t.TempDir(),
		CheckRule: func(string) (bool, error) { return false, nil },
	})
	if err == nil {
		t.Fatal("expected validation error for malformed rule code")
	}
	if !strings.Contains(err.Error(), "malformed rule code") {
		t.Errorf("expected 'malformed rule code' in error, got: %v", err)
	}
}

// --- test helpers ---

func mustGit(t *testing.T, gitBin, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitBin, append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustGitOutput(t *testing.T, gitBin, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBin, append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func itoa(i int) string {
	// avoid pulling in strconv for one call site
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	idx := len(buf)
	for i > 0 {
		idx--
		buf[idx] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		idx--
		buf[idx] = '-'
	}
	return string(buf[idx:])
}
