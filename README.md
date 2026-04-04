# claude-dashboard

A terminal dashboard for monitoring active [Claude Code](https://claude.ai/code) sessions across tmux. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

![Two-panel layout showing tmux sessions on the left and Claude session details on the right]

## Features

- Two-panel UI: tmux sessions (left) with Claude session details (right)
- Shows model, permission mode, uptime, token usage, and context window percentage
- Visual context bar with colour coding (green/yellow/red)
- Vim-style navigation (j/k, h/l, tab)
- Press Enter to switch to a session's tmux pane
- Auto-refreshes every 5 seconds
- Designed for use in tmux popups

## Data sources

Reads directly from Claude Code's own files:

- `~/.claude/sessions/*.json` -- live session metadata (pid, sessionId, cwd, name)
- `~/.claude/projects/-<path>/<sessionId>.jsonl` -- transcripts for token usage, model, mode
- `tmux list-panes` -- maps Claude processes to tmux sessions

No hooks or custom configuration required.

## Install

```bash
go install github.com/JohnPlummer/claude-dashboard@latest
```

Or build from source:

```bash
git clone https://github.com/JohnPlummer/claude-dashboard.git
cd claude-dashboard
go build -o claude-dashboard .
```

## Usage

Run directly:

```bash
claude-dashboard
```

Or bind to a tmux key (recommended):

```tmux
bind-key C display-popup -E -w 80% -h 80% "claude-dashboard"
```

## Keybindings

| Key | Action |
|-----|--------|
| `j/k` | Navigate within panel |
| `l` / `Tab` | Move to detail panel |
| `h` / `Esc` | Move back to sessions panel |
| `Enter` | Switch to selected session's tmux pane |
| `r` | Refresh |
| `q` | Quit |

## Requirements

- macOS (uses `ps` for process inspection)
- tmux
- Claude Code with active sessions
