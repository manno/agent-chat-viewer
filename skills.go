package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── data ─────────────────────────────────────────────────────────────────────

// Skill describes one agent skill discovered on disk. Each skill lives in a
// directory and is identified by the presence of a SKILL.md file with YAML
// frontmatter (at least a "name:" field).
type Skill struct {
	Agent     string    // "copilot" | "claude" | "canonical"
	Name      string    // from frontmatter name: (falls back to dir name)
	Desc      string    // from frontmatter description:
	Dir       string    // path to the skill directory
	SkillFile string    // path to SKILL.md
	IsSymlink bool      // true if Dir is a symlink
	LinkTo    string    // absolute target if IsSymlink
	Synced    bool      // symlink pointing into the canonical dir
	ModTime   time.Time // mtime of SKILL.md
	Size      int64
}

// SkillDirs holds the per-agent skill directories that acv knows about.
type SkillDirs struct {
	Copilot   string
	Claude    string
	Canonical string
}

// defaultSkillDirs returns the standard locations relative to a home dir.
// The canonical store honours XDG_CONFIG_HOME when set.
func defaultSkillDirs(home string) SkillDirs {
	canonical := filepath.Join(home, ".config", "agent-skills")
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		canonical = filepath.Join(x, "agent-skills")
	}
	return SkillDirs{
		Copilot:   filepath.Join(home, ".copilot", "skills"),
		Claude:    filepath.Join(home, ".claude", "skills"),
		Canonical: canonical,
	}
}

// ── discovery ────────────────────────────────────────────────────────────────

func findSkills(home string) []Skill {
	return findSkillsIn(defaultSkillDirs(home))
}

func findSkillsIn(dirs SkillDirs) []Skill {
	var out []Skill
	for _, d := range []struct {
		agent string
		path  string
	}{
		{"copilot", dirs.Copilot},
		{"claude", dirs.Claude},
		{"canonical", dirs.Canonical},
	} {
		out = append(out, scanSkillDir(d.agent, d.path, dirs.Canonical)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Agent < out[j].Agent
	})
	return out
}

func scanSkillDir(agent, root, canonical string) []Skill {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		info, lerr := os.Lstat(dir)
		if lerr != nil {
			continue
		}
		isSym := info.Mode()&os.ModeSymlink != 0
		var target string
		if isSym {
			if t, err := os.Readlink(dir); err == nil {
				if !filepath.IsAbs(t) {
					t = filepath.Join(root, t)
				}
				target, _ = filepath.Abs(t)
			}
		}

		// Must resolve to a directory and contain SKILL.md.
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, "SKILL.md")
		sf, err := os.Stat(skillFile)
		if err != nil {
			continue
		}

		s := Skill{
			Agent:     agent,
			Name:      e.Name(),
			Dir:       dir,
			SkillFile: skillFile,
			IsSymlink: isSym,
			LinkTo:    target,
			ModTime:   sf.ModTime(),
			Size:      sf.Size(),
		}
		if isSym && canonical != "" {
			absCanon, _ := filepath.Abs(canonical)
			s.Synced = strings.HasPrefix(target, absCanon+string(os.PathSeparator))
		}
		parseSkillFrontmatter(&s)
		out = append(out, s)
	}
	return out
}

// parseSkillFrontmatter fills s.Name / s.Desc from the SKILL.md YAML
// frontmatter when available. Supports multi-line "description: >-" blocks.
func parseSkillFrontmatter(s *Skill) {
	f, err := os.Open(s.SkillFile)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return
	}

	var (
		inDesc    bool
		descParts []string
	)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if inDesc {
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				descParts = append(descParts, strings.TrimSpace(line))
				continue
			}
			inDesc = false
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "name":
			if v != "" {
				s.Name = v
			}
		case "description":
			if v == "" || v == ">-" || v == ">" || v == "|" {
				inDesc = true
				continue
			}
			s.Desc = strings.Trim(v, "\"'")
		}
	}
	if len(descParts) > 0 {
		s.Desc = strings.Join(descParts, " ")
	}
}

// ── sync ─────────────────────────────────────────────────────────────────────

// SyncReport summarises one sync run for display in the TUI.
type SyncReport struct {
	Moved   []string // "agent:name → canonical"
	Linked  []string // "agent:name (link)"
	Skipped []string // "agent:name: reason"
	Errors  []string
}

func (r SyncReport) Summary() string {
	return fmt.Sprintf("synced: moved %d · linked %d · skipped %d · errors %d",
		len(r.Moved), len(r.Linked), len(r.Skipped), len(r.Errors))
}

