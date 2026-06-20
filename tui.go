package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	viewMemories
	viewMemoryDetail
	viewArtifacts
	viewArtifactDetail
)

type listMode int

const (
	modeNormal  listMode = iota
	modeFilter           // "/" active – filter by project/agent
	modeSearch           // "s" active – awaiting full-text query
	modeResults          // showing content-search results
)

// ── styles ───────────────────────────────────────────────────────────────────

var (
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
			Foreground(lipgloss.AdaptiveColor{Dark: "141", Light: "91"})

	assistantHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Dark: "221", Light: "136"})

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "243", Light: "241"})

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Dark: "238", Light: "250"})

	tabBarBgStyle = lipgloss.NewStyle().
			Background(lipgloss.AdaptiveColor{Dark: "234", Light: "252"})

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("57")).
			PaddingLeft(1).PaddingRight(1)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Dark: "244", Light: "244"}).
				Background(lipgloss.AdaptiveColor{Dark: "234", Light: "252"}).
				PaddingLeft(1).PaddingRight(1)

	memTypeStyles = map[string]lipgloss.Style{
		"user":      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "81", Light: "25"}).Bold(true),
		"feedback":  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "221", Light: "130"}).Bold(true),
		"project":   lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "119", Light: "28"}).Bold(true),
		"reference": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "51", Light: "31"}).Bold(true),
	}

	kindStyles = map[string]lipgloss.Style{
		"tool-result": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "214", Light: "130"}).Bold(true),
		"gemini-log":  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "177", Light: "97"}).Bold(true),
	}

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

func renderMemTypeTag(t string) string {
	if s, ok := memTypeStyles[t]; ok {
		return s.Render(t)
	}
	return dimStyle.Render(t)
}

func renderKindTag(k string) string {
	if s, ok := kindStyles[k]; ok {
		return s.Render(k)
	}
	return dimStyle.Render(k)
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 02")
	}
}

func relTimeColored(t time.Time) string {
	s := relTime(t)
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "119", Light: "28"}).Render(s)
	case d < 24*time.Hour:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "221", Light: "130"}).Render(s)
	default:
		return dimStyle.Render(s)
	}
}

// ── types ─────────────────────────────────────────────────────────────────────

type sessionsLoadedMsg struct{ sessions []Session }
type memoriesLoadedMsg struct{ memories []MemoryFile }
type artifactsLoadedMsg struct{ artifacts []Artifact }

type searchHit struct {
	session *Session
	snippet string
}

// ── model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	view viewKind
	mode listMode

	// sessions
	allSessions []Session
	sessions    []Session
	searchHits  []searchHit

	// memories
	memories        []MemoryFile
	memoriesLoaded  bool
	filtMemories    []MemoryFile
	selectedMemory  *MemoryFile

	// artifacts
	artifacts        []Artifact
	artifactsLoaded  bool
	filtArtifacts    []Artifact
	selectedArtifact *Artifact

	width  int
	height int

	table     table.Model
	textInput textinput.Model
	viewport  viewport.Model
	spinner   spinner.Model

	selected   *Session
	roleFilter string
	saveStatus string
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

func loadMemoriesCmd() tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		return memoriesLoadedMsg{memories: findMemories(home)}
	}
}

func loadArtifactsCmd() tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		return artifactsLoadedMsg{artifacts: findArtifacts(home)}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		switch m.view {
		case viewList:
			m.rebuildTable()
		case viewDetail:
			m.viewport.Width = m.width
			m.viewport.Height = m.vpHeight()
			m.viewport.SetContent(m.renderMessages())
		case viewMemories:
			m.rebuildMemoriesTable()
		case viewMemoryDetail:
			m.viewport.Width = m.width
			m.viewport.Height = m.vpHeight()
			m.viewport.SetContent(m.renderMemory())
		case viewArtifacts:
			m.rebuildArtifactsTable()
		case viewArtifactDetail:
			m.viewport.Width = m.width
			m.viewport.Height = m.vpHeight()
			m.viewport.SetContent(m.renderArtifact())
		}
		return m, nil

	case sessionsLoadedMsg:
		m.allSessions = msg.sessions
		m.sessions = msg.sessions
		m.view = viewList
		m.rebuildTable()
		return m, nil

	case memoriesLoadedMsg:
		m.memories = msg.memories
		m.memoriesLoaded = true
		m.filtMemories = msg.memories
		if m.view == viewMemories {
			m.rebuildMemoriesTable()
		}
		return m, nil

	case artifactsLoadedMsg:
		m.artifacts = msg.artifacts
		m.artifactsLoaded = true
		m.filtArtifacts = msg.artifacts
		if m.view == viewArtifacts {
			m.rebuildArtifactsTable()
		}
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
	case viewMemories:
		return m.memoriesView()
	case viewMemoryDetail:
		return m.memoryDetailView()
	case viewArtifacts:
		return m.artifactsView()
	case viewArtifactDetail:
		return m.artifactDetailView()
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
	case viewMemories:
		return m.handleMemoriesKey(msg)
	case viewMemoryDetail:
		return m.handleMemoryDetailKey(msg)
	case viewArtifacts:
		return m.handleArtifactsKey(msg)
	case viewArtifactDetail:
		return m.handleArtifactDetailKey(msg)
	}
	return m, nil
}

