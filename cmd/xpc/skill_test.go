package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkill_FreshTarget(t *testing.T) {
	target := t.TempDir()

	canonical, symlink, err := installSkill(target, skillName, false)
	if err != nil {
		t.Fatalf("installSkill: %v", err)
	}

	wantCanonical := filepath.Join(target, ".agents", "skills", skillName)
	if canonical != wantCanonical {
		t.Errorf("canonical = %q, want %q", canonical, wantCanonical)
	}
	wantSymlink := filepath.Join(target, ".claude", "skills", skillName)
	if symlink != wantSymlink {
		t.Errorf("symlink = %q, want %q", symlink, wantSymlink)
	}

	skillPath := filepath.Join(canonical, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read installed SKILL.md: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\nname: xpc\n") {
		t.Errorf("SKILL.md does not start with expected frontmatter; got prefix: %q", string(data[:min(80, len(data))]))
	}

	got, err := os.Readlink(symlink)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	want := filepath.Join("..", "..", ".agents", "skills", skillName)
	if got != want {
		t.Errorf("symlink target = %q, want %q", got, want)
	}

	through := filepath.Join(symlink, "SKILL.md")
	if _, err := os.Stat(through); err != nil {
		t.Errorf("SKILL.md not reachable via symlink: %v", err)
	}
}

func TestInstallSkill_Idempotent(t *testing.T) {
	target := t.TempDir()

	for i := 0; i < 3; i++ {
		if _, _, err := installSkill(target, skillName, false); err != nil {
			t.Fatalf("install attempt %d: %v", i, err)
		}
	}
}

func TestInstallSkill_DivergentSymlinkRequiresForce(t *testing.T) {
	target := t.TempDir()
	claudeDir := filepath.Join(target, ".claude", "skills")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(claudeDir, skillName)
	if err := os.Symlink("/somewhere/else", symlinkPath); err != nil {
		t.Fatal(err)
	}

	if _, _, err := installSkill(target, skillName, false); err == nil {
		t.Fatal("expected error on divergent symlink without --force, got nil")
	}

	if _, _, err := installSkill(target, skillName, true); err != nil {
		t.Fatalf("install with --force should succeed, got: %v", err)
	}

	got, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("readlink after force: %v", err)
	}
	want := filepath.Join("..", "..", ".agents", "skills", skillName)
	if got != want {
		t.Errorf("symlink target after force = %q, want %q", got, want)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
