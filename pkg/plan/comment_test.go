package plan

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// clearCommentEnv zeros the four CI env vars relevant to ParseCommentSpec
// so that tests are hermetic regardless of the host shell environment
// (e.g. when run inside a real GitLab CI / GitHub Actions runner).
func clearCommentEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CI_PROJECT_PATH", "")
	t.Setenv("CI_MERGE_REQUEST_IID", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_REF", "")
}

func TestParseCommentSpec(t *testing.T) {
	type envPair struct{ k, v string }
	tests := []struct {
		name    string
		spec    string
		env     []envPair
		wantErr error // sentinel; nil means success
		want    CommentTarget
	}{
		// --- valid URL forms ---
		{
			name: "gitlab simple",
			spec: "gitlab://acme/proj/-/merge_requests/42",
			want: CommentTarget{VCS: "gitlab", Project: "acme/proj", MRorPR: 42},
		},
		{
			name: "gitlab nested groups",
			spec: "gitlab://acme/team/sub/proj/-/merge_requests/1",
			want: CommentTarget{VCS: "gitlab", Project: "acme/team/sub/proj", MRorPR: 1},
		},
		{
			name: "github simple",
			spec: "github://acme/proj/pull/7",
			want: CommentTarget{VCS: "github", Project: "acme/proj", MRorPR: 7},
		},
		{
			name: "github dashes and big number",
			spec: "github://owner/repo-with-dashes/pull/1234",
			want: CommentTarget{VCS: "github", Project: "owner/repo-with-dashes", MRorPR: 1234},
		},

		// --- auto cases ---
		{
			name: "auto gitlab",
			spec: "auto",
			env: []envPair{
				{"CI_PROJECT_PATH", "acme/proj"},
				{"CI_MERGE_REQUEST_IID", "99"},
			},
			want: CommentTarget{VCS: "gitlab", Project: "acme/proj", MRorPR: 99},
		},
		{
			name: "auto github",
			spec: "auto",
			env: []envPair{
				{"GITHUB_REPOSITORY", "owner/repo"},
				{"GITHUB_REF", "refs/pull/42/merge"},
			},
			want: CommentTarget{VCS: "github", Project: "owner/repo", MRorPR: 42},
		},
		{
			name:    "auto github non-PR ref",
			spec:    "auto",
			env:     []envPair{{"GITHUB_REF", "refs/heads/main"}},
			wantErr: ErrAutoDetect,
		},
		{
			name:    "auto nothing set",
			spec:    "auto",
			wantErr: ErrAutoDetect,
		},

		// --- invalid forms ---
		{
			name:    "gitlab missing dash separator",
			spec:    "gitlab://acme/proj/merge_requests/1",
			wantErr: ErrCommentSpec,
		},
		{
			name:    "gitlab non-numeric MR",
			spec:    "gitlab://acme/proj/-/merge_requests/abc",
			wantErr: ErrCommentSpec,
		},
		{
			name:    "gitlab project missing slash",
			spec:    "gitlab://acme/-/merge_requests/1",
			wantErr: ErrCommentSpec,
		},
		{
			name:    "github missing repo",
			spec:    "github://acme/pull/7",
			wantErr: ErrCommentSpec,
		},
		{
			name:    "github trailing slash",
			spec:    "github://acme/proj/pull/7/",
			wantErr: ErrCommentSpec,
		},
		{
			name:    "unknown scheme",
			spec:    "bitbucket://x/y/pulls/1",
			wantErr: ErrCommentSpec,
		},
		{
			name:    "empty",
			spec:    "",
			wantErr: ErrCommentSpec,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearCommentEnv(t)
			for _, e := range tc.env {
				t.Setenv(e.k, e.v)
			}

			got, err := ParseCommentSpec(tc.spec)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("ParseCommentSpec(%q) = %+v, want error %v", tc.spec, got, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ParseCommentSpec(%q) err = %v, want errors.Is %v", tc.spec, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCommentSpec(%q) unexpected err: %v", tc.spec, err)
			}
			if got != tc.want {
				t.Fatalf("ParseCommentSpec(%q) = %+v, want %+v", tc.spec, got, tc.want)
			}
		})
	}
}