// handleResourceKey processes 1/2/3 resource-switching keys from any list view.
// Returns (model, cmd, handled); when handled is false the caller should continue.
func (m tuiModel) handleResourceKey(key string) (tuiModel, tea.Cmd, bool) {
	switch key {
	case "1":
		if m.view == viewList {
			return m, nil, true
		}
		m.view = viewList
		m.mode = modeNormal
		m.textInput.SetValue("")
		m.sessions = m.allSessions
		m.rebuildTable()
		return m, nil, true

	case "2":
		if m.view == viewMemories {
			return m, nil, true
		}
		m.view = viewMemories
		m.mode = modeNormal
		m.textInput.SetValue("")
		if m.memoriesLoaded {
			m.filtMemories = m.memories
			m.rebuildMemoriesTable()
			return m, nil, true
		}
		return m, loadMemoriesCmd(), true

	case "3":
		if m.view == viewArtifacts {
			return m, nil, true
		}
		m.view = viewArtifacts
		m.mode = modeNormal
		m.textInput.SetValue("")
		if m.artifactsLoaded {
			m.filtArtifacts = m.artifacts
			m.rebuildArtifactsTable()
			return m, nil, true
		}
		return m, loadArtifactsCmd(), true
	}
	return m, nil, false
}

// ── sessions list keys ────────────────────────────────────────────────────────

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
		m.resetFilter()
		return m, nil
	case "enter":
		m.mode = modeNormal
		m.textInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		m.applyFilter(m.textInput.Value())
		return m, cmd
	}
}

func (m *tuiModel) resetFilter() {
	switch m.view {
	case viewList:
		m.sessions = m.allSessions
		m.rebuildTable()
	case viewMemories:
		m.filtMemories = m.memories
		m.rebuildMemoriesTable()
	case viewArtifacts:
		m.filtArtifacts = m.artifacts
		m.rebuildArtifactsTable()
	}
}

