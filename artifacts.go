package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── data types ────────────────────────────────────────────────────────────────

type MemoryFile struct {
	Path    string
	Project string
	Name    string    // from frontmatter name:
	Type    string    // from frontmatter metadata.type:
	Desc    string    // from frontmatter description:
	Content string    // body after frontmatter
	ModTime time.Time
	Size    int64
}

type Artifact struct {
	Path      string
	Agent     string
	Project   string
	SessionID string // session UUID dir, "" if project-level
	Kind      string // "tool-result" | "gemini-log"
	Name      string
	Preview   string // first ~200 chars, newlines collapsed
	ModTime   time.Time
	Size      int64
}

// ── discovery ─────────────────────────────────────────────────────────────────

func findMemories(home string) []MemoryFile {
	var out []MemoryFile
	claudePath := filepath.Join(home, ".claude", "projects")
	homePrefix := strings.ReplaceAll(home, string(os.PathSeparator), "-") + "-"

	entries, err := os.ReadDir(claudePath)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		project := strings.TrimPrefix(entry.Name(), homePrefix)
		memDir := filepath.Join(claudePath, entry.Name(), "memory")
		files, _ := filepath.Glob(filepath.Join(memDir, "*.md"))
		for _, f := range files {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			m := parseMemoryFile(f)
			if m == nil {
				continue
			}
			m.Project = project
			m.ModTime = info.ModTime()
			m.Size = info.Size()
			out = append(out, *m)
		}
	}
	return out
}

func parseMemoryFile(path string) *MemoryFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	m := &MemoryFile{
		Path: path,
		Name: strings.TrimSuffix(filepath.Base(path), ".md"),
	}
	content := string(data)

	if !strings.HasPrefix(content, "---") {
		m.Content = content
		return m
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		m.Content = content
		return m
	}
	front := rest[:end]
	m.Content = strings.TrimSpace(rest[end+4:])

	inMetadata := false
	scanner := bufio.NewScanner(strings.NewReader(front))
	for scanner.Scan() {
		line := scanner.Text()
		isIndented := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
		stripped := strings.TrimLeft(line, " \t")

		if strings.TrimSpace(stripped) == "metadata:" {
			inMetadata = true
			continue
		}
		if !isIndented {
			inMetadata = false
		}

		k, v, ok := strings.Cut(stripped, ": ")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)

		if inMetadata {
			if k == "type" {
				m.Type = v
			}
			continue
		}
		switch k {
		case "name":
			m.Name = v
		case "description":
			m.Desc = v
		}
	}
	return m
}

func findArtifacts(home string) []Artifact {
	var out []Artifact

	claudePath := filepath.Join(home, ".claude", "projects")
	homePrefix := strings.ReplaceAll(home, string(os.PathSeparator), "-") + "-"

	projEntries, _ := os.ReadDir(claudePath)
	for _, projEntry := range projEntries {
		if !projEntry.IsDir() {
			continue
		}
		project := strings.TrimPrefix(projEntry.Name(), homePrefix)
		projDir := filepath.Join(claudePath, projEntry.Name())

		sessionEntries, _ := os.ReadDir(projDir)
		for _, sessionEntry := range sessionEntries {
			if !sessionEntry.IsDir() {
				continue
			}
			sessionID := sessionEntry.Name()
			toolDir := filepath.Join(projDir, sessionID, "tool-results")
			files, _ := filepath.Glob(filepath.Join(toolDir, "*.txt"))
			for _, f := range files {
				info, err := os.Stat(f)
				if err != nil {
					continue
				}
				out = append(out, Artifact{
					Path:      f,
					Agent:     "claude",
					Project:   project,
					SessionID: sessionID,
					Kind:      "tool-result",
					Name:      filepath.Base(f),
					Preview:   filePreview(f, 200),
					ModTime:   info.ModTime(),
					Size:      info.Size(),
				})
			}
		}
	}

	geminiTmp := filepath.Join(home, ".gemini", "tmp")
	geminiEntries, _ := os.ReadDir(geminiTmp)
	for _, entry := range geminiEntries {
		if !entry.IsDir() {
			continue
		}
		logPath := filepath.Join(geminiTmp, entry.Name(), "logs.json")
		info, err := os.Stat(logPath)
		if err != nil {
			continue
		}
		out = append(out, Artifact{
			Path:    logPath,
			Agent:   "gemini",
			Project: entry.Name(),
			Kind:    "gemini-log",
			Name:    "logs.json",
			Preview: filePreview(logPath, 200),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}

	return out
}

func filePreview(path string, maxBytes int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, _ := f.Read(buf)
	return strings.ReplaceAll(strings.TrimSpace(string(buf[:n])), "\n", " ")
}

// ── CLI output ────────────────────────────────────────────────────────────────

func printMemories(memories []MemoryFile, agentFilter, projectFilter string) {
	if agentFilter != "" && !strings.EqualFold(agentFilter, "claude") {
		fmt.Printf("Memories are Claude-only; no results for agent %q.\n", agentFilter)
		return
	}
	var out []MemoryFile
	for _, m := range memories {
		if projectFilter != "" && !strings.Contains(strings.ToLower(m.Project), strings.ToLower(projectFilter)) {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		fmt.Println("No memory files found.")
		return
	}
	fmt.Printf("%-24s %-12s %-24s %-44s %s\n", "PROJECT", "TYPE", "NAME", "DESC", "MODIFIED")
	fmt.Println(strings.Repeat("-", 130))
	for _, m := range out {
		fmt.Printf("%-24s %-12s %-24s %-44s %s\n",
			truncate(m.Project, 24),
			truncate(m.Type, 12),
			truncate(m.Name, 24),
			truncate(m.Desc, 44),
			m.ModTime.Format("2006-01-02 15:04"),
		)
	}
	fmt.Printf("\n%d memory file(s).\n", len(out))
}

func printArtifacts(artifacts []Artifact, agentFilter, projectFilter string) {
	var out []Artifact
	for _, a := range artifacts {
		if agentFilter != "" && !strings.EqualFold(a.Agent, agentFilter) {
			continue
		}
		if projectFilter != "" && !strings.Contains(strings.ToLower(a.Project), strings.ToLower(projectFilter)) {
			continue
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		fmt.Println("No artifact files found.")
		return
	}
	fmt.Printf("%-10s %-20s %-14s %-12s %-20s %s\n", "AGENT", "PROJECT", "SESSION", "KIND", "NAME", "SIZE")
	fmt.Println(strings.Repeat("-", 100))
	for _, a := range out {
		sessionShort := a.SessionID
		if len(sessionShort) > 13 {
			sessionShort = sessionShort[:13]
		}
		fmt.Printf("%-10s %-20s %-14s %-12s %-20s %s\n",
			a.Agent,
			truncate(a.Project, 20),
			sessionShort,
			a.Kind,
			truncate(a.Name, 20),
			formatSize(a.Size),
		)
	}
	fmt.Printf("\n%d artifact file(s).\n", len(out))
}
