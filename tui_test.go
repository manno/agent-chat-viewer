package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// ── filterSessions ────────────────────────────────────────────────────────────

func makeSessions() []Session {
	return []Session{
		{Agent: "claude", Project: "my-project", Title: "fix code layout", Messages: []Message{{Role: "user", Content: "hello from claude"}}},
		{Agent: "gemini", Project: "other-project", Title: "write tests", Messages: []Message{{Role: "user", Content: "hello from gemini"}}},
		{Agent: "copilot", Project: "my-project", Title: "implement chat feature", Messages: []Message{{Role: "assistant", Content: "copilot reply"}}},
	}
}

func TestFilterSessions_EmptyQuery(t *testing.T) {
	sessions := makeSessions()
	got := filterSessions(sessions, "")
	if len(got) != len(sessions) {
		t.Errorf("empty query should return all sessions, got %d", len(got))
	}
}

func TestFilterSessions_ByAgent(t *testing.T) {
	sessions := makeSessions()
	got := filterSessions(sessions, "claude")
	if len(got) != 1 || got[0].Agent != "claude" {
		t.Errorf("expected 1 claude session, got %v", got)
	}
}

func TestFilterSessions_ByTitle(t *testing.T) {
	sessions := makeSessions()
	got := filterSessions(sessions, "layout")
	if len(got) != 1 || got[0].Title != "fix code layout" {
		t.Errorf("expected 1 session matching layout title, got %v", got)
	}
}

func TestFilterSessions_ByProject(t *testing.T) {
	sessions := makeSessions()
	got := filterSessions(sessions, "my-project")
	if len(got) != 2 {
		t.Errorf("expected 2 sessions in my-project, got %d", len(got))
	}
}

func TestFilterSessions_CaseInsensitive(t *testing.T) {
	sessions := makeSessions()
	got := filterSessions(sessions, "CLAUDE")
	if len(got) != 1 {
		t.Errorf("filter should be case-insensitive, got %d", len(got))
	}
}

func TestFilterSessions_NoMatch(t *testing.T) {
	sessions := makeSessions()
	got := filterSessions(sessions, "nonexistent")
	if len(got) != 0 {
		t.Errorf("expected 0 results, got %d", len(got))
	}
}

// ── searchContent ─────────────────────────────────────────────────────────────

func TestSearchContent_BasicMatch(t *testing.T) {
	sessions := makeSessions()
	hits := searchContent(sessions, "hello")
	if len(hits) != 2 {
		t.Errorf("expected 2 hits for 'hello', got %d", len(hits))
	}
}

func TestSearchContent_CaseInsensitive(t *testing.T) {
	sessions := makeSessions()
	hits := searchContent(sessions, "CLAUDE")
	if len(hits) != 1 {
		t.Errorf("expected 1 hit, got %d", len(hits))
	}
}

func TestSearchContent_Wildcard(t *testing.T) {
	sessions := makeSessions()
	hits := searchContent(sessions, "hel*")
	if len(hits) != 2 {
		t.Errorf("expected 2 hits for 'hel*', got %d", len(hits))
	}
}

func TestSearchContent_NoMatch(t *testing.T) {
	sessions := makeSessions()
	hits := searchContent(sessions, "zzznomatch")
	if len(hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(hits))
	}
}

func TestSearchContent_OneHitPerSession(t *testing.T) {
	sessions := []Session{{
		Agent:   "claude",
		Project: "p",
		Messages: []Message{
			{Role: "user", Content: "needle here"},
			{Role: "assistant", Content: "also needle"},
		},
	}}
	hits := searchContent(sessions, "needle")
	if len(hits) != 1 {
		t.Errorf("expected 1 hit per session, got %d", len(hits))
	}
}

// ── wordWrap ──────────────────────────────────────────────────────────────────

func TestWordWrap_ShortLine(t *testing.T) {
	out := wordWrap("hello", 80)
	if out != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestWordWrap_LongLine(t *testing.T) {
	input := strings.TrimSpace(strings.Repeat("word ", 20))
	out := wordWrap(input, 20)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 20 {
			t.Errorf("line too long (%d): %q", len(line), line)
		}
	}
}