func (m *tuiModel) applyFilter(q string) {
	switch m.view {
	case viewList:
		m.sessions = filterSessions(m.allSessions, q)
		m.rebuildTable()
	case viewMemories:
		m.filtMemories = filterMemories(m.memories, q)
		m.rebuildMemoriesTable()
	case viewArtifacts:
		m.filtArtifacts = filterArtifacts(m.artifacts, q)
		m.rebuildArtifactsTable()
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
	if mm, cmd, handled := m.handleResourceKey(msg.String()); handled {
		return mm, cmd
	}

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
			return m, nil
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
			}
		} else {
			if cursor >= 0 && cursor < len(m.sessions) {
				m.openDetail(&m.sessions[cursor])
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
		m.saveStatus = ""
		return m, nil

	case "s":
		path, err := m.saveCurrentSession()
		if err != nil {
			m.saveStatus = "error: " + err.Error()
		} else {
			m.saveStatus = "saved → " + path
		}
		return m, nil

	case "u":
		m.saveStatus = ""
		m.roleFilter = "user"
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoTop()
		return m, nil

	case "a":
		m.saveStatus = ""
		m.roleFilter = "assistant"
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoTop()
		return m, nil

	case "0":
		m.saveStatus = ""
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

// ── memories keys ─────────────────────────────────────────────────────────────

func (m tuiModel) handleMemoriesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeFilter {
		return m.handleFilterKey(msg)
	}
	return m.handleMemoriesTableKey(msg)
}

func (m tuiModel) handleMemoriesTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if mm, cmd, handled := m.handleResourceKey(msg.String()); handled {
		return mm, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "/":
		m.mode = modeFilter
		m.textInput.Placeholder = "filter memories…"
		m.textInput.SetValue("")
		cmd := m.textInput.Focus()
		return m, cmd

	case "r":
		m.memoriesLoaded = false
		m.memories = nil
		m.filtMemories = nil
		return m, loadMemoriesCmd()

	case "enter":
		cursor := m.table.Cursor()
		if cursor >= 0 && cursor < len(m.filtMemories) {
			m.openMemoryDetail(&m.filtMemories[cursor])
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *tuiModel) openMemoryDetail(mem *MemoryFile) {
	m.selectedMemory = mem
	m.view = viewMemoryDetail
	m.viewport = viewport.New(m.width, m.vpHeight())
	m.viewport.SetContent(m.renderMemory())
}

func (m tuiModel) handleMemoryDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.view = viewMemories
		m.selectedMemory = nil
		m.saveStatus = ""
		return m, nil
	case "s":
		path, err := m.saveCurrentMemory()
		if err != nil {
			m.saveStatus = "error: " + err.Error()
		} else {
			m.saveStatus = "saved → " + path
		}
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

// ── artifacts keys ────────────────────────────────────────────────────────────

func (m tuiModel) handleArtifactsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeFilter {
		return m.handleFilterKey(msg)
	}
	return m.handleArtifactsTableKey(msg)
}

func (m tuiModel) handleArtifactsTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if mm, cmd, handled := m.handleResourceKey(msg.String()); handled {
		return mm, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "/":
		m.mode = modeFilter
		m.textInput.Placeholder = "filter artifacts…"
		m.textInput.SetValue("")
		cmd := m.textInput.Focus()
		return m, cmd

	case "r":
		m.artifactsLoaded = false
		m.artifacts = nil
		m.filtArtifacts = nil
		return m, loadArtifactsCmd()

	case "enter":
		cursor := m.table.Cursor()
		if cursor >= 0 && cursor < len(m.filtArtifacts) {
			m.openArtifactDetail(&m.filtArtifacts[cursor])
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *tuiModel) openArtifactDetail(a *Artifact) {
	m.selectedArtifact = a
	m.view = viewArtifactDetail
	m.viewport = viewport.New(m.width, m.vpHeight())
	m.viewport.SetContent(m.renderArtifact())
}

func (m tuiModel) handleArtifactDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.view = viewArtifacts
		m.selectedArtifact = nil
		m.saveStatus = ""
		return m, nil
	case "s":
		path, err := m.saveCurrentArtifact()
		if err != nil {
			m.saveStatus = "error: " + err.Error()
		} else {
			m.saveStatus = "saved → " + path
		}
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

// ── tab bar ───────────────────────────────────────────────────────────────────

func (m tuiModel) renderTabBar() string {
	sessCount := fmt.Sprintf("%d", len(m.allSessions))

	memCount := "…"
	if m.memoriesLoaded {
		memCount = fmt.Sprintf("%d", len(m.memories))
	}

	artCount := "…"
	if m.artifactsLoaded {
		artCount = fmt.Sprintf("%d", len(m.artifacts))
	}

	isSess := m.view == viewList || m.view == viewDetail
	isMem := m.view == viewMemories || m.view == viewMemoryDetail
	isArt := m.view == viewArtifacts || m.view == viewArtifactDetail

	renderTab := func(label, count string, active bool) string {
		text := label + " " + count
		if active {
			return tabActiveStyle.Render(text)
		}
		return tabInactiveStyle.Render(text)
	}

	tabs := renderTab("1 Sessions", sessCount, isSess) +
		"  " + renderTab("2 Memories", memCount, isMem) +
		"  " + renderTab("3 Artifacts", artCount, isArt)

	return tabBarBgStyle.Width(m.width).Render(tabs)
}

// ── views ─────────────────────────────────────────────────────────────────────

func (m tuiModel) loadingView() string {
	msg := m.spinner.View() + " scanning agent sessions…"
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
}

func (m tuiModel) listView() string {
	tabBar := m.renderTabBar()
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
		left := fmt.Sprintf(" results for %q  %d matches", query, len(m.searchHits))
		right := "esc: clear  enter: open  q: quit  "
		footer = statusBarStyle.Width(m.width).Render(
			left + strings.Repeat(" ", max(1, m.width-len(left)-len(right))) + right,
		)

	default:
		filterHint := ""
		if v := m.textInput.Value(); v != "" {
			filterHint = dimStyle.Render("  [" + v + "]")
		}
		left := " ↑↓jk: nav  enter: open  /: filter  s: search  r: reload  1/2/3: switch  q: quit"
		footer = statusBarStyle.Width(m.width).Render(left + filterHint)
	}

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, tableView, footer)
}

