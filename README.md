# acv — Agent Chat Viewer

A terminal UI for browsing and searching chat history from AI agent CLIs.

## Supported agents

- **Claude Code** — `~/.claude/projects/`
- **Gemini CLI** — `~/.gemini/tmp/`
- **GitHub Copilot CLI** — `~/.copilot/session-state/`
- **Google Antigravity CLI (`agy`)** — `~/.gemini/antigravity-cli/brain/`


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

## Agent Skills

The `skills/` directory contains reusable agent skill definitions (e.g. for GitHub Copilot CLI).

**Install a skill:**

```bash
# 1. Copy the skill folder into the canonical store
cp -r skills/search-prior-sessions ~/.config/agent-skills/

# 2. Sync it into each agent's skill directory (creates symlinks)
acv -sync-skills
```

`acv -sync-skills` moves any per-agent skill directories into `~/.config/agent-skills/`
and symlinks them back so all agents share the same canonical copy. You can also
run the sync interactively from the `acv` TUI (Skills tab → `s`).

- **[search-prior-sessions](skills/search-prior-sessions/SKILL.md)** — skill that teaches agents to use `acv` for recalling prior work across Copilot, Claude, Gemini, and agy sessions.

## License

MIT
