package ir

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func TestResolveValueFilePrefix(t *testing.T) {
	cwd := "/tmp/app"
	siblings := []types.ArgoSource{
		{RepoURL: "https://example.com/repo.git", Path: "./values-repo", Ref: "values"},
	}

	cases := []struct {
		name    string
		in      string
		want    string
		outcome valueRefOutcome
	}{
		{
			name:    "plain relative — no change",
			in:      "values/override.yaml",
			want:    "values/override.yaml",
			outcome: valueRefNoChange,
		},
		{
			name:    "resolved",
			in:      "$values/deploy/override.yaml",
			want:    "/tmp/app/values-repo/deploy/override.yaml",
			outcome: valueRefResolved,
		},
		{
			name:    "unknown ref",
			in:      "$other/x.yaml",
			want:    "$other/x.yaml",
			outcome: valueRefUnknownRef,
		},
		{
			name:    "lone $ — not a ref prefix",
			in:      "$",
			want:    "$",
			outcome: valueRefNoChange,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, outcome := resolveValueFilePrefix(tc.in, siblings, cwd)
			if outcome != tc.outcome {
				t.Errorf("outcome = %d, want %d", outcome, tc.outcome)
			}
			if outcome == valueRefResolved {
				// Compare on absolutized form, matching the resolver's
				// behavior.
				wantAbs, _ := filepath.Abs(tc.want)
				if got != wantAbs {
					t.Errorf("got %q, want %q", got, wantAbs)
				}
			} else if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveValueFilePrefix_RemoteSibling covers the case where a sibling
// has `ref:` set but no local Path — the resolver needs a `.git` ancestor
// to anchor to, and surfaces `valueRefRemote` when none exists.
func TestResolveValueFilePrefix_RemoteSibling(t *testing.T) {
	siblings := []types.ArgoSource{
		{RepoURL: "https://example.com/repo.git", Ref: "values"},
	}
	// /tmp won't have a .git ancestor on CI runners.
	got, outcome := resolveValueFilePrefix("$values/x.yaml", siblings, "/tmp")
	if outcome != valueRefRemote {
		t.Fatalf("outcome = %d, want valueRefRemote", outcome)
	}
	if got != "$values/x.yaml" {
		t.Errorf("expected unmodified input, got %q", got)
	}
}

// TestRewriteHelmValueFiles_CloneSemantics asserts that the helper returns
// a distinct *ArgoHelmSource when any file got rewritten, so the input
// (which may be shared across AppSet-expanded Applications) is never
// mutated.
func TestRewriteHelmValueFiles_CloneSemantics(t *testing.T) {
	in := &types.ArgoHelmSource{
		ValueFiles: []string{"$values/a.yaml", "$values/b.yaml"},
		Values:     "set: original",
	}
	siblings := []types.ArgoSource{
		{Path: "./values-repo", Ref: "values"},
	}
	out, outcomes := rewriteHelmValueFiles(in, siblings, "/tmp/app")
	if out == in {
		t.Fatal("expected a fresh *ArgoHelmSource, got the input pointer")
	}
	if len(outcomes) != 2 || outcomes[0] != valueRefResolved || outcomes[1] != valueRefResolved {
		t.Errorf("unexpected outcomes: %+v", outcomes)
	}
	if strings.HasPrefix(out.ValueFiles[0], "$") {
		t.Errorf("first value file not rewritten: %q", out.ValueFiles[0])
	}
	if in.ValueFiles[0] != "$values/a.yaml" {
		t.Errorf("input was mutated: %q", in.ValueFiles[0])
	}
}

// TestRewriteHelmValueFiles_NoChange returns the input pointer untouched
// when no file had a $<ref>/ prefix.
func TestRewriteHelmValueFiles_NoChange(t *testing.T) {
	in := &types.ArgoHelmSource{ValueFiles: []string{"values.yaml", "prod.yaml"}}
	out, outcomes := rewriteHelmValueFiles(in, nil, "/tmp/app")
	if out != in {
		t.Error("no-op rewrite should return input pointer")
	}
	for i, o := range outcomes {
		if o != valueRefNoChange {
			t.Errorf("outcome[%d] = %d, want valueRefNoChange", i, o)
		}
	}
}