func (m tuiModel) memoriesView() string {
	tabBar := m.renderTabBar()

	var body string
	if !m.memoriesLoaded {
		body = lipgloss.NewStyle().
			Width(m.width).Height(m.tableHeight()+2).
			Render(dimStyle.Render("\n  Scanning memories…"))
	} else if len(m.filtMemories) == 0 {
		msg := "  No memory files found."
		if m.textInput.Value() != "" {
			msg = fmt.Sprintf("  No matches for %q.", m.textInput.Value())
		}
		body = lipgloss.NewStyle().Width(m.width).Height(m.tableHeight()+2).Render(dimStyle.Render("\n" + msg))
	} else {
		body = m.table.View()
	}

	var footer string
	if m.mode == modeFilter {
		label := "/ " + m.textInput.View()
		hint := dimStyle.Render("   enter: apply  esc: clear")
		footer = inputBarStyle.Width(m.width).Render(label + hint)
	} else {
		left := " ↑↓jk: nav  enter: open  /: filter  r: rescan  1/2/3: switch  q: quit"
		footer = statusBarStyle.Width(m.width).Render(left)
	}

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, body, footer)
}

func (m tuiModel) artifactsView() string {
	tabBar := m.renderTabBar()

	var body string
	if !m.artifactsLoaded {
		body = lipgloss.NewStyle().
			Width(m.width).Height(m.tableHeight()+2).
			Render(dimStyle.Render("\n  Scanning artifacts…"))
	} else if len(m.filtArtifacts) == 0 {
		msg := "  No artifact files found."
		if m.textInput.Value() != "" {
			msg = fmt.Sprintf("  No matches for %q.", m.textInput.Value())
		}
		body = lipgloss.NewStyle().Width(m.width).Height(m.tableHeight()+2).Render(dimStyle.Render("\n" + msg))
	} else {
		body = m.table.View()
	}

	var footer string
	if m.mode == modeFilter {
		label := "/ " + m.textInput.View()
		hint := dimStyle.Render("   enter: apply  esc: clear")
		footer = inputBarStyle.Width(m.width).Render(label + hint)
	} else {
		left := " ↑↓jk: nav  enter: open  /: filter  r: rescan  1/2/3: switch  q: quit"
		footer = statusBarStyle.Width(m.width).Render(left)
	}

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, body, footer)
}

func (m tuiModel) detailView() string {
	s := m.selected
	if s == nil {
		return ""
	}

	tabBar := m.renderTabBar()

	roleTag := ""
	if m.roleFilter != "" {
		roleTag = "  [" + strings.ToUpper(m.roleFilter) + " only]"
	}
	titleStr := s.Title
	maxTitleLen := m.width - 45
	if maxTitleLen > 10 {
		titleStr = truncate(titleStr, maxTitleLen)
	}
	titleContent := fmt.Sprintf("  %s  %s  %s  %s%s",
		agentColored(s.Agent),
		s.Project,
		lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("81")).Render(titleStr),
		dimStyle.Render(s.LastTime.Format("2006-01-02 15:04")),
		dimStyle.Render(roleTag),
	)
	titleBar := titleBarStyle.Width(m.width).Render(titleContent)

	idShort := s.ID
	if len(idShort) > 14 {
		idShort = idShort[:14]
	}
	metaContent := fmt.Sprintf(" %s  ·  %s  ·  %s  ·  %d messages",
		idShort, formatSize(s.Size), s.Path, len(s.Messages))
	meta := detailMetaStyle.Width(m.width).Render(metaContent)

	vpView := m.viewport.View()

	pct := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)
	helpLeft := " ↑↓/jk: scroll  PgUp/PgDn: page  g/G: top/bottom  u: user  a: assistant  0: all  s: save  q: back"
	if m.saveStatus != "" {
		helpLeft = "  " + m.saveStatus
	}
	helpRight := pct + "  "
	help := statusBarStyle.Width(m.width).Render(
		helpLeft + strings.Repeat(" ", max(1, m.width-len(helpLeft)-len(helpRight))) + helpRight,
	)

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, titleBar, meta, vpView, help)
}

