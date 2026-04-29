package plan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CommentTarget identifies a single GitLab MR or GitHub PR that the plan
// delta should be posted to.
type CommentTarget struct {
	VCS     string // "gitlab" or "github"
	Project string // "group/project" or "owner/repo" (path may contain nested groups for GitLab)
	MRorPR  int
}

// Sentinel errors for the comment-posting subsystem. Callers use errors.Is
// to discriminate between user-input problems, missing CLIs, subprocess
// failures, and CI auto-detection misses.
var (
	ErrCommentSpec  = errors.New("plan: invalid --post-comment spec")
	ErrPosterAbsent = errors.New("plan: required CLI not on PATH")
	ErrPostFailed   = errors.New("plan: post-comment subprocess failed")
	ErrAutoDetect   = errors.New("plan: --post-comment=auto could not infer target from CI env")
)

// githubPullSpec matches "<owner>/<repo>/pull/<n>" with non-empty,
// slash-free owner and repo segments.
var githubPullSpec = regexp.MustCompile(`^([^/]+)/([^/]+)/pull/(\d+)$`)

// githubRefPR matches the GITHUB_REF GitHub Actions sets on pull-request
// events: "refs/pull/<n>/merge".
var githubRefPR = regexp.MustCompile(`^refs/pull/(\d+)/merge$`)

// ParseCommentSpec turns a --post-comment value into a CommentTarget.
//
// Three forms are accepted:
//
//   - gitlab://<group/path>/-/merge_requests/<n>
//   - github://<owner>/<repo>/pull/<n>
//   - auto (read CI env vars: GitLab CI_PROJECT_PATH+CI_MERGE_REQUEST_IID,
//     GitHub GITHUB_REPOSITORY+GITHUB_REF=refs/pull/<n>/merge)
func ParseCommentSpec(spec string) (CommentTarget, error) {
	switch {
	case strings.HasPrefix(spec, "gitlab://"):
		rest := strings.TrimPrefix(spec, "gitlab://")
		const sep = "/-/merge_requests/"
		idx := strings.Index(rest, sep)
		if idx < 0 {
			return CommentTarget{}, fmt.Errorf("%w: %q", ErrCommentSpec, spec)
		}
		project := rest[:idx]
		mrStr := rest[idx+len(sep):]
		if project == "" || !strings.Contains(project, "/") {
			return CommentTarget{}, fmt.Errorf("%w: %q", ErrCommentSpec, spec)
		}
		mr, err := strconv.Atoi(mrStr)
		if err != nil {
			return CommentTarget{}, fmt.Errorf("%w: %q", ErrCommentSpec, spec)
		}
		return CommentTarget{VCS: "gitlab", Project: project, MRorPR: mr}, nil

	case strings.HasPrefix(spec, "github://"):
		rest := strings.TrimPrefix(spec, "github://")
		m := githubPullSpec.FindStringSubmatch(rest)
		if m == nil {
			return CommentTarget{}, fmt.Errorf("%w: %q", ErrCommentSpec, spec)
		}
		pr, err := strconv.Atoi(m[3])
		if err != nil {
			// regex already guarantees \d+, but be defensive.
			return CommentTarget{}, fmt.Errorf("%w: %q", ErrCommentSpec, spec)
		}
		return CommentTarget{VCS: "github", Project: m[1] + "/" + m[2], MRorPR: pr}, nil

	case spec == "auto":
		return inferAutoTarget()

	default:
		return CommentTarget{}, fmt.Errorf("%w: %q", ErrCommentSpec, spec)
	}
}

