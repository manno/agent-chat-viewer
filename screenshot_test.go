package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestGenerateScreenshots renders the TUI against a synthetic home directory and
// writes raw ANSI frames to docs/. It is skipped unless ACV_SCREENSHOT=1 so that
// a normal `go test ./...` stays side-effect free.
//
//	ACV_SCREENSHOT=1 go test -run TestGenerateScreenshots
//
// hack/screenshot.sh wraps this and pipes the frames through `freeze` to get PNGs.
func TestGenerateScreenshots(t *testing.T) {
	if os.Getenv("ACV_SCREENSHOT") != "1" {
		t.Skip("set ACV_SCREENSHOT=1 to regenerate docs/ screenshots")
	}

	// A directory inside the repo rather than t.TempDir(): the per-agent parsers
	// derive the project name from path elements, so a home under /tmp or
	// /var/folders would leak those into the rendered rows.
	home, err := filepath.Abs(demoHomeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	writeDemoHome(t, home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Tests have no TTY, so lipgloss would otherwise strip every escape code.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	const width = 110

	load := func(height int) tea.Model {
		var m tea.Model = newTUI()
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m, _ = m.Update(sessionsLoadedMsg{sessions: relabelSessions(findAndSortSessions(home), home)})
		m, _ = m.Update(memoriesLoadedMsg{memories: findMemories(home)})
		m, _ = m.Update(artifactsLoadedMsg{artifacts: findArtifacts(home)})
		m, _ = m.Update(skillsLoadedMsg{skills: findSkills(home)})
		return m
	}

	// tabBar(1) + table header, rule and a spare line(3) + footer(1) frame the
	// rows, so sizing to the row count avoids a tail of empty filler lines.
	writeFrame(t, "docs/list.ansi", load(len(demoSessions())+5).View())

	// Session detail: open the third row, a multi-turn Claude session.
	d := load(20)
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyDown})
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyDown})
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	writeFrame(t, "docs/detail.ansi", d.View())

	// Content search across every agent. Run it once to count the hits, then
	// replay at a height that fits them.
	const query = "timeout"
	search := func(height int) tea.Model {
		s := load(height)
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		for _, r := range query {
			s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return s
	}
	hits := len(search(26).(tuiModel).searchHits)
	writeFrame(t, "docs/search.ansi", search(hits+5).View())
}

const (
	demoHomeDir   = ".acv-demo-home" // scratch dir, relative to the repo root
	demoHomeLabel = "/home/demo"     // what the screenshots show instead
)

// relabelSessions rewrites the scratch directory out of the paths the detail
// view prints, so the screenshots show /home/demo rather than wherever the
// repo happens to be checked out. Rewriting the loaded data (instead of the
// rendered frame) keeps the TUI's line wrapping correct.
func relabelSessions(sessions []Session, home string) []Session {
	// Claude encodes the workspace path into its project dir name with dashes.
	dashedHome := strings.ReplaceAll(home, string(os.PathSeparator), "-")
	dashedLabel := strings.ReplaceAll(demoHomeLabel, "/", "-")
	for i := range sessions {
		p := strings.ReplaceAll(sessions[i].Path, dashedHome, dashedLabel)
		sessions[i].Path = strings.ReplaceAll(p, home, demoHomeLabel)
	}
	return sessions
}

