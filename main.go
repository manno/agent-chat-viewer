package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	showStart := flag.Bool("s", false, "Show start time in listing")
	searchQuery := flag.String("f", "", "Search for pattern in all sessions (supports * and ?)")
	noTUI := flag.Bool("no-tui", false, "Disable TUI, use plain CLI output")
	flag.Parse()

	// Default: launch TUI when no arguments are given
	if flag.NArg() == 0 && *searchQuery == "" && !*noTUI {
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

	sessions := findSessions(home)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastTime.After(sessions[j].LastTime)
	})

	if *searchQuery != "" {
		runSearch(sessions, *searchQuery)
		return
	}

	args := flag.Args()
	if len(args) > 0 {
		arg := args[0]
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
		fmt.Printf("%-5s %-10s %-15s %-20s %-20s %-30s %-12s\n", "IDX", "AGENT", "SIZE", "START TIME", "LAST UPDATED", "PROJECT", "ID")
		fmt.Println(strings.Repeat("-", 140))
	} else {
		fmt.Printf("%-5s %-10s %-15s %-20s %-30s %-12s\n", "IDX", "AGENT", "SIZE", "LAST UPDATED", "PROJECT", "ID")
		fmt.Println(strings.Repeat("-", 120))
	}
	for i, s := range sessions {
		sizeStr := formatSize(s.Size)
		if *showStart {
			fmt.Printf("[%d] %-10s %-15s %-20s %-20s %-30s %.12s\n", i, s.Agent, sizeStr, s.StartTime.Format("2006-01-02 15:04"), s.LastTime.Format("2006-01-02 15:04"), s.Project, s.ID)
		} else {
			fmt.Printf("[%d] %-10s %-15s %-20s %-30s %.12s\n", i, s.Agent, sizeStr, s.LastTime.Format("2006-01-02 15:04"), s.Project, s.ID)
		}
	}
	fmt.Println("\nTo view a session: viewer <index> [user|assistant]")
	fmt.Println("To show start times: viewer -s")
}
