package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── view states ──────────────────────────────────────────────────────────────

type viewKind int

const (
	viewLoading viewKind = iota
	viewList
	viewDetail
)

type listMode int

const (
	modeNormal  listMode = iota
	modeFilter           // "/" active – filter by project/agent
	modeSearch           // "s" active – awaiting full-text query
	modeResults          // showing content-search results
)

// ── styles ───────────────────────────────────────────────────────────────────
//
// Every color that varies by theme uses AdaptiveColor{Dark, Light}.
// lipgloss resolves the correct value automatically via HasDarkBackground(),
// which bubbletea queries from the terminal at startup.

var (
	// Title bar: fixed purple band — readable on both dark and light terminals.
	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("57")).
			PaddingLeft(1).PaddingRight(1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "252", Light: "237"}).
			Background(lipgloss.AdaptiveColor{Dark: "236", Light: "253"}).
			PaddingLeft(1).PaddingRight(1)

	inputBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "226", Light: "94"}).
			Background(lipgloss.AdaptiveColor{Dark: "236", Light: "253"}).
			PaddingLeft(1).PaddingRight(1)

	detailMetaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "247", Light: "239"}).
			Background(lipgloss.AdaptiveColor{Dark: "237", Light: "254"}).
			PaddingLeft(1).PaddingRight(1)

	userHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Dark: "141", Light: "91"}) // lavender / plum

	assistantHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Dark: "221", Light: "136"}) // gold / amber

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "243", Light: "241"})

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "238", Light: "250"})

	agentAdaptiveColors = map[string]lipgloss.AdaptiveColor{
		"claude":  {Dark: "81", Light: "25"},
		"gemini":  {Dark: "214", Light: "130"},
		"copilot": {Dark: "119", Light: "28"},
	}
)

func agentColored(agent string) string {
	ac, ok := agentAdaptiveColors[agent]
	if !ok {
		return agent
	}
	return lipgloss.NewStyle().Foreground(ac).Bold(true).Render(agent)
}

// ── types ─────────────────────────────────────────────────────────────────────

type sessionsLoadedMsg struct{ sessions []Session }

type searchHit struct {
	session *Session
	snippet string
}

// ── model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	view viewKind
	mode listMode

	allSessions []Session
	sessions    []Session // list view: filtered sessions
	searchHits  []searchHit

	width  int
	height int

	table     table.Model
	textInput textinput.Model
	viewport  viewport.Model
	spinner   spinner.Model

	selected   *Session
	roleFilter string // "user" | "assistant" | ""
}

func newTUI() tuiModel {
	ti := textinput.New()
	ti.CharLimit = 200

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("205"))),
	)

	return tuiModel{
		view:      viewLoading,
		textInput: ti,
		spinner:   sp,
	}
}

// ── tea interface ─────────────────────────────────────────────────────────────

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, loadSessionsCmd())
}

func loadSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		return sessionsLoadedMsg{sessions: findAndSortSessions(home)}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.view == viewList {
			m.rebuildTable()
		}
		if m.view == viewDetail {
			m.viewport.Width = m.width
			m.viewport.Height = m.vpHeight()
			m.viewport.SetContent(m.renderMessages())
		}
		return m, nil

	case sessionsLoadedMsg:
		m.allSessions = msg.sessions
		m.sessions = msg.sessions
		m.view = viewList
		m.rebuildTable()
		return m, nil

	case spinner.TickMsg:
		if m.view == viewLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m tuiModel) View() string {
	switch m.view {
	case viewLoading:
		return m.loadingView()
	case viewList:
		return m.listView()
	case viewDetail:
		return m.detailView()
	}
	return ""
}

// ── key handling ──────────────────────────────────────────────────────────────

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewList:
		return m.handleListKey(msg)
	case viewDetail:
		return m.handleDetailKey(msg)
	}
	return m, nil
}

func (m tuiModel) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeSearch:
		return m.handleSearchKey(msg)
	default:
		return m.handleTableKey(msg)
	}
}