func writeFrame(t *testing.T, path, frame string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(frame+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

// ── synthetic home ───────────────────────────────────────────────────────────

// demoTurn is one user/assistant exchange in a fabricated session.
type demoTurn struct {
	user      string
	assistant string
}

// demoSession describes a fabricated session in agent-neutral terms; the
// per-agent writers below translate it into each CLI's on-disk format.
type demoSession struct {
	id      string
	project string // last path element of the fake workspace
	ago     time.Duration
	turns   []demoTurn
}

func demoSessions() []demoSession {
	return []demoSession{
		{
			id: "9f3c1a7e-2b48-4d16-9c05-7ae61d0f82b3", project: "acme-api", ago: 14 * time.Minute,
			turns: []demoTurn{
				{"The /orders endpoint returns 502 under load. Where is the timeout coming from?",
					"The upstream call in `handlers/orders.go:88` uses the default `http.Client`, which has no timeout at all. Under load the connection pool saturates and the ingress gives up first — that's your 502.\n\nTwo changes:\n\n1. Give the client an explicit `Timeout: 5 * time.Second`.\n2. Raise `MaxIdleConnsPerHost` from the default 2 to 32, so keep-alives actually get reused."},
				{"Does the retry wrapper interact badly with that?",
					"Yes. `retry.Do` in `internal/retry/retry.go:41` retries three times with no budget, so a slow upstream turns one 5s request into 15s. Pass a `context.WithTimeout` down and check `ctx.Err()` between attempts."},
			},
		},
		{
			id: "c47b2e90-51da-4f3b-8e21-6d94ac35f017", project: "infra", ago: 2*time.Hour + 20*time.Minute,
			turns: []demoTurn{
				{"Split the VPC terraform module into subnets and gateways.",
					"Done. `modules/vpc` now only owns the VPC and its flow logs; `modules/subnets` and `modules/gateways` take the VPC id as input.\n\nThe NAT gateway moved with the gateways module, so `terraform plan` shows a destroy/create for it unless you add a `moved` block — I put one in `modules/gateways/moved.tf`."},
				{"Add the moved blocks for the route tables too. The last plan also hit a state lock timeout.",
					"Added. Four `moved` blocks in `modules/subnets/moved.tf` covering the public and private route tables in both availability zones. `terraform plan` is clean now — no resource replacements.\n\nThe lock timeout was a stale lease from the cancelled run; `terraform force-unlock` cleared it."},
			},
		},
		{
			id: "1d8a6b04-93c7-4e52-a10f-2f7b845ce6d9", project: "agent-chat-viewer", ago: 5*time.Hour + 5*time.Minute,
			turns: []demoTurn{
				{"Add a --json flag so search results can be piped into jq.",
					"Added `-json` to the search path. Each hit is one object on its own line:\n\n```json\n{\"agent\":\"claude\",\"project\":\"acme-api\",\"title\":\"...\",\"snippet\":\"...\"}\n```\n\nBanners and the trailing hint are suppressed when the flag is set, so the output is valid JSONL. I also made a non-TTY stdout imply `-no-tui`, which means `acv search foo | jq` does the right thing without any flag at all."},
				{"What about the exit code when nothing matches?",
					"Still 0. grep-style semantics (exit 1 on no match) would be more useful in scripts, but it changes behaviour for anyone already piping the output — say the word and I'll switch it."},
				{"Leave it. Document the jq usage in the README instead.",
					"Added a short \"Piping to jq\" section under CLI mode with three examples: counting hits per agent, listing distinct projects, and pulling the newest match with `jq -s 'max_by(.updated)'`."},
			},
		},
		{
			id: "6e05f3c8-7a91-42bd-b3f4-05c8e1d97a2b", project: "dotfiles", ago: 26 * time.Hour,
			turns: []demoTurn{
				{"My zsh prompt should show the git branch and whether the tree is dirty.",
					"Rewrote the prompt in `zsh/prompt.zsh` using `vcs_info` instead of shelling out to `git` on every render — that was costing ~40ms per prompt in large repos.\n\nDirty state shows as a `*` after the branch name. It's computed lazily via `check-for-changes`, which you can turn off per-repo if a monorepo makes it slow."},
			},
		},
		{
			id: "b2917d4a-6c30-4e88-91af-3c7e52f08d61", project: "acme-api", ago: 2*24*time.Hour + 3*time.Hour,
			turns: []demoTurn{
				{"Port the auth middleware to the new router.",
					"Ported. The new router passes route params through the request context, so `RequireScope` no longer needs its own path parser — that dropped ~60 lines.\n\nOne behaviour change worth knowing: the old middleware ran before path normalisation, so `/Admin/users` bypassed the scope check. The new one runs after, so that hole is closed. Two tests asserted the old behaviour; I updated them."},
				{"Good catch. Is there a test for the normalisation case?",
					"There is now — `TestRequireScope_MixedCasePath` in `auth/middleware_test.go` asserts a 403 for `/Admin/users` with a non-admin token. The token introspection call still has no timeout, though — worth a follow-up."},
			},
		},
		{
			id: "48fa1c6b-d573-4a02-8b19-e6470f2cd385", project: "notes", ago: 3*24*time.Hour + 7*time.Hour,
			turns: []demoTurn{
				{"Summarise this week's meeting notes into one page grouped by project.",
					"Wrote `weekly/2026-W38.md`. Three projects came up: acme-api (migration sign-off pending on the load test), infra (the terraform split, now merged), and hiring (two onsites scheduled).\n\nThere are four action items with no owner attached in the source notes — I listed them under \"Unassigned\" rather than guessing."},
			},
		},
		{
			id: "a70e29b5-4f18-4c6d-92e3-18b4d5a07f9c", project: "agent-chat-viewer", ago: 4*24*time.Hour + 2*time.Hour,
			turns: []demoTurn{
				{"Generate an OpenAPI spec from the handler structs.",
					"Generated `openapi.yaml` from the request/response types via reflection over the handler registry.\n\nIt covers 14 of the 17 routes. The three missing ones (`/export`, `/import`, `/healthz`) take or return `any`, so there's nothing to reflect on — they need hand-written schemas or a doc comment I can parse."},
			},
		},
		{
			id: "3c6d80f2-19ab-4e75-8d04-b7f31ae62c58", project: "infra", ago: 6*24*time.Hour + 5*time.Hour,
			turns: []demoTurn{
				{"The nightly backup job fails every third run. Any pattern in the logs?",
					"Yes — every failure is a `context deadline exceeded` against the snapshot API, and they all land between 02:00 and 02:05 UTC. That's when the retention sweep runs in the same cluster.\n\nThe snapshot client has no timeout and the job has no retry, so one slow call kills the whole run. Either move the sweep to 03:00 or give the backup job a deadline plus backoff; I'd do both."},
			},
		},
	}
}

func writeDemoHome(t *testing.T, home string) {
	t.Helper()
	sessions := demoSessions()
	now := time.Now()

	// Spread the sessions across the four supported agents so the listing shows
	// all of them rather than a wall of one.
	agents := []string{"claude", "copilot", "claude", "gemini", "agy", "claude", "copilot", "gemini"}
	for i, s := range sessions {
		start := now.Add(-s.ago)
		switch agents[i%len(agents)] {
		case "claude":
			writeClaudeSession(t, home, s, start)
		case "copilot":
			writeCopilotSession(t, home, s, start)
		case "gemini":
			writeGeminiSession(t, home, s, start)
		case "agy":
			writeAgySession(t, home, s, start)
		}
	}

	writeDemoMemories(t, home)
	writeDemoArtifacts(t, home, sessions)
	writeDemoSkills(t, home)
}

// demoWorkspace is where a fabricated project "lives" on the fake machine.
func demoWorkspace(home, project string) string {
	return filepath.Join(home, project)
}

// claudeProjectDir mirrors Claude Code's encoding: the workspace's absolute
// path with '/' replaced by '-'.
func claudeProjectDir(home, project string) string {
	return strings.ReplaceAll(demoWorkspace(home, project), string(os.PathSeparator), "-")
}

func writeClaudeSession(t *testing.T, home string, s demoSession, start time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", claudeProjectDir(home, s.project))
	var b strings.Builder
	ts := start
	for i, turn := range s.turns {
		writeJSONLine(t, &b, map[string]any{
			"uuid":      fmt.Sprintf("%s-u%d", s.id, i),
			"timestamp": ts.Format(time.RFC3339),
			"message":   map[string]any{"role": "user", "content": turn.user},
		})
		ts = ts.Add(90 * time.Second)
		writeJSONLine(t, &b, map[string]any{
			"uuid":      fmt.Sprintf("%s-a%d", s.id, i),
			"timestamp": ts.Format(time.RFC3339),
			"message": map[string]any{
				"role":    "assistant",
				"content": []any{map[string]any{"type": "text", "text": turn.assistant}},
			},
		})
		ts = ts.Add(4 * time.Minute)
	}
	writeDemoFile(t, filepath.Join(dir, s.id+".jsonl"), b.String())
}

func writeCopilotSession(t *testing.T, home string, s demoSession, start time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".copilot", "session-state", s.id)
	var b strings.Builder
	ts := start
	writeJSONLine(t, &b, map[string]any{
		"id": s.id + "-start", "type": "session.start", "timestamp": ts.Format(time.RFC3339),
	})
	for i, turn := range s.turns {
		ts = ts.Add(30 * time.Second)
		writeJSONLine(t, &b, map[string]any{
			"id": fmt.Sprintf("%s-u%d", s.id, i), "type": "user.message",
			"timestamp": ts.Format(time.RFC3339),
			"data":      map[string]any{"content": turn.user},
		})
		ts = ts.Add(2 * time.Minute)
		writeJSONLine(t, &b, map[string]any{
			"id": fmt.Sprintf("%s-a%d", s.id, i), "type": "assistant.message",
			"timestamp": ts.Format(time.RFC3339),
			"data":      map[string]any{"content": turn.assistant},
		})
	}
	writeDemoFile(t, filepath.Join(dir, "events.jsonl"), b.String())
	writeDemoFile(t, filepath.Join(dir, "workspace.yaml"), fmt.Sprintf(
		"cwd: %s\ncreated_at: %s\nupdated_at: %s\n",
		demoWorkspace(home, s.project),
		start.Format(time.RFC3339), ts.Format(time.RFC3339),
	))
}

func writeGeminiSession(t *testing.T, home string, s demoSession, start time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "tmp", s.project, "chats")
	var b strings.Builder
	ts := start
	writeJSONLine(t, &b, map[string]any{"startTime": ts.Format(time.RFC3339)})
	for i, turn := range s.turns {
		ts = ts.Add(45 * time.Second)
		writeJSONLine(t, &b, map[string]any{
			"id": fmt.Sprintf("%s-u%d", s.id, i), "type": "user",
			"timestamp": ts.Format(time.RFC3339),
			"content":   []any{map[string]any{"text": turn.user}},
		})
		ts = ts.Add(3 * time.Minute)
		writeJSONLine(t, &b, map[string]any{
			"id": fmt.Sprintf("%s-a%d", s.id, i), "type": "gemini",
			"timestamp": ts.Format(time.RFC3339),
			"content":   turn.assistant,
		})
	}
	writeDemoFile(t, filepath.Join(dir, "session-"+s.id+".jsonl"), b.String())
}