// syncSkills consolidates per-agent skill directories under canonical and
// creates symlinks from each agent dir to the canonical entry. It never
// overwrites a real (non-symlink) directory: on name collision the agent's
// copy is left alone and reported in Skipped.
func syncSkills(dirs SkillDirs) SyncReport {
	r := SyncReport{}

	if dirs.Canonical == "" {
		r.Errors = append(r.Errors, "canonical dir is empty")
		return r
	}
	if err := os.MkdirAll(dirs.Canonical, 0o755); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("mkdir canonical: %v", err))
		return r
	}
	absCanon, err := filepath.Abs(dirs.Canonical)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("abs canonical: %v", err))
		return r
	}

	agentDirs := []struct {
		agent string
		path  string
	}{
		{"copilot", dirs.Copilot},
		{"claude", dirs.Claude},
	}

	// Phase 1: move real agent skill dirs into canonical (when no collision)
	// and symlink them back.
	for _, ad := range agentDirs {
		if err := os.MkdirAll(ad.path, 0o755); err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: mkdir: %v", ad.agent, err))
			continue
		}
		entries, err := os.ReadDir(ad.path)
		if err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: read: %v", ad.agent, err))
			continue
		}
		for _, e := range entries {
			src := filepath.Join(ad.path, e.Name())
			info, err := os.Lstat(src)
			if err != nil {
				continue
			}
			canonDest := filepath.Join(absCanon, e.Name())

			if info.Mode()&os.ModeSymlink != 0 {
				// Re-point broken or off-target symlinks at canonical when possible.
				target, _ := os.Readlink(src)
				if !filepath.IsAbs(target) {
					target = filepath.Join(ad.path, target)
				}
				absTarget, _ := filepath.Abs(target)
				if absTarget == canonDest {
					continue // already correct
				}
				if _, err := os.Stat(canonDest); err == nil {
					if err := os.Remove(src); err == nil {
						if err := os.Symlink(canonDest, src); err == nil {
							r.Linked = append(r.Linked, fmt.Sprintf("%s:%s (repointed)", ad.agent, e.Name()))
							continue
						}
					}
				}
				r.Skipped = append(r.Skipped, fmt.Sprintf("%s:%s: symlink to %s (canonical missing)", ad.agent, e.Name(), target))
				continue
			}
			if !info.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
				continue // not a skill dir
			}

			if _, err := os.Stat(canonDest); err == nil {
				r.Skipped = append(r.Skipped, fmt.Sprintf("%s:%s: canonical already has this skill (resolve manually)", ad.agent, e.Name()))
				continue
			}
			if err := os.Rename(src, canonDest); err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("%s:%s: move: %v", ad.agent, e.Name(), err))
				continue
			}
			if err := os.Symlink(canonDest, src); err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("%s:%s: link back: %v", ad.agent, e.Name(), err))
				continue
			}
			r.Moved = append(r.Moved, fmt.Sprintf("%s:%s → canonical", ad.agent, e.Name()))
		}
	}

	// Phase 2: for every canonical skill, ensure each agent dir has a symlink
	// pointing at it (unless something is already in the way).
	canonEntries, err := os.ReadDir(absCanon)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("read canonical: %v", err))
		return r
	}
	for _, e := range canonEntries {
		canonDir := filepath.Join(absCanon, e.Name())
		if st, err := os.Stat(canonDir); err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(canonDir, "SKILL.md")); err != nil {
			continue
		}
		for _, ad := range agentDirs {
			dest := filepath.Join(ad.path, e.Name())
			info, err := os.Lstat(dest)
			if err != nil {
				if err := os.Symlink(canonDir, dest); err == nil {
					r.Linked = append(r.Linked, fmt.Sprintf("%s:%s", ad.agent, e.Name()))
				} else {
					r.Errors = append(r.Errors, fmt.Sprintf("%s:%s: link: %v", ad.agent, e.Name(), err))
				}
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(dest)
				if !filepath.IsAbs(target) {
					target = filepath.Join(ad.path, target)
				}
				absT, _ := filepath.Abs(target)
				if absT == canonDir {
					continue // already linked
				}
				if err := os.Remove(dest); err == nil {
					if err := os.Symlink(canonDir, dest); err == nil {
						r.Linked = append(r.Linked, fmt.Sprintf("%s:%s (repointed)", ad.agent, e.Name()))
						continue
					}
				}
				r.Skipped = append(r.Skipped, fmt.Sprintf("%s:%s: existing symlink → %s", ad.agent, e.Name(), target))
				continue
			}
			// Real file or dir blocks the symlink.
			r.Skipped = append(r.Skipped, fmt.Sprintf("%s:%s: blocked by existing non-symlink entry", ad.agent, e.Name()))
		}
	}
	return r
}

// ── filter helper ────────────────────────────────────────────────────────────

func filterSkills(skills []Skill, query string) []Skill {
	if query == "" {
		return skills
	}
	q := strings.ToLower(query)
	var out []Skill
	for _, s := range skills {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Agent), q) ||
			strings.Contains(strings.ToLower(s.Desc), q) {
			out = append(out, s)
		}
	}
	return out
}
