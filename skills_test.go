package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSyncSkills_NameCollisionSurfacesAsConflict(t *testing.T) {
	dirs := newSkillDirs(t)
	for _, d := range []string{dirs.Copilot, dirs.Claude, dirs.Canonical} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(t, dirs.Canonical, "shared", "---\nname: shared\n---\nCANON BODY\n")
	writeSkill(t, dirs.Copilot, "shared", "---\nname: shared\n---\nLOCAL BODY\n")

	r := syncSkills(dirs)

	if len(r.Conflicts) != 1 {
		t.Fatalf("want 1 conflict, got %d (skipped=%v)", len(r.Conflicts), r.Skipped)
	}
	c := r.Conflicts[0]
	if c.Name != "shared" || c.Agent != "copilot" {
		t.Errorf("unexpected conflict: %+v", c)
	}
	if c.DiffText == "" || !strings.Contains(c.DiffText, "CANON BODY") || !strings.Contains(c.DiffText, "LOCAL BODY") {
		t.Errorf("diff text missing or incomplete:\n%s", c.DiffText)
	}

	// Nothing should have been moved or overwritten yet.
	body, _ := os.ReadFile(filepath.Join(dirs.Canonical, "shared", "SKILL.md"))
	if !strings.Contains(string(body), "CANON BODY") {
		t.Errorf("canonical mutated unexpectedly: %q", body)
	}
	info, _ := os.Lstat(filepath.Join(dirs.Copilot, "shared"))
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("copilot dir should still be a real dir before resolution")
	}
}

func TestResolveSkillConflict_KeepCanonical(t *testing.T) {
	dirs := newSkillDirs(t)
	_ = os.MkdirAll(dirs.Copilot, 0o755)
	_ = os.MkdirAll(dirs.Canonical, 0o755)
	writeSkill(t, dirs.Canonical, "shared", "---\nname: shared\n---\nCANON\n")
	writeSkill(t, dirs.Copilot, "shared", "---\nname: shared\n---\nLOCAL\n")

	r := syncSkills(dirs)
	if len(r.Conflicts) != 1 {
		t.Fatalf("want 1 conflict, got %d", len(r.Conflicts))
	}
	if err := resolveSkillConflict(r.Conflicts[0], ActionKeepCanonical); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// copilot/shared should now be a symlink to canonical.
	info, err := os.Lstat(filepath.Join(dirs.Copilot, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("copilot/shared should be symlink after KeepCanonical")
	}
	body, _ := os.ReadFile(filepath.Join(dirs.Copilot, "shared", "SKILL.md"))
	if !strings.Contains(string(body), "CANON") {
		t.Errorf("symlinked content should reflect canonical, got %q", body)
	}
	// And a backup of the old local dir must exist.
	matches, _ := filepath.Glob(filepath.Join(dirs.Copilot, "shared.conflict-*"))
	if len(matches) != 1 {
		t.Errorf("expected exactly 1 backup of local copy, got %v", matches)
	}
}

func TestResolveSkillConflict_UseLocal(t *testing.T) {
	dirs := newSkillDirs(t)
	_ = os.MkdirAll(dirs.Copilot, 0o755)
	_ = os.MkdirAll(dirs.Canonical, 0o755)
	writeSkill(t, dirs.Canonical, "shared", "---\nname: shared\n---\nCANON\n")
	writeSkill(t, dirs.Copilot, "shared", "---\nname: shared\n---\nLOCAL\n")

	r := syncSkills(dirs)
	if err := resolveSkillConflict(r.Conflicts[0], ActionUseLocal); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// canonical/shared should now contain the local body.
	body, _ := os.ReadFile(filepath.Join(dirs.Canonical, "shared", "SKILL.md"))
	if !strings.Contains(string(body), "LOCAL") {
		t.Errorf("canonical/shared should hold local body, got %q", body)
	}
	// copilot/shared is now a symlink → canonical (which holds local body).
	info, err := os.Lstat(filepath.Join(dirs.Copilot, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("copilot/shared should be symlink after UseLocal")
	}
	// Backup of the old canonical body should exist.
	matches, _ := filepath.Glob(filepath.Join(dirs.Canonical, "shared.conflict-*"))
	if len(matches) != 1 {
		t.Errorf("expected exactly 1 backup of old canonical, got %v", matches)
	}
}

func TestFindSkillsIn_DeduplicatesSyncedEntries(t *testing.T) {
	dirs := newSkillDirs(t)
	for _, d := range []string{dirs.Copilot, dirs.Claude} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(t, dirs.Copilot, "alpha", "---\nname: alpha\n---\nA")
	writeSkill(t, dirs.Claude, "beta", "---\nname: beta\n---\nB")

	// Sync creates canonical copies and symlinks in agent dirs.
	r := syncSkills(dirs)
	if len(r.Errors) > 0 {
		t.Fatalf("sync errors: %v", r.Errors)
	}

	skills := findSkillsIn(dirs)

	// After full sync: canonical has alpha + beta, each agent dir has synced
	// symlinks. findSkillsIn should return only 2 entries (the canonical ones),
	// not 6 (2 canonical + 2 copilot synced + 2 claude synced).
	if len(skills) != 2 {
		names := make([]string, len(skills))
		for i, s := range skills {
			names[i] = s.Agent + "/" + s.Name
		}
		t.Fatalf("want 2 skills (canonical only), got %d: %v", len(skills), names)
	}
	for _, s := range skills {
		if s.Agent != "canonical" {
			t.Errorf("expected canonical agent, got %q for skill %q", s.Agent, s.Name)
		}
	}
}
