# AI Agent Chat Viewer

A lightweight, read-only command-line tool written in Go to view chat history from various AI agent CLIs. 

This tool helps you quickly audit, search, and review past interactions without needing to open the individual agent environments.

## Supported Agents
- **Claude Code**: Extracts sessions from `~/.claude/projects/`
- **Gemini CLI**: Extracts sessions from `~/.gemini/tmp/`
- **GitHub Copilot CLI**: Extracts sessions from `~/.copilot/session-state/`

## Features
- **Auto-Discovery**: Automatically scans standard directories to find chat sessions.
- **Last Activity Tracking**: Sessions are sorted by the most recent activity by default.
- **Smart Table View**: A clean overview showing agent type, file size, start time, and last updated time.
- **Message Filtering**: Easily filter a session to see only **questions** (`user`) or only **answers** (`assistant`).
- **Global Search**: Search for terms across all sessions using simple wildcards (`*`, `?`).
- **Detailed Inspection**: View full message contents, including "thinking" blocks and timestamps.

- **Zero Dependencies**: Built entirely using the Go standard library.

## Installation

Requires Go 1.16 or higher.

```bash
# Clone the repository
git clone https://github.com/manno/agent-chat-viewer
cd agent-chat-viewer

# Build the binary
go build -o viewer main.go

# Optional: Move to your bin directory
mv viewer /usr/local/bin/
```

## Usage

### 1. List All Sessions
Running the tool without arguments lists all discovered sessions, sorted by the latest activity. By default, the start time is hidden.

```bash
# Standard listing
./viewer

# List with start times
./viewer -s
```

### 2. View a Specific Session
You can view a session by its index from the list or by providing a direct file path.

```bash
# View session with index 0
./viewer 0

# View session via direct path
./viewer ~/.gemini/tmp/my-project/chats/session-xxx.jsonl
```

### 3. Filter Messages
Append `user` or `assistant` to filter the output.

```bash
# View only your questions (user messages)
./viewer 0 user

# View only agent answers (assistant messages)
./viewer 0 assistant
```

### 4. Search All Sessions
Use the `-f` flag to search across all discovered sessions. Supports wildcards like `*`.

```bash
# Find all mentions of "FreeCAD"
./viewer -f "*FreeCAD*"

# Search for "golang" case-insensitively
./viewer -f golang
```

## Session Table Columns
- **IDX**: The index used to select a session for viewing.
- **AGENT**: The CLI agent that generated the session (claude, gemini, or copilot).
- **SIZE**: Human-readable file size of the session log.
- **START TIME**: When the session was first created (only shown with `-s` flag).
- **LAST UPDATED**: When the last message was recorded in the session.
- **PROJECT**: The project directory or workspace name.
- **ID**: Truncated session identifier (full ID is shown in detailed view).

## License
MIT