// writeShim drops a POSIX sh script at path that records its argv (one
// line, space-joined) to $CAPTURE_DIR/args and its stdin to
// $CAPTURE_DIR/stdin.
func writeShim(t *testing.T, path string) {
	t.Helper()
	const script = `#!/bin/sh
echo "$@" > "$CAPTURE_DIR/args"
cat > "$CAPTURE_DIR/stdin"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim %s: %v", path, err)
	}
}

func TestPostComment_GitLab_StdinShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell shim not portable to windows")
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	captureDir := filepath.Join(dir, "capture")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir captureDir: %v", err)
	}
	writeShim(t, filepath.Join(binDir, "glab"))

	// Prepend binDir to PATH so the shim wins over any real glab on the
	// host, while keeping /bin et al available for `cat` inside the shim.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_DIR", captureDir)

	body := "## hello\nbody bytes\nwith\tmixed whitespace\n"
	target := CommentTarget{VCS: "gitlab", Project: "acme/proj", MRorPR: 42}
	if err := PostComment(target, body); err != nil {
		t.Fatalf("PostComment: %v", err)
	}

	gotArgs, err := os.ReadFile(filepath.Join(captureDir, "args"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	wantTokens := []string{"mr", "note", "42", "--repo=acme/proj", "--message-file=-"}
	gotArgsStr := string(gotArgs)
	for _, tok := range wantTokens {
		if !strings.Contains(gotArgsStr, tok) {
			t.Errorf("argv %q missing token %q", gotArgsStr, tok)
		}
	}

	gotStdin, err := os.ReadFile(filepath.Join(captureDir, "stdin"))
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	if string(gotStdin) != body {
		t.Errorf("stdin mismatch\n got:  %q\n want: %q", gotStdin, body)
	}
}

func TestPostComment_GitHub_StdinShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell shim not portable to windows")
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	captureDir := filepath.Join(dir, "capture")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir captureDir: %v", err)
	}
	writeShim(t, filepath.Join(binDir, "gh"))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_DIR", captureDir)

	body := "delta body\nline 2\n"
	target := CommentTarget{VCS: "github", Project: "acme/repo", MRorPR: 7}
	if err := PostComment(target, body); err != nil {
		t.Fatalf("PostComment: %v", err)
	}

	gotArgs, err := os.ReadFile(filepath.Join(captureDir, "args"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	wantTokens := []string{"pr", "comment", "7", "--repo=acme/repo", "--body-file=-"}
	gotArgsStr := string(gotArgs)
	for _, tok := range wantTokens {
		if !strings.Contains(gotArgsStr, tok) {
			t.Errorf("argv %q missing token %q", gotArgsStr, tok)
		}
	}

	gotStdin, err := os.ReadFile(filepath.Join(captureDir, "stdin"))
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	if string(gotStdin) != body {
		t.Errorf("stdin mismatch\n got:  %q\n want: %q", gotStdin, body)
	}
}

func TestPostComment_BinaryAbsent(t *testing.T) {
	// Replace PATH with an empty directory so neither glab nor gh can be
	// resolved by exec.LookPath, regardless of what's installed on the
	// host.
	t.Setenv("PATH", t.TempDir())

	err := PostComment(
		CommentTarget{VCS: "gitlab", Project: "x/y", MRorPR: 1},
		"body",
	)
	if !errors.Is(err, ErrPosterAbsent) {
		t.Fatalf("err = %v, want errors.Is ErrPosterAbsent", err)
	}

	err = PostComment(
		CommentTarget{VCS: "github", Project: "x/y", MRorPR: 1},
		"body",
	)
	if !errors.Is(err, ErrPosterAbsent) {
		t.Fatalf("err = %v, want errors.Is ErrPosterAbsent", err)
	}
}

func TestDescribeTarget(t *testing.T) {
	tests := []struct {
		name string
		in   CommentTarget
		want string
	}{
		{
			name: "gitlab",
			in:   CommentTarget{VCS: "gitlab", Project: "acme/proj", MRorPR: 42},
			want: "gitlab MR acme/proj!42",
		},
		{
			name: "github",
			in:   CommentTarget{VCS: "github", Project: "owner/repo", MRorPR: 7},
			want: "github PR owner/repo#7",
		},
		{
			name: "unknown vcs fallback",
			in:   CommentTarget{VCS: "bitbucket", Project: "x/y", MRorPR: 3},
			want: "bitbucket x/y/3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DescribeTarget(tc.in)
			if got != tc.want {
				t.Errorf("DescribeTarget = %q, want %q", got, tc.want)
			}
		})
	}
}