// inferAutoTarget reads CI env vars to deduce the target. GitLab is checked
// first (CI_PROJECT_PATH + CI_MERGE_REQUEST_IID); on miss, fall through to
// GitHub Actions (GITHUB_REPOSITORY + GITHUB_REF).
func inferAutoTarget() (CommentTarget, error) {
	glProject := os.Getenv("CI_PROJECT_PATH")
	glIID := os.Getenv("CI_MERGE_REQUEST_IID")
	if glProject != "" && glIID != "" {
		iid, err := strconv.Atoi(glIID)
		if err != nil {
			return CommentTarget{}, fmt.Errorf(
				"%w: CI_MERGE_REQUEST_IID=%q is not numeric",
				ErrAutoDetect, truncate(glIID, 64),
			)
		}
		return CommentTarget{VCS: "gitlab", Project: glProject, MRorPR: iid}, nil
	}

	ghRepo := os.Getenv("GITHUB_REPOSITORY")
	ghRef := os.Getenv("GITHUB_REF")
	if ghRepo != "" {
		if m := githubRefPR.FindStringSubmatch(ghRef); m != nil {
			pr, err := strconv.Atoi(m[1])
			if err == nil {
				return CommentTarget{VCS: "github", Project: ghRepo, MRorPR: pr}, nil
			}
		}
	}

	return CommentTarget{}, fmt.Errorf(
		"%w: CI_PROJECT_PATH=%q CI_MERGE_REQUEST_IID=%q GITHUB_REPOSITORY=%q GITHUB_REF=%q",
		ErrAutoDetect,
		truncate(glProject, 64), truncate(glIID, 64),
		truncate(ghRepo, 64), truncate(ghRef, 64),
	)
}

// PostComment publishes body as a comment on the target MR/PR by shelling
// out to glab (GitLab) or gh (GitHub). The body is piped through stdin via
// --message-file=- / --body-file=- so that arbitrary Markdown content
// (including newlines and quotes) survives the boundary unchanged. The
// subprocess inherits the parent process's environment so that whichever
// CLI is invoked sees its own auth tokens (e.g. GITLAB_TOKEN, CI_JOB_TOKEN,
// GH_TOKEN, GITHUB_TOKEN); xpc itself never reads or stores those tokens.
func PostComment(target CommentTarget, body string) error {
	switch target.VCS {
	case "gitlab":
		bin, err := exec.LookPath("glab")
		if err != nil {
			return fmt.Errorf("%w: glab: %v", ErrPosterAbsent, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, bin,
			"mr", "note", strconv.Itoa(target.MRorPR),
			"--repo="+target.Project,
			"--message-file=-",
		)
		cmd.Stdin = strings.NewReader(body)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%w: glab mr note: %v: %s",
				ErrPostFailed, err, tailStderr(&stderr))
		}
		return nil

	case "github":
		bin, err := exec.LookPath("gh")
		if err != nil {
			return fmt.Errorf("%w: gh: %v", ErrPosterAbsent, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, bin,
			"pr", "comment", strconv.Itoa(target.MRorPR),
			"--repo="+target.Project,
			"--body-file=-",
		)
		cmd.Stdin = strings.NewReader(body)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%w: gh pr comment: %v: %s",
				ErrPostFailed, err, tailStderr(&stderr))
		}
		return nil

	default:
		return fmt.Errorf("%w: unknown VCS %q", ErrCommentSpec, target.VCS)
	}
}

// DescribeTarget returns a short human-readable label for stderr/dry-run
// logs.
func DescribeTarget(t CommentTarget) string {
	switch t.VCS {
	case "gitlab":
		return fmt.Sprintf("gitlab MR %s!%d", t.Project, t.MRorPR)
	case "github":
		return fmt.Sprintf("github PR %s#%d", t.Project, t.MRorPR)
	default:
		return fmt.Sprintf("%s %s/%d", t.VCS, t.Project, t.MRorPR)
	}
}

// tailStderr returns the last ~512 bytes of buf as a string, with leading
// newlines trimmed. Used to keep error messages from dragging in pages of
// CLI noise while still surfacing the diagnostic the user needs.
func tailStderr(buf *bytes.Buffer) string {
	const max = 512
	b := buf.Bytes()
	if len(b) > max {
		b = b[len(b)-max:]
	}
	return strings.TrimLeft(string(b), "\n\r")
}

// truncate caps s at n runes (best-effort: byte slice; ASCII-safe for env
// var content) for use in error messages so an accidentally giant env var
// doesn't blow up a one-line error string.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