func (m tuiModel) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.textInput.Blur()
		m.textInput.SetValue("")
		m.sessions = m.allSessions
		m.rebuildTable()
		return m, nil
	case "enter":
		m.mode = modeNormal
		m.textInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		m.sessions = filterSessions(m.allSessions, m.textInput.Value())
		m.rebuildTable()
		return m, cmd
	}
}

func (m tuiModel) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.textInput.Blur()
		m.textInput.SetValue("")
		m.sessions = m.allSessions
		m.rebuildTable()
		return m, nil
	case "enter":
		query := m.textInput.Value()
		if query == "" {
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		}
		m.searchHits = searchContent(m.allSessions, query)
		m.mode = modeResults
		m.textInput.Blur()
		m.buildSearchTable()
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m tuiModel) handleTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		if m.mode == modeResults {
			m.mode = modeNormal
			m.sessions = m.allSessions
			m.searchHits = nil
			m.textInput.SetValue("")
			m.rebuildTable()
		}
		return m, nil

	case "/":
		m.mode = modeFilter
		m.textInput.Placeholder = "filter by project or agent…"
		cmd := m.textInput.Focus()
		return m, cmd

	case "s":
		m.mode = modeSearch
		m.textInput.Placeholder = "search message content…"
		m.textInput.SetValue("")
		cmd := m.textInput.Focus()
		return m, cmd

	case "r":
		if m.mode == modeResults {
			return m, nil // don't reload while viewing results
		}
		m.view = viewLoading
		m.mode = modeNormal
		m.textInput.SetValue("")
		m.allSessions = nil
		m.sessions = nil
		return m, tea.Batch(m.spinner.Tick, loadSessionsCmd())

	case "enter":
		cursor := m.table.Cursor()
		if m.mode == modeResults {
			if cursor >= 0 && cursor < len(m.searchHits) {
				m.openDetail(m.searchHits[cursor].session)
				return m, nil
			}
		} else {
			if cursor >= 0 && cursor < len(m.sessions) {
				m.openDetail(&m.sessions[cursor])
				return m, nil
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *tuiModel) openDetail(s *Session) {
	m.selected = s
	m.roleFilter = ""
	m.view = viewDetail
	m.viewport = viewport.New(m.width, m.vpHeight())
	m.viewport.SetContent(m.renderMessages())
}

func (m tuiModel) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.view = viewList
		m.selected = nil
		m.roleFilter = ""
		return m, nil

	case "u":
		m.roleFilter = "user"
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoTop()
		return m, nil

	case "a":
		m.roleFilter = "assistant"
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoTop()
		return m, nil

	case "0":
		m.roleFilter = ""
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoTop()
		return m, nil

	case "g":
		m.viewport.GotoTop()
		return m, nil

	case "G":
		m.viewport.GotoBottom()
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// ── views ─────────────────────────────────────────────────────────────────────

func (m tuiModel) loadingView() string {
	msg := m.spinner.View() + " scanning agent sessions…"
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
}

func (m tuiModel) listView() string {
	titleLeft := "  Agent Chat Viewer"
	var titleRight string
	switch m.mode {
	case modeResults:
		titleRight = fmt.Sprintf("%d matches  ", len(m.searchHits))
	default:
		titleRight = fmt.Sprintf("%d sessions  ", len(m.sessions))
	}
	titleBar := titleBarStyle.Width(m.width).Render(
		titleLeft + strings.Repeat(" ", max(1, m.width-len(titleLeft)-len(titleRight))) + titleRight,
	)

	tableView := m.table.View()

	var footer string
	switch m.mode {
	case modeFilter:
		label := "/ " + m.textInput.View()
		hint := dimStyle.Render("   enter: apply  esc: clear")
		footer = inputBarStyle.Width(m.width).Render(label + hint)

	case modeSearch:
		label := "s " + m.textInput.View()
		hint := dimStyle.Render("   enter: search  esc: cancel")
		footer = inputBarStyle.Width(m.width).Render(label + hint)

	case modeResults:
		query := m.textInput.Value()
		left := fmt.Sprintf(" results for %q", query)
		right := "esc: clear  enter: open  q: quit  "
		footer = statusBarStyle.Width(m.width).Render(
			left + strings.Repeat(" ", max(1, m.width-len(left)-len(right))) + right,
		)

	default:
		filterHint := ""
		if v := m.textInput.Value(); v != "" {
			filterHint = dimStyle.Render("  [" + v + "]")
		}
		left := " ↑↓: navigate  enter: open  /: filter  s: search  r: reload  q: quit"
		footer = statusBarStyle.Width(m.width).Render(left + filterHint)
	}

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, tableView, footer)
}