func writeAgySession(t *testing.T, home string, s demoSession, start time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "brain", s.id, ".system_generated", "logs")
	var b strings.Builder
	ts := start
	for _, turn := range s.turns {
		writeJSONLine(t, &b, map[string]any{
			"type": "USER_INPUT", "created_at": ts.Format(time.RFC3339),
			"content": "<USER_REQUEST>" + turn.user + "</USER_REQUEST>",
		})
		ts = ts.Add(2 * time.Minute)
		writeJSONLine(t, &b, map[string]any{
			"type": "PLANNER_RESPONSE", "created_at": ts.Format(time.RFC3339),
			"content": turn.assistant,
		})
		ts = ts.Add(5 * time.Minute)
	}
	writeDemoFile(t, filepath.Join(dir, "transcript.jsonl"), b.String())

	// agy resolves the project name through a cache file, not the path.
	cache := map[string]string{demoWorkspace(home, s.project): s.id}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	writeDemoFile(t, filepath.Join(home, ".gemini", "antigravity-cli", "cache", "projects.json"), string(data))
}

func writeDemoMemories(t *testing.T, home string) {
	t.Helper()
	memories := []struct {
		project, name, desc, typ, body string
	}{
		{"acme-api", "load-test-before-release", "Release sign-off needs a load test run against staging", "project",
			"Every release is signed off only after a 30-minute load test against staging at 500 rps.\n\n**Why:** The 502s in the last two releases both surfaced under sustained load, not in CI.\n\n**How to apply:** Run `make loadtest ENV=staging` and attach the summary to the release PR."},
		{"acme-api", "no-panics-in-handlers", "Handlers return errors; only main may panic", "feedback",
			"HTTP handlers must return an error rather than panicking, even for \"impossible\" states.\n\n**Why:** The recover middleware turns a panic into a bare 500 with no request id, which makes incidents hard to trace.\n\n**How to apply:** Return a wrapped error and let the error middleware map it to a status code."},
		{"infra", "terraform-state-is-per-env", "State lives in one bucket per environment, never shared", "project",
			"Each environment has its own state bucket; there is no shared root module.\n\n**Why:** A single shared state made every plan lock the whole estate.\n\n**How to apply:** Run terraform from `envs/<name>/` — never from the repo root."},
		{"dotfiles", "prefers-vcs-info-over-git-shellout", "Prompt code should use vcs_info, not git subprocesses", "feedback",
			"Prompt rendering must not shell out to `git`.\n\n**Why:** Subprocess-per-prompt added ~40ms in large repos.\n\n**How to apply:** Extend the `vcs_info` format strings in `zsh/prompt.zsh` instead. See [[zsh-startup-budget]]."},
	}
	for _, m := range memories {
		dir := filepath.Join(home, ".claude", "projects", claudeProjectDir(home, m.project), "memory")
		body := fmt.Sprintf("---\nname: %s\ndescription: %s\nmetadata:\n  type: %s\n---\n\n%s\n",
			m.name, m.desc, m.typ, m.body)
		writeDemoFile(t, filepath.Join(dir, m.name+".md"), body)
	}
}

