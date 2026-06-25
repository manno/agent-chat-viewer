package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSkillDirs(t *testing.T) SkillDirs {
	t.Helper()
	root := t.TempDir()
	return SkillDirs{
		Copilot:   filepath.Join(root, "copilot", "skills"),
		Claude:    filepath.Join(root, "claude", "skills"),
		Canonical: filepath.Join(root, "canonical", "agent-skills"),
	}
}

func TestParseSkillFrontmatter_NameAndDescription(t *testing.T) {
	dirs := newSkillDirs(t)
	if err := os.MkdirAll(dirs.Copilot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, dirs.Copilot, "hello", `---
name: hello-world
description: >-
    A multi-line description that
    spans two lines.
---

# body
`)
	skills := findSkillsIn(dirs)
	if len(skills) != 1 {
		t.Fatalf("want 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Name != "hello-world" {
		t.Errorf("name: want hello-world, got %q", s.Name)
	}
	if s.Desc != "A multi-line description that spans two lines." {
		t.Errorf("desc: got %q", s.Desc)
	}
	if s.Agent != "copilot" {
		t.Errorf("agent: want copilot, got %q", s.Agent)
	}
	if s.IsSymlink {
		t.Errorf("should not be a symlink")
	}
}

func TestSyncSkills_InitialMigrationCreatesSymlinks(t *testing.T) {
	dirs := newSkillDirs(t)
	for _, d := range []string{dirs.Copilot, dirs.Claude} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(t, dirs.Copilot, "alpha", "---\nname: alpha\n---\nA")
	writeSkill(t, dirs.Claude, "beta", "---\nname: beta\n---\nB")

	r := syncSkills(dirs)
	if len(r.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", r.Errors)
	}

	// Both skills end up in canonical as real dirs.
	for _, name := range []string{"alpha", "beta"} {
		canon := filepath.Join(dirs.Canonical, name)
		info, err := os.Lstat(canon)
		if err != nil {
			t.Fatalf("canonical/%s missing: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("canonical/%s should be a real dir, not symlink", name)
		}
	}

	// Each agent dir has a symlink for every canonical skill.
	for _, ad := range []string{dirs.Copilot, dirs.Claude} {
		for _, name := range []string{"alpha", "beta"} {
			p := filepath.Join(ad, name)
			info, err := os.Lstat(p)
			if err != nil {
				t.Fatalf("%s missing: %v", p, err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s should be a symlink", p)
			}
		}
	}

	// Re-running sync should be a no-op (no errors, no new moves).
	r2 := syncSkills(dirs)
	if len(r2.Errors) > 0 {
		t.Fatalf("second run errors: %v", r2.Errors)
	}
	if len(r2.Moved) != 0 {
		t.Errorf("second run should not move anything, got %v", r2.Moved)
	}
}

func TestSyncSkills_NameCollisionIsSkipped(t *testing.T) {
	dirs := newSkillDirs(t)
	for _, d := range []string{dirs.Copilot, dirs.Claude, dirs.Canonical} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// canonical already has "shared"
	writeSkill(t, dirs.Canonical, "shared", "---\nname: shared-canonical\n---\nC")
	// copilot has its own different "shared" — must NOT be overwritten.
	writeSkill(t, dirs.Copilot, "shared", "---\nname: shared-copilot\n---\nLOCAL")

	r := syncSkills(dirs)

	// canonical SKILL.md must still be the original.
	body, _ := os.ReadFile(filepath.Join(dirs.Canonical, "shared", "SKILL.md"))
	if string(body) != "---\nname: shared-canonical\n---\nC" {
		t.Errorf("canonical SKILL.md was overwritten: %q", body)
	}

	// copilot dir must still be a real (non-symlink) dir.
	info, err := os.Lstat(filepath.Join(dirs.Copilot, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("colliding copilot dir should be left intact")
	}

	// Should appear in Skipped report.
	if len(r.Skipped) == 0 {
		t.Errorf("expected at least one skipped entry, got none")
	}
}