func (m tuiModel) detailView() string {
	s := m.selected
	if s == nil {
		return ""
	}

	// Title bar
	agentTag := agentColored(s.Agent)
	roleTag := ""
	if m.roleFilter != "" {
		roleTag = "  [" + strings.ToUpper(m.roleFilter) + " only]"
	}
	titleContent := fmt.Sprintf("  %s  %s  %s%s",
		agentTag,
		s.Project,
		dimStyle.Render(s.LastTime.Format("2006-01-02 15:04")),
		dimStyle.Render(roleTag),
	)
	titleBar := titleBarStyle.Width(m.width).Render(titleContent)

	// Meta line
	idShort := s.ID
	if len(idShort) > 14 {
		idShort = idShort[:14]
	}
	metaContent := fmt.Sprintf(" %s  ·  %s  ·  %s  ·  %d messages",
		idShort, formatSize(s.Size), s.Path, len(s.Messages))
	meta := detailMetaStyle.Width(m.width).Render(metaContent)

	// Viewport
	vpView := m.viewport.View()

	// Help bar
	pct := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)
	helpLeft := " ↑↓/jk: scroll  PgUp/PgDn: page  g/G: top/bottom  u: user only  a: assistant only  0: all  q: back"
	helpRight := pct + "  "
	help := statusBarStyle.Width(m.width).Render(
		helpLeft + strings.Repeat(" ", max(1, m.width-len(helpLeft)-len(helpRight))) + helpRight,
	)

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, meta, vpView, help)
}

// ── table helpers ─────────────────────────────────────────────────────────────

func (m *tuiModel) rebuildTable() {
	projectW := max(12, min(45, m.width-8-9-17-12-8))
	cols := []table.Column{
		{Title: "AGENT", Width: 8},
		{Title: "SIZE", Width: 9},
		{Title: "UPDATED", Width: 17},
		{Title: "PROJECT", Width: projectW},
		{Title: "ID", Width: 12},
	}
	rows := make([]table.Row, len(m.sessions))
	for i, s := range m.sessions {
		project := truncate(s.Project, projectW)
		id := truncate(s.ID, 12)
		rows[i] = table.Row{s.Agent, formatSize(s.Size), s.LastTime.Format("2006-01-02 15:04"), project, id}
	}
	m.table = buildStyledTable(cols, rows, m.tableHeight())
}

func (m *tuiModel) buildSearchTable() {
	previewW := max(20, m.width-8-20-8)
	cols := []table.Column{
		{Title: "AGENT", Width: 8},
		{Title: "PROJECT", Width: 20},
		{Title: "PREVIEW", Width: previewW},
	}
	rows := make([]table.Row, len(m.searchHits))
	for i, h := range m.searchHits {
		rows[i] = table.Row{
			h.session.Agent,
			truncate(h.session.Project, 20),
			truncate(h.snippet, previewW),
		}
	}
	m.table = buildStyledTable(cols, rows, m.tableHeight())
}

func buildStyledTable(cols []table.Column, rows []table.Row, height int) table.Model {
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(height),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Dark: "240", Light: "248"}).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Dark: "255", Light: "232"})
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	s.Cell = s.Cell.Foreground(lipgloss.AdaptiveColor{Dark: "252", Light: "237"})
	t.SetStyles(s)
	return t
}

