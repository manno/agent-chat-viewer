package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── formatSize ────────────────────────────────────────────────────────────────

func TestFormatSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{int64(1.5 * 1024 * 1024), "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		got := formatSize(c.in)
		if got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── parseClaude ───────────────────────────────────────────────────────────────

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	// Embed in a path that parseClaude recognises as claude
	path := filepath.Join(dir, ".claude", "projects", "my-project", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseClaude_BasicMessages(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"uuid":"a1","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"hello world"}}`,
		`{"uuid":"b2","timestamp":"2026-01-01T10:00:05Z","message":{"role":"assistant","content":"hi there"}}`,
	}, "\n")

	path := writeTmp(t, jsonl)
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(s.Messages))
	}
	if s.Messages[0].Role != "user" || s.Messages[0].Content != "hello world" {
		t.Errorf("unexpected first message: %+v", s.Messages[0])
	}
	if s.Messages[1].Role != "assistant" || s.Messages[1].Content != "hi there" {
		t.Errorf("unexpected second message: %+v", s.Messages[1])
	}
}

func TestParseClaude_DeduplicatesUUID(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"uuid":"dup","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"first"}}`,
		`{"uuid":"dup","timestamp":"2026-01-01T10:00:01Z","message":{"role":"user","content":"duplicate"}}`,
		`{"uuid":"uniq","timestamp":"2026-01-01T10:00:02Z","message":{"role":"assistant","content":"reply"}}`,
	}, "\n")

	path := writeTmp(t, jsonl)
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("got %d messages, want 2 (duplicate uuid should be skipped)", len(s.Messages))
	}
}

func TestParseClaude_ContentArray(t *testing.T) {
	jsonl := `{"uuid":"c1","timestamp":"2026-01-01T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"part one "},{"type":"text","text":"part two"}]}}`

	path := writeTmp(t, jsonl)
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(s.Messages))
	}
	if s.Messages[0].Content != "part one part two" {
		t.Errorf("got content %q", s.Messages[0].Content)
	}
}

func TestParseClaude_Timestamps(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"uuid":"t1","timestamp":"2026-01-01T09:00:00Z","message":{"role":"user","content":"early"}}`,
		`{"uuid":"t2","timestamp":"2026-01-01T10:00:00Z","message":{"role":"assistant","content":"later"}}`,
	}, "\n")

	path := writeTmp(t, jsonl)
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	wantLast := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if !s.StartTime.Equal(wantStart) {
		t.Errorf("StartTime = %v, want %v", s.StartTime, wantStart)
	}
	if !s.LastTime.Equal(wantLast) {
		t.Errorf("LastTime = %v, want %v", s.LastTime, wantLast)
	}
}

func TestParseClaude_ProjectFromPath(t *testing.T) {
	jsonl := `{"uuid":"p1","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"hi"}}`
	path := writeTmp(t, jsonl)
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Project != "my-project" {
		t.Errorf("Project = %q, want %q", s.Project, "my-project")
	}
}

func TestParseClaude_EmptyFile(t *testing.T) {
	path := writeTmp(t, "")
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error on empty file: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(s.Messages))
	}
}

func TestParseClaude_MalformedLines(t *testing.T) {
	jsonl := strings.Join([]string{
		`not json at all`,
		`{"uuid":"ok","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"valid"}}`,
		`{"broken":`,
	}, "\n")

	path := writeTmp(t, jsonl)
	s, err := parseClaude(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(s.Messages))
	}
}

// ── parseCopilotDir ───────────────────────────────────────────────────────────

func writeCopilotDir(t *testing.T, id, eventsContent, workspaceContent string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".copilot", "session-state", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(eventsContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if workspaceContent != "" {
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(workspaceContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseCopilotDir_BasicMessages(t *testing.T) {
	events := strings.Join([]string{
		`{"type":"session.start","data":{},"id":"s1","timestamp":"2026-01-01T09:00:00Z"}`,
		`{"type":"user.message","data":{"content":"hello"},"id":"m1","timestamp":"2026-01-01T09:00:01Z"}`,
		`{"type":"assistant.message","data":{"content":"hi"},"id":"m2","timestamp":"2026-01-01T09:00:02Z"}`,
	}, "\n")
	workspace := "cwd: /home/user/my-project\ncreated_at: 2026-01-01T09:00:00Z\nupdated_at: 2026-01-01T10:00:00Z\n"

	dir := writeCopilotDir(t, "test-uuid", events, workspace)
	s, err := parseCopilotDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ID != "test-uuid" {
		t.Errorf("ID = %q, want %q", s.ID, "test-uuid")
	}
	if s.Project != "my-project" {
		t.Errorf("Project = %q, want %q", s.Project, "my-project")
	}
	if len(s.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(s.Messages))
	}
	wantUpdated := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if !s.LastTime.Equal(wantUpdated) {
		t.Errorf("LastTime = %v, want %v (from workspace.yaml updated_at)", s.LastTime, wantUpdated)
	}
}

func TestParseCopilotDir_NoWorkspace(t *testing.T) {
	events := `{"type":"user.message","data":{"content":"hi"},"id":"m1","timestamp":"2026-01-01T09:00:00Z"}`
	dir := writeCopilotDir(t, "no-workspace", events, "")
	s, err := parseCopilotDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Messages) != 1 {
		t.Errorf("got %d messages, want 1", len(s.Messages))
	}
}



func TestFindAndSortSessions_OrderedByLastTime(t *testing.T) {
	s1 := Session{LastTime: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	s2 := Session{LastTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	s3 := Session{LastTime: time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)}

	sessions := []Session{s1, s2, s3}
	// apply the same sort logic used by findAndSortSessions
	sorted := make([]Session, len(sessions))
	copy(sorted, sessions)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].LastTime.After(sorted[i].LastTime) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	if !sorted[0].LastTime.Equal(s2.LastTime) {
		t.Errorf("first element should be newest, got %v", sorted[0].LastTime)
	}
	if !sorted[2].LastTime.Equal(s3.LastTime) {
		t.Errorf("last element should be oldest, got %v", sorted[2].LastTime)
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name     string
		messages []Message
		want     string
	}{
		{
			name:     "Empty",
			messages: nil,
			want:     "Empty Session",
		},
		{
			name: "Simple User Msg",
			messages: []Message{
				{Role: "user", Content: "Hello world!"},
			},
			want: "Hello world!",
		},
		{
			name: "Markdown Header",
			messages: []Message{
				{Role: "user", Content: "###  My Important Topic"},
			},
			want: "My Important Topic",
		},
		{
			name: "List Marker",
			messages: []Message{
				{Role: "user", Content: "- Fix the bug"},
			},
			want: "Fix the bug",
		},
		{
			name: "Numeric List Marker",
			messages: []Message{
				{Role: "user", Content: "1. Write tests"},
			},
			want: "Write tests",
		},
		{
			name: "Multi-line First Line Empty",
			messages: []Message{
				{Role: "user", Content: "\n  \nFirst non-empty line\nSecond line"},
			},
			want: "First non-empty line",
		},
		{
			name: "Code Block Fence",
			messages: []Message{
				{Role: "user", Content: "```go\npackage main\n"},
			},
			want: "package main",
		},
		{
			name: "No User Message Fallback",
			messages: []Message{
				{Role: "assistant", Content: "How can I help you?"},
			},
			want: "How can I help you?",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractTitle(c.messages)
			if got != c.want {
				t.Errorf("extractTitle() = %q, want %q", got, c.want)
			}
		})
	}
}