func TestWordWrap_PreservesExistingNewlines(t *testing.T) {
	input := "line one\nline two\nline three"
	out := wordWrap(input, 80)
	if out != input {
		t.Errorf("short lines with newlines should be unchanged; got %q", out)
	}
}

func TestWordWrap_ZeroWidth(t *testing.T) {
	input := "hello world"
	out := wordWrap(input, 0)
	if out != input {
		t.Errorf("zero width should return input unchanged")
	}
}

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncate_ShortString(t *testing.T) {
	if truncate("hi", 10) != "hi" {
		t.Error("short string should not be truncated")
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	if truncate("hello", 5) != "hello" {
		t.Error("exact length should not be truncated")
	}
}

func TestTruncate_LongString(t *testing.T) {
	got := truncate("hello world", 8)
	if len([]rune(got)) > 8 {
		t.Errorf("result too long: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated string should end with ellipsis: %q", got)
	}
}

// ── buildSnippet ──────────────────────────────────────────────────────────────

func TestBuildSnippet_MatchInMiddle(t *testing.T) {
	re := regexp.MustCompile(`(?i)needle`)
	content := "some text before needle and some text after it for context"
	snippet := buildSnippet(content, re, 100)
	if !strings.Contains(snippet, "needle") {
		t.Errorf("snippet should contain the match: %q", snippet)
	}
}

func TestBuildSnippet_MaxLen(t *testing.T) {
	re := regexp.MustCompile(`(?i)x`)
	content := strings.Repeat("a", 200) + "x" + strings.Repeat("b", 200)
	snippet := buildSnippet(content, re, 50)
	if len(snippet) > 55 {
		t.Errorf("snippet too long: %d chars: %q", len(snippet), snippet)
	}
}

func TestBuildSnippet_NewlinesCollapsed(t *testing.T) {
	re := regexp.MustCompile(`(?i)target`)
	content := "line one\nline two\ntarget here\nline four"
	snippet := buildSnippet(content, re, 100)
	if strings.Contains(snippet, "\n") {
		t.Errorf("snippet should not contain newlines: %q", snippet)
	}
}

// ── renderMessages (no terminal required) ─────────────────────────────────────

func TestRenderMessages_RoleFilter(t *testing.T) {
	m := newTUI()
	m.width = 80
	m.height = 24
	ts := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	m.selected = &Session{
		Messages: []Message{
			{Role: "user", Content: "user says hi", Time: ts},
			{Role: "assistant", Content: "assistant replies", Time: ts},
		},
	}

	m.roleFilter = "user"
	out := m.renderMessages()
	if !strings.Contains(out, "user says hi") {
		t.Error("user message should be present with user filter")
	}
	if strings.Contains(out, "assistant replies") {
		t.Error("assistant message should be filtered out")
	}

	m.roleFilter = "assistant"
	out = m.renderMessages()
	if strings.Contains(out, "user says hi") {
		t.Error("user message should be filtered out")
	}
	if !strings.Contains(out, "assistant replies") {
		t.Error("assistant message should be present with assistant filter")
	}
}

func TestRenderMessages_NoFilterShowsAll(t *testing.T) {
	m := newTUI()
	m.width = 80
	ts := time.Now()
	m.selected = &Session{
		Messages: []Message{
			{Role: "user", Content: "user msg", Time: ts},
			{Role: "assistant", Content: "assistant msg", Time: ts},
		},
	}
	m.roleFilter = ""
	out := m.renderMessages()
	if !strings.Contains(out, "user msg") || !strings.Contains(out, "assistant msg") {
		t.Error("no filter should show all messages")
	}
}

func TestRenderMessages_EmptyResult(t *testing.T) {
	m := newTUI()
	m.width = 80
	m.selected = &Session{
		Messages: []Message{
			{Role: "user", Content: "hi", Time: time.Now()},
		},
	}
	m.roleFilter = "assistant"
	out := m.renderMessages()
	if !strings.Contains(out, "No messages") {
		t.Errorf("expected empty-state message, got: %q", out)
	}
}

func TestRenderMessages_NilSession(t *testing.T) {
	m := newTUI()
	m.width = 80
	m.selected = nil
	out := m.renderMessages()
	if out != "" {
		t.Errorf("nil session should return empty string, got %q", out)
	}
}
