package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Message struct {
	Role    string
	Content string
	Time    time.Time
}

type Session struct {
	ID        string
	Agent     string
	Path      string
	Project   string
	Size      int64
	StartTime time.Time
	LastTime  time.Time
	Messages  []Message
}

func findSessions(home string) []Session {
	var sessions []Session

	copilotPath := filepath.Join(home, ".copilot", "session-state")
	files, _ := filepath.Glob(filepath.Join(copilotPath, "*.jsonl"))
	for _, f := range files {
		if s, err := parseCopilot(f); err == nil {
			if info, err := os.Stat(f); err == nil {
				s.Size = info.Size()
			}
			sessions = append(sessions, *s)
		}
	}

	geminiPath := filepath.Join(home, ".gemini", "tmp")
	filepath.Walk(geminiPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") && strings.Contains(path, "/chats/session-") {
			if s, err := parseGemini(path); err == nil {
				s.Size = info.Size()
				sessions = append(sessions, *s)
			}
		}
		return nil
	})

	claudePath := filepath.Join(home, ".claude", "projects")
	filepath.Walk(claudePath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			if s, err := parseClaude(path); err == nil {
				s.Size = info.Size()
				sessions = append(sessions, *s)
			}
		}
		return nil
	})

	return sessions
}

func findAndSortSessions(home string) []Session {
	sessions := findSessions(home)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastTime.After(sessions[j].LastTime)
	})
	return sessions
}

func parseSession(path string) (*Session, error) {
	if strings.Contains(path, ".copilot") {
		return parseCopilot(path)
	}
	if strings.Contains(path, ".gemini") {
		return parseGemini(path)
	}
	if strings.Contains(path, ".claude") {
		return parseClaude(path)
	}
	return nil, fmt.Errorf("unknown session type for path: %s", path)
}

func parseCopilot(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	s := &Session{ID: filepath.Base(path), Agent: "copilot", Path: path}
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var line struct {
			ID        string    `json:"id"`
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Data      struct {
				Content string `json:"content"`
			} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.ID != "" {
			if seen[line.ID] {
				continue
			}
			seen[line.ID] = true
		}
		if line.Type == "session.start" {
			s.StartTime = line.Timestamp
			s.LastTime = line.Timestamp
		}
		if line.Type == "user.message" {
			s.Messages = append(s.Messages, Message{Role: "user", Content: line.Data.Content, Time: line.Timestamp})
		} else if line.Type == "assistant.message" {
			s.Messages = append(s.Messages, Message{Role: "assistant", Content: line.Data.Content, Time: line.Timestamp})
		}
		if !line.Timestamp.IsZero() {
			if s.LastTime.IsZero() || line.Timestamp.After(s.LastTime) {
				s.LastTime = line.Timestamp
			}
		}
	}
	if s.StartTime.IsZero() && len(s.Messages) > 0 {
		s.StartTime = s.Messages[0].Time
	}
	if s.LastTime.IsZero() && len(s.Messages) > 0 {
		s.LastTime = s.Messages[len(s.Messages)-1].Time
	}
	return s, nil
}

