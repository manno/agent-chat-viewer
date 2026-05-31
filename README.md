# acv — Agent Chat Viewer

A terminal UI for browsing and searching chat history from AI agent CLIs.

## Supported agents

- **Claude Code** — `~/.claude/projects/`
- **Gemini CLI** — `~/.gemini/tmp/`
- **GitHub Copilot CLI** — `~/.copilot/session-state/`

## Installation

Requires Go 1.21+.

```bash
git clone https://github.com/manno/agent-chat-viewer
cd agent-chat-viewer
./build.sh
mv acv /usr/local/bin/
```

## Usage

### TUI (default)

```bash
acv
```

| Key | Action |
|-----|--------|
| `↑↓` / `jk` | navigate |
| `Enter` | open session |
| `/` | filter list by project or agent |
| `s` | search message content |
| `r` | reload |
| `q` | quit / go back |
| **In detail view** | |
| `u` / `a` / `0` | show user / assistant / all messages |
| `g` / `G` | jump to top / bottom |
| `PgUp` / `PgDn` | page scroll |

### CLI mode

```bash
# List sessions
acv -no-tui

# List with start times
acv -no-tui -s

# View session by index
acv 0

# Filter by role
acv 0 user
acv 0 assistant

# Search across all sessions
acv -f "*FreeCAD*"
```

## License

MIT