func (m tuiModel) memoryDetailView() string {
	mem := m.selectedMemory
	if mem == nil {
		return ""
	}

	tabBar := m.renderTabBar()

	typeTag := renderMemTypeTag(mem.Type)
	titleContent := fmt.Sprintf("  MEMORY  %s  %s", mem.Name, typeTag)
	titleBar := titleBarStyle.Width(m.width).Render(titleContent)

	metaContent := fmt.Sprintf(" %s  ·  %s  ·  %s",
		mem.Project, formatSize(mem.Size), dimStyle.Render(mem.Path))
	meta := detailMetaStyle.Width(m.width).Render(metaContent)

	vpView := m.viewport.View()

	pct := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)
	helpLeft := " ↑↓/jk: scroll  PgUp/PgDn: page  g/G: top/bottom  s: save  q: back"
	if m.saveStatus != "" {
		helpLeft = "  " + m.saveStatus
	}
	helpRight := pct + "  "
	help := statusBarStyle.Width(m.width).Render(
		helpLeft + strings.Repeat(" ", max(1, m.width-len(helpLeft)-len(helpRight))) + helpRight,
	)

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, titleBar, meta, vpView, help)
}

func (m tuiModel) artifactDetailView() string {
	a := m.selectedArtifact
	if a == nil {
		return ""
	}

	tabBar := m.renderTabBar()

	kindTag := renderKindTag(a.Kind)
	titleContent := fmt.Sprintf("  ARTIFACT  %s  %s", kindTag, a.Name)
	titleBar := titleBarStyle.Width(m.width).Render(titleContent)

	session := a.SessionID
	if session == "" {
		session = "project-level"
	} else if len(session) > 13 {
		session = session[:13]
	}
	metaContent := fmt.Sprintf(" %s  ·  %s  ·  %s  ·  %s",
		agentColored(a.Agent), a.Project, session, formatSize(a.Size))
	meta := detailMetaStyle.Width(m.width).Render(metaContent)

	vpView := m.viewport.View()

	pct := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)
	helpLeft := " ↑↓/jk: scroll  PgUp/PgDn: page  g/G: top/bottom  s: save  q: back"
	if m.saveStatus != "" {
		helpLeft = "  " + m.saveStatus
	}
	helpRight := pct + "  "
	help := statusBarStyle.Width(m.width).Render(
		helpLeft + strings.Repeat(" ", max(1, m.width-len(helpLeft)-len(helpRight))) + helpRight,
	)

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, titleBar, meta, vpView, help)
}

// ── table helpers ─────────────────────────────────────────────────────────────

func (m *tuiModel) rebuildTable() {
	remaining := m.width - 60
	var projectW, titleW int
	if remaining < 22 {
		projectW = 12
		titleW = 10
	} else {
		projectW = max(12, min(30, remaining*4/10))
		titleW = remaining - projectW
	}

	cols := []table.Column{
		{Title: "AGENT", Width: 8},
		{Title: "SIZE", Width: 9},
		{Title: "UPDATED", Width: 17},
		{Title: "PROJECT", Width: projectW},
		{Title: "TITLE", Width: titleW},
		{Title: "ID", Width: 12},
	}
	rows := make([]table.Row, len(m.sessions))
	for i, s := range m.sessions {
		project := truncate(s.Project, projectW)
		title := truncate(s.Title, titleW)
		id := truncate(s.ID, 12)
		rows[i] = table.Row{s.Agent, formatSize(s.Size), s.LastTime.Format("2006-01-02 15:04"), project, title, id}
	}
	m.table = buildStyledTable(cols, rows, m.tableHeight(), m.width-2)
}

