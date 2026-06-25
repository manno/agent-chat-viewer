package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// isTerminal reports whether fd refers to a terminal (character device).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func main() {
	showStart   := flag.Bool("s", false, "Show start time in listing")
	searchQuery := flag.String("f", "", "Search for pattern in all sessions (supports * and ?)")
	noTUI       := flag.Bool("no-tui", false, "Disable TUI, use plain CLI output")
	showMem     := flag.Bool("memories", false, "List agent memory files")
	showFiles   := flag.Bool("files", false, "List agent artifact files (tool-results, logs)")
	agentFilter := flag.String("agent", "", "Filter by agent name (claude/gemini/copilot/agy)")
	projFilter  := flag.String("project", "", "Filter by project name (substring match)")
	regexFlag   := flag.Bool("regex", false, "Treat -f pattern as a full regex (default: literal + * ? wildcards)")
	sinceFlag   := flag.String("since", "", "Only sessions updated since (YYYY-MM-DD, RFC3339, or duration like 7d/24h)")
	untilFlag   := flag.String("until", "", "Only sessions updated before (YYYY-MM-DD, RFC3339, or duration like 7d/24h)")
	limitFlag   := flag.Int("limit", 0, "Stop after N search matches (0 = unlimited)")
	jsonFlag    := flag.Bool("json", false, "Emit one JSON object per search hit (no banners)")
	flag.Parse()

	// -json and non-TTY stdout both imply -no-tui.
	if *jsonFlag || !isTerminal(os.Stdout) {
		*noTUI = true
	}

	// Default: launch TUI when no arguments are given
	cliMode := *noTUI || *showMem || *showFiles || *searchQuery != "" ||
		flag.NArg() > 0 || *agentFilter != "" || *projFilter != ""
	if !cliMode {
		p := tea.NewProgram(newTUI(), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// CLI mode ──────────────────────────────────────────────────────────────
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	if *showMem {
		printMemories(findMemories(home), *agentFilter, *projFilter)
		return
	}

	if *showFiles {
		printArtifacts(findArtifacts(home), *agentFilter, *projFilter)
		return
	}

	since, err := parseTimeFlag(*sinceFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid -since: %v\n", err)
		os.Exit(1)
	}
	until, err := parseTimeFlag(*untilFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid -until: %v\n", err)
		os.Exit(1)
	}
	searchOpts := SearchOptions{
		Regex: *regexFlag,
		Since: since,
		Until: until,
		Limit: *limitFlag,
		JSON:  *jsonFlag,
	}

	sessions := findSessions(home)
	if *agentFilter != "" || *projFilter != "" {
		sessions = filterSessionsCLI(sessions, *agentFilter, *projFilter)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastTime.After(sessions[j].LastTime)
	})

	if *searchQuery != "" {
		runSearch(sessions, *searchQuery, searchOpts)
		return
	}

	args := flag.Args()
	if len(args) > 0 {
		arg := args[0]

		// "acv search <query>" subcommand
		if arg == "search" {
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: acv search <query>")
				os.Exit(1)
			}
			runSearch(sessions, strings.Join(args[1:], " "), searchOpts)
			return
		}

		var session *Session

		idx, idxErr := strconv.Atoi(arg)
		if idxErr == nil && idx >= 0 && idx < len(sessions) {
			s := sessions[idx]
			session, err = parseSession(s.Path)
			if session != nil {
				session.Size = s.Size
				session.LastTime = s.LastTime
			}
		} else {
			session, err = parseSession(arg)
			if session != nil {
				if info, errStat := os.Stat(arg); errStat == nil {
					session.Size = info.Size()
				}
			}
			// If arg is not a valid path or index, treat it as a search query.
			if err != nil && !strings.Contains(arg, string(os.PathSeparator)) {
				runSearch(sessions, strings.Join(args, " "), searchOpts)
				return
			}
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		filter := ""
		if len(args) > 1 {
			filter = strings.ToLower(args[1])
		}
		printSession(session, filter)
		return
	}

	// Print session list
	if *showStart {
		fmt.Printf("%-5s %-10s %-15s %-20s %-20s %-25s %-30s %-12s\n", "IDX", "AGENT", "SIZE", "START TIME", "LAST UPDATED", "PROJECT", "TITLE", "ID")
		fmt.Println(strings.Repeat("-", 170))
	} else {
		fmt.Printf("%-5s %-10s %-15s %-20s %-25s %-30s %-12s\n", "IDX", "AGENT", "SIZE", "LAST UPDATED", "PROJECT", "TITLE", "ID")
		fmt.Println(strings.Repeat("-", 145))
	}
	for i, s := range sessions {
		sizeStr := formatSize(s.Size)
		projectStr := s.Project
		if len(projectStr) > 25 {
			projectStr = projectStr[:22] + "..."
		}
		titleStr := s.Title
		if len(titleStr) > 30 {
			titleStr = titleStr[:27] + "..."
		}
		if *showStart {
			fmt.Printf("[%d] %-10s %-15s %-20s %-20s %-25s %-30s %.12s\n", i, s.Agent, sizeStr, s.StartTime.Format("2006-01-02 15:04"), s.LastTime.Format("2006-01-02 15:04"), projectStr, titleStr, s.ID)
		} else {
			fmt.Printf("[%d] %-10s %-15s %-20s %-25s %-30s %.12s\n", i, s.Agent, sizeStr, s.LastTime.Format("2006-01-02 15:04"), projectStr, titleStr, s.ID)
		}
	}
	fmt.Println("\nTo view a session: acv <index> [user|assistant]")
	fmt.Println("To search sessions: acv <query>  or  acv search <query>  or  acv -f <query>")
	fmt.Println("To show start times: acv -s")
	fmt.Println("To list memories:   acv -memories [-project NAME]")
	fmt.Println("To list artifacts:  acv -files [-agent NAME] [-project NAME]")
}

func filterSessionsCLI(sessions []Session, agent, project string) []Session {
	var out []Session
	for _, s := range sessions {
		if agent != "" && !strings.EqualFold(s.Agent, agent) {
			continue
		}
		if project != "" && !strings.Contains(strings.ToLower(s.Project), strings.ToLower(project)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// parseTimeFlag accepts an empty string, a YYYY-MM-DD date, an RFC3339
// timestamp, or a duration suffix (e.g. "7d", "24h", "2w") interpreted as
// "now minus that duration".
func parseTimeFlag(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, nil
	}
	// Duration suffix: support h/m/s natively, plus d (24h) and w (7d).
	expanded := v
	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err == nil {
			expanded = fmt.Sprintf("%dh", n*24)
		}
	} else if strings.HasSuffix(v, "w") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "w"))
		if err == nil {
			expanded = fmt.Sprintf("%dh", n*24*7)
		}
	}
	if d, err := time.ParseDuration(expanded); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q (want YYYY-MM-DD, RFC3339, or duration like 7d/24h)", v)
}
