package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed embedded-skills
var installableSkillsFS embed.FS

const (
	installableRoot = "embedded-skills"
	skillName       = "xpc"
)

func runSkill(args []string) int {
	if len(args) == 0 {
		printSkillUsage()
		return 1
	}
	switch args[0] {
	case "install":
		return runSkillInstall(args[1:])
	case "help", "--help", "-h":
		printSkillUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown skill subcommand: %s\n\n", args[0])
		printSkillUsage()
		return 1
	}
}

func runSkillInstall(args []string) int {
	var force bool
	var target string
	for _, a := range args {
		switch {
		case a == "--force" || a == "-f":
			force = true
		case a == "--help" || a == "-h":
			printSkillInstallUsage()
			return 0
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", a)
			printSkillInstallUsage()
			return 1
		default:
			if target != "" {
				fmt.Fprintln(os.Stderr, "too many positional args")
				printSkillInstallUsage()
				return 1
			}
			target = a
		}
	}
	if target == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolve cwd: %v\n", err)
			return 1
		}
		target = cwd
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve target: %v\n", err)
		return 1
	}
	if info, err := os.Stat(abs); err != nil {
		fmt.Fprintf(os.Stderr, "target %s: %v\n", abs, err)
		return 1
	} else if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "target %s is not a directory\n", abs)
		return 1
	}

	canonicalDir, symlinkPath, err := installSkill(abs, skillName, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "install: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "✓ skill canonical: %s\n", canonicalDir)
	fmt.Fprintf(os.Stderr, "✓ claude symlink:  %s -> ../../.agents/skills/%s\n", symlinkPath, skillName)
	return 0
}

// installSkill writes the embedded skill into <target>/.agents/skills/<name>/
// and creates <target>/.claude/skills/<name> as a symlink pointing at it.
// Re-running is idempotent: files are overwritten with embedded content; the
// symlink is left alone if it already points correctly. Returns the canonical
// path and symlink path on success.
func installSkill(target, name string, force bool) (string, string, error) {
	srcRoot := installableRoot + "/" + name
	if _, err := fs.Stat(installableSkillsFS, srcRoot); err != nil {
		return "", "", fmt.Errorf("embedded skill %q not found", name)
	}
	canonicalDir := filepath.Join(target, ".agents", "skills", name)
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create canonical dir: %w", err)
	}
	walkErr := fs.WalkDir(installableSkillsFS, srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, srcRoot+"/")
		dst := filepath.Join(canonicalDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := installableSkillsFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if walkErr != nil {
		return "", "", fmt.Errorf("write skill files: %w", walkErr)
	}

	claudeDir := filepath.Join(target, ".claude", "skills")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return canonicalDir, "", fmt.Errorf("create claude dir: %w", err)
	}
	symlinkPath := filepath.Join(claudeDir, name)
	relTarget := filepath.Join("..", "..", ".agents", "skills", name)

	if existing, err := os.Readlink(symlinkPath); err == nil {
		if existing == relTarget {
			return canonicalDir, symlinkPath, nil
		}
		if !force {
			return canonicalDir, symlinkPath, fmt.Errorf("%s is a symlink pointing elsewhere (%s); rerun with --force to replace", symlinkPath, existing)
		}
		if err := os.Remove(symlinkPath); err != nil {
			return canonicalDir, symlinkPath, fmt.Errorf("remove stale symlink: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrInvalid) {
		// Readlink returned an unexpected error; check if a non-symlink lives there.
		if _, statErr := os.Lstat(symlinkPath); statErr == nil {
			if !force {
				return canonicalDir, symlinkPath, fmt.Errorf("%s exists and is not a symlink; rerun with --force to replace", symlinkPath)
			}
			if err := os.Remove(symlinkPath); err != nil {
				return canonicalDir, symlinkPath, fmt.Errorf("remove non-symlink: %w", err)
			}
		}
	} else if _, statErr := os.Lstat(symlinkPath); statErr == nil {
		// Path exists but isn't a symlink (Readlink hit ErrNotExist/ErrInvalid).
		if !force {
			return canonicalDir, symlinkPath, fmt.Errorf("%s exists and is not a symlink; rerun with --force to replace", symlinkPath)
		}
		if err := os.Remove(symlinkPath); err != nil {
			return canonicalDir, symlinkPath, fmt.Errorf("remove non-symlink: %w", err)
		}
	}

	if err := os.Symlink(relTarget, symlinkPath); err != nil {
		return canonicalDir, symlinkPath, fmt.Errorf("create symlink: %w", err)
	}
	return canonicalDir, symlinkPath, nil
}

func printSkillUsage() {
	fmt.Fprintf(os.Stderr, `xpc skill — install agent skills bundled with the binary

Usage:
  xpc skill install [target]   Install the xpc skill into <target>/ (default: cwd)

Layout:
  <target>/.agents/skills/xpc/      canonical install location
  <target>/.claude/skills/xpc       symlink -> ../../.agents/skills/xpc

Re-running is idempotent. Use --force to replace a divergent symlink or
non-symlink at .claude/skills/xpc.
`)
}

func printSkillInstallUsage() {
	fmt.Fprintf(os.Stderr, `xpc skill install [flags] [target]

Flags:
  --force, -f   Replace a non-symlink or wrong-target symlink at .claude/skills/xpc
  --help, -h    Show this help

If [target] is omitted, the current working directory is used.
`)
}