func (m *tuiModel) buildSearchTable() {
	previewW := max(20, m.width-36)
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
	m.table = buildStyledTable(cols, rows, m.tableHeight(), m.width-2)
}

func (m *tuiModel) rebuildMemoriesTable() {
	descW := max(20, m.width-67)
	cols := []table.Column{
		{Title: "TYPE", Width: 12},
		{Title: "PROJECT", Width: 15},
		{Title: "NAME", Width: 18},
		{Title: "DESC", Width: descW},
		{Title: "MODIFIED", Width: 10},
	}
	rows := make([]table.Row, len(m.filtMemories))
	for i, mem := range m.filtMemories {
		rows[i] = table.Row{
			renderMemTypeTag(mem.Type),
			truncate(mem.Project, 15),
			truncate(mem.Name, 18),
			truncate(mem.Desc, descW),
			relTimeColored(mem.ModTime),
		}
	}
	m.table = buildStyledTable(cols, rows, m.tableHeight(), m.width-2)
}

func (m *tuiModel) rebuildArtifactsTable() {
	nameW := max(10, m.width-84)
	cols := []table.Column{
		{Title: "AGENT", Width: 8},
		{Title: "PROJECT", Width: 15},
		{Title: "SESSION", Width: 14},
		{Title: "KIND", Width: 12},
		{Title: "NAME", Width: nameW},
		{Title: "SIZE", Width: 9},
		{Title: "MODIFIED", Width: 10},
	}
	rows := make([]table.Row, len(m.filtArtifacts))
	for i, a := range m.filtArtifacts {
		sessionShort := a.SessionID
		if len(sessionShort) > 13 {
			sessionShort = sessionShort[:13]
		}
		rows[i] = table.Row{
			agentColored(a.Agent),
			truncate(a.Project, 15),
			sessionShort,
			renderKindTag(a.Kind),
			truncate(a.Name, nameW),
			formatSize(a.Size),
			relTimeColored(a.ModTime),
		}
	}
	m.table = buildStyledTable(cols, rows, m.tableHeight(), m.width-2)
}

func buildStyledTable(cols []table.Column, rows []table.Row, height int, width int) table.Model {
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(height),
		table.WithWidth(width),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Dark: "240", Light: "248"}).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Dark: "255", Light: "232"})
	s.Selected = s.Selected.
		Foreground(lipgloss.AdaptiveColor{Dark: "229", Light: "235"}).
		Background(lipgloss.AdaptiveColor{Dark: "57", Light: "189"}).
		Bold(false)
	s.Cell = s.Cell.Foreground(lipgloss.AdaptiveColor{Dark: "252", Light: "237"})
	t.SetStyles(s)
	return t
}

func (m tuiModel) tableHeight() int {
	// tabBar(1) + table-header+border(2) + footer(1) = 4
	h := m.height - 4
	if h < 1 {
		return 1
	}
	return h
}