// writeDemoArtifacts drops the overflow files agents spill next to a session:
// Claude tool-result blobs and a Gemini log.
func writeDemoArtifacts(t *testing.T, home string, sessions []demoSession) {
	t.Helper()
	artifacts := []struct{ session, name, body string }{
		{sessions[0].id, "bash-go-test-orders.txt",
			"--- FAIL: TestOrders_UpstreamTimeout (5.02s)\n    orders_test.go:114: want 504, got 502\nFAIL\nexit status 1"},
		{sessions[0].id, "read-handlers-orders.txt",
			"func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {\n\tresp, err := http.DefaultClient.Do(req)\n\t...\n}"},
		{sessions[2].id, "bash-jq-search-hits.txt",
			"{\"agent\":\"claude\",\"project\":\"acme-api\",\"snippet\":\"retry.Do retries three times with no budget\"}"},
	}
	for _, a := range artifacts {
		project := "acme-api"
		if a.session == sessions[2].id {
			project = sessions[2].project
		}
		dir := filepath.Join(home, ".claude", "projects", claudeProjectDir(home, project), a.session, "tool-results")
		writeDemoFile(t, filepath.Join(dir, a.name), a.body)
	}

	writeDemoFile(t, filepath.Join(home, ".gemini", "tmp", "dotfiles", "logs.json"),
		`[{"sessionId":"6e05f3c8","type":"user","message":"reload the prompt"}]`)
}

func writeDemoSkills(t *testing.T, home string) {
	t.Helper()
	skills := []struct{ name, desc, body string }{
		{"search-prior-sessions", "Recall earlier work by searching Claude, Copilot, Gemini and agy sessions with acv.",
			"Run `acv search <terms>` before starting work that sounds familiar. Terms are AND-ed; add `-json` to pipe hits into jq."},
		{"release-checklist", "Walk the release steps for this repo: changelog, tag, load test, announcement.",
			"1. Update CHANGELOG.md\n2. Run the staging load test\n3. Tag and push\n4. Post the release note"},
	}
	for _, s := range skills {
		dir := filepath.Join(home, ".config", "agent-skills", s.name)
		writeDemoFile(t, filepath.Join(dir, "SKILL.md"), fmt.Sprintf(
			"---\nname: %s\ndescription: %s\n---\n\n%s\n", s.name, s.desc, s.body))
	}
}

func writeJSONLine(t *testing.T, b *strings.Builder, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	b.Write(data)
	b.WriteByte('\n')
}

func writeDemoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