func parseGemini(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	s := &Session{ID: filepath.Base(path), Agent: "gemini", Path: path}
	seen := make(map[string]bool)
	parts := strings.Split(path, string(os.PathSeparator))
	for i, p := range parts {
		if p == "tmp" && i+1 < len(parts) {
			s.Project = parts[i+1]
			break
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var line map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		id, _ := line["id"].(string)
		if id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		if _, ok := line["startTime"]; ok {
			if st, ok := line["startTime"].(string); ok {
				s.StartTime, _ = time.Parse(time.RFC3339, st)
				s.LastTime = s.StartTime
			}
			continue
		}
		msgType, _ := line["type"].(string)
		tsStr, _ := line["timestamp"].(string)
		ts, _ := time.Parse(time.RFC3339, tsStr)
		if !ts.IsZero() {
			if s.LastTime.IsZero() || ts.After(s.LastTime) {
				s.LastTime = ts
			}
		}
		if msgType == "user" {
			content, _ := line["content"].([]interface{})
			if len(content) > 0 {
				cMap, _ := content[0].(map[string]interface{})
				text, _ := cMap["text"].(string)
				s.Messages = append(s.Messages, Message{Role: "user", Content: text, Time: ts})
			}
		} else if msgType == "gemini" {
			content, _ := line["content"].(string)
			if content == "" {
				thoughts, _ := line["thoughts"].([]interface{})
				if len(thoughts) > 0 {
					tFirst, _ := thoughts[0].(map[string]interface{})
					content = "[Thought] " + tFirst["description"].(string)
				}
			}
			if content != "" {
				s.Messages = append(s.Messages, Message{Role: "assistant", Content: content, Time: ts})
			}
		}
	}
	return s, nil
}

func parseClaude(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	s := &Session{ID: filepath.Base(path), Agent: "claude", Path: path}
	seen := make(map[string]bool)
	parts := strings.Split(path, string(os.PathSeparator))
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			s.Project = parts[i+1]
			break
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for scanner.Scan() {
		var line map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		uuid, _ := line["uuid"].(string)
		if uuid != "" {
			if seen[uuid] {
				continue
			}
			seen[uuid] = true
		}
		msg, ok := line["message"].(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		tsStr, _ := line["timestamp"].(string)
		ts, _ := time.Parse(time.RFC3339, tsStr)
		if s.StartTime.IsZero() {
			s.StartTime = ts
		}
		if s.LastTime.IsZero() || ts.After(s.LastTime) {
			s.LastTime = ts
		}
		content := msg["content"]
		var fullContent strings.Builder
		switch c := content.(type) {
		case string:
			fullContent.WriteString(c)
		case []interface{}:
			for _, item := range c {
				cMap, _ := item.(map[string]interface{})
				cType, _ := cMap["type"].(string)
				if cType == "text" {
					text, _ := cMap["text"].(string)
					fullContent.WriteString(text)
				} else if cType == "thinking" {
					text, _ := cMap["thinking"].(string)
					if text != "" {
						fullContent.WriteString("[Thinking] " + text + "\n")
					}
				}
			}
		}
		if fullContent.Len() > 0 {
			s.Messages = append(s.Messages, Message{Role: role, Content: fullContent.String(), Time: ts})
		}
	}
	return s, nil
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func printSession(s *Session, filter string) {
	fmt.Printf("Session: %s\n", s.ID)
	fmt.Printf("Agent:   %s\n", s.Agent)
	fmt.Printf("Project: %s\n", s.Project)
	fmt.Printf("Start:   %s\n", s.StartTime.Format(time.RFC1123))
	fmt.Printf("Updated: %s\n", s.LastTime.Format(time.RFC1123))
	fmt.Printf("Path:    %s\n", s.Path)
	fmt.Printf("Size:    %s\n", formatSize(s.Size))
	fmt.Println(strings.Repeat("=", 80))
	for _, m := range s.Messages {
		if filter != "" && !strings.Contains(strings.ToLower(m.Role), filter) {
			continue
		}
		role := strings.ToUpper(m.Role)
		fmt.Printf("[%s] (%s)\n%s\n\n", role, m.Time.Format("15:04:05"), m.Content)
		fmt.Println(strings.Repeat("-", 40))
	}
}

func runSearch(sessions []Session, query string) {
	pattern := regexp.QuoteMeta(query)
	pattern = strings.ReplaceAll(pattern, "\\*", ".*")
	pattern = strings.ReplaceAll(pattern, "\\?", ".")
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid search pattern: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Searching for: %s\n", query)
	fmt.Println(strings.Repeat("=", 80))
	found := 0
	for _, s := range sessions {
		for _, m := range s.Messages {
			if re.MatchString(m.Content) {
				found++
				fmt.Printf("[%s] %s | %s | %s\n", s.Agent, s.Project, s.StartTime.Format("2006-01-02"), strings.ToUpper(m.Role))
				content := strings.ReplaceAll(m.Content, "\n", " ")
				if len(content) > 100 {
					content = content[:97] + "..."
				}
				fmt.Printf("  %s\n", content)
				fmt.Println(strings.Repeat("-", 40))
			}
		}
	}
	fmt.Printf("\nFound %d matches in %d sessions.\n", found, len(sessions))
}