func (m tuiModel) vpHeight() int {
	// tabBar(1) + titleBar(1) + meta(1) + help(1) = 4
	h := m.height - 4
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

func (m tuiModel) renderMemory() string {
	mem := m.selectedMemory
	if mem == nil {
		return ""
	}
	contentW := m.width - 4
	if contentW < 20 {
		contentW = 20
	}
	sep := separatorStyle.Render(strings.Repeat("─", contentW))

	var sb strings.Builder
	sb.WriteString(dimStyle.Render("  name:        ") + mem.Name + "\n")
	sb.WriteString(dimStyle.Render("  type:        ") + renderMemTypeTag(mem.Type) + "\n")
	sb.WriteString(dimStyle.Render("  description: ") + mem.Desc + "\n")
	sb.WriteString(dimStyle.Render("  project:     ") + mem.Project + "\n")
	sb.WriteString(dimStyle.Render("  modified:    ") + relTimeColored(mem.ModTime) +
		dimStyle.Render("  ("+mem.ModTime.Format("2006-01-02 15:04")+")") + "\n")
	sb.WriteString("\n")
	sb.WriteString(sep + "\n\n")

	wrapped := wordWrap(mem.Content, contentW-2)
	for _, line := range strings.Split(wrapped, "\n") {
		sb.WriteString("  " + line + "\n")
	}
	return sb.String()
}

func (m tuiModel) renderArtifact() string {
	a := m.selectedArtifact
	if a == nil {
		return ""
	}
	contentW := m.width - 4
	if contentW < 20 {
		contentW = 20
	}
	sep := separatorStyle.Render(strings.Repeat("─", contentW))

	data, err := os.ReadFile(a.Path)
	var content string
	if err != nil {
		content = fmt.Sprintf("Error reading file: %v", err)
	} else {
		const maxDisplay = 100 * 1024
		if len(data) > maxDisplay {
			content = string(data[:maxDisplay]) + "\n\n[… truncated at 100 KB …]"
		} else {
			content = string(data)
		}
	}

	var sb strings.Builder
	sb.WriteString(sep + "\n\n")
	wrapped := wordWrap(content, contentW-2)
	for _, line := range strings.Split(wrapped, "\n") {
		sb.WriteString("  " + line + "\n")
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
			strings.Contains(strings.ToLower(s.Agent), q) ||
			strings.Contains(strings.ToLower(s.Title), q) {
			out = append(out, s)
		}
	}
	return out
}

func filterMemories(memories []MemoryFile, query string) []MemoryFile {
	if query == "" {
		return memories
	}
	q := strings.ToLower(query)
	var out []MemoryFile
	for _, m := range memories {
		if strings.Contains(strings.ToLower(m.Project), q) ||
			strings.Contains(strings.ToLower(m.Name), q) ||
			strings.Contains(strings.ToLower(m.Type), q) ||
			strings.Contains(strings.ToLower(m.Desc), q) {
			out = append(out, m)
		}
	}
	return out
}

func filterArtifacts(artifacts []Artifact, query string) []Artifact {
	if query == "" {
		return artifacts
	}
	q := strings.ToLower(query)
	var out []Artifact
	for _, a := range artifacts {
		if strings.Contains(strings.ToLower(a.Agent), q) ||
			strings.Contains(strings.ToLower(a.Project), q) ||
			strings.Contains(strings.ToLower(a.Kind), q) ||
			strings.Contains(strings.ToLower(a.Name), q) {
			out = append(out, a)
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
				break
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

// ── save helpers ──────────────────────────────────────────────────────────────

func filenameSafe(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := regexp.MustCompile(`-+`).ReplaceAllString(b.String(), "-")
	return strings.Trim(result, "-")
}

func (m tuiModel) saveCurrentSession() (string, error) {
	s := m.selected
	if s == nil {
		return "", fmt.Errorf("no session selected")
	}
	project := filenameSafe(filepath.Base(s.Project))
	if project == "" {
		project = "unknown"
	}
	ts := s.LastTime.Format("2006-01-02_150405")
	name := fmt.Sprintf("session-%s-%s-%s.md", s.Agent, project, ts)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Session: %s / %s - %s\n\n", s.Agent, s.Project, s.Title))
	sb.WriteString(fmt.Sprintf("- **Date:** %s\n", s.LastTime.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("- **ID:** %s\n", s.ID))
	sb.WriteString(fmt.Sprintf("- **Messages:** %d\n\n", len(s.Messages)))

	sep := strings.Repeat("─", 80)
	for _, msg := range s.Messages {
		timeStr := msg.Time.Format("15:04:05")
		sb.WriteString(fmt.Sprintf("## %s  %s\n\n", strings.ToUpper(msg.Role), timeStr))
		sb.WriteString(sep + "\n\n")
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	if err := os.WriteFile(name, []byte(sb.String()), 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func (m tuiModel) saveCurrentMemory() (string, error) {
	mem := m.selectedMemory
	if mem == nil {
		return "", fmt.Errorf("no memory selected")
	}
	base := filenameSafe(mem.Name)
	if base == "" {
		base = filenameSafe(filepath.Base(mem.Path))
	}
	name := fmt.Sprintf("memory-%s.md", base)

	data, err := os.ReadFile(mem.Path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func (m tuiModel) saveCurrentArtifact() (string, error) {
	a := m.selectedArtifact
	if a == nil {
		return "", fmt.Errorf("no artifact selected")
	}
	ext := filepath.Ext(a.Name)
	base := filenameSafe(strings.TrimSuffix(a.Name, ext))
	if base == "" {
		base = "artifact"
	}
	name := fmt.Sprintf("artifact-%s%s", base, ext)

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return "", err
	}
	return name, nil
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