func (m tuiModel) tableHeight() int {
	// title(1) + table-header+border(2) + footer(1) = 4
	h := m.height - 4
	if h < 1 {
		return 1
	}
	return h
}

func (m tuiModel) vpHeight() int {
	// title(1) + meta(1) + help(1) = 3
	h := m.height - 3
	if h < 1 {
		return 1
	}
	return h
}

// ── message rendering ─────────────────────────────────────────────────────────

func (m tuiModel) renderMessages() string {
	s := m.selected
	if s == nil {
		return ""
	}
	contentW := m.width - 4
	if contentW < 20 {
		contentW = 20
	}
	sep := separatorStyle.Render(strings.Repeat("─", contentW))

	var sb strings.Builder
	count := 0
	for _, msg := range s.Messages {
		if m.roleFilter != "" && msg.Role != m.roleFilter {
			continue
		}
		count++

		// Header line
		timeStr := msg.Time.Format("15:04:05")
		switch msg.Role {
		case "user":
			sb.WriteString(userHeaderStyle.Render("USER") + dimStyle.Render("  "+timeStr) + "\n")
		case "assistant":
			sb.WriteString(assistantHeaderStyle.Render("ASSISTANT") + dimStyle.Render("  "+timeStr) + "\n")
		default:
			sb.WriteString(dimStyle.Render(strings.ToUpper(msg.Role)+"  "+timeStr) + "\n")
		}
		sb.WriteString(sep + "\n")

		// Content with word-wrap and 2-space indent
		wrapped := wordWrap(msg.Content, contentW-2)
		for _, line := range strings.Split(wrapped, "\n") {
			if strings.HasPrefix(line, "[Thinking]") {
				sb.WriteString("  " + dimStyle.Render(line) + "\n")
			} else {
				sb.WriteString("  " + line + "\n")
			}
		}
		sb.WriteString("\n")
	}

	if count == 0 {
		return dimStyle.Render("  No messages to display.")
	}
	return sb.String()
}

// ── search & filter ───────────────────────────────────────────────────────────

func filterSessions(sessions []Session, query string) []Session {
	if query == "" {
		return sessions
	}
	q := strings.ToLower(query)
	var out []Session
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Project), q) ||
			strings.Contains(strings.ToLower(s.Agent), q) {
			out = append(out, s)
		}
	}
	return out
}

func searchContent(sessions []Session, query string) []searchHit {
	pattern := regexp.QuoteMeta(query)
	pattern = strings.ReplaceAll(pattern, "\\*", ".*")
	pattern = strings.ReplaceAll(pattern, "\\?", ".")
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil
	}
	var hits []searchHit
	for i := range sessions {
		s := &sessions[i]
		for _, msg := range s.Messages {
			if re.MatchString(msg.Content) {
				snippet := buildSnippet(msg.Content, re, 80)
				hits = append(hits, searchHit{session: s, snippet: snippet})
				break // one hit per session
			}
		}
	}
	return hits
}

func buildSnippet(content string, re *regexp.Regexp, maxLen int) string {
	loc := re.FindStringIndex(content)
	if loc == nil {
		content = strings.ReplaceAll(content, "\n", " ")
		return truncate(content, maxLen)
	}
	start := max(0, loc[0]-20)
	end := min(len(content), loc[1]+60)
	snippet := strings.ReplaceAll(content[start:end], "\n", " ")
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(content) {
		snippet = snippet + "…"
	}
	return truncate(snippet, maxLen)
}

// ── misc helpers ──────────────────────────────────────────────────────────────

func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var out strings.Builder
	for i, para := range strings.Split(text, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if len(para) <= width {
			out.WriteString(para)
			continue
		}
		pos := 0
		for _, word := range strings.Fields(para) {
			if pos+len(word)+1 > width && pos > 0 {
				out.WriteByte('\n')
				pos = 0
			} else if pos > 0 {
				out.WriteByte(' ')
				pos++
			}
			out.WriteString(word)
			pos += len(word)
		}
	}
	return out.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
