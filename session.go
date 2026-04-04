package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SessionFile represents ~/.claude/sessions/<pid>.json
type SessionFile struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	StartedAt int64  `json:"startedAt"` // milliseconds
	Name      string `json:"name"`
}

// Session is the enriched session data for display
type Session struct {
	TMUXSession  string
	Project      string
	Model        string
	Mode         string
	Name         string
	Uptime       time.Duration
	InputTokens  int64
	OutputTokens int64
	CtxTokens    int64
	CtxLimit     int64
	CtxPct       int
	PID          int
	PaneTarget   string // tmux target for switching: "session:window.pane"
}

func loadSessions() []Session {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	sessDir := filepath.Join(homeDir, ".claude", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil
	}

	paneMap := buildPaneMap()

	var sessions []Session
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		var sf SessionFile
		if err := json.Unmarshal(data, &sf); err != nil {
			continue
		}

		if !processAlive(sf.PID) {
			continue
		}

		s := Session{
			PID:     sf.PID,
			Name:    sf.Name,
			Project: filepath.Base(sf.CWD),
		}

		// Calculate uptime
		startTime := time.UnixMilli(sf.StartedAt)
		s.Uptime = time.Since(startTime)

		// Find tmux session
		s.TMUXSession, s.PaneTarget = findTMUXSession(sf.PID, paneMap)

		// Load transcript details
		projectKey := strings.ReplaceAll(sf.CWD, "/", "-")
		if strings.HasPrefix(projectKey, "-") {
			projectKey = projectKey[1:]
		}
		transcriptPath := filepath.Join(homeDir, ".claude", "projects",
			"-"+projectKey, sf.SessionID+".jsonl")
		loadTranscriptDetails(&s, transcriptPath)

		sessions = append(sessions, s)
	}

	return sessions
}

type paneInfo struct {
	tmuxSession string
	target      string // full target: "session:window.pane"
}

func buildPaneMap() map[int]paneInfo {
	m := make(map[int]paneInfo)

	out, err := exec.Command("tmux", "list-panes", "-a",
		"-F", "#{session_name}:#{window_index}.#{pane_index}:#{pane_pid}").Output()
	if err != nil {
		return m
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		pid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		m[pid] = paneInfo{
			tmuxSession: parts[0],
			target:      parts[0] + ":" + parts[1],
		}
	}

	return m
}

func findTMUXSession(pid int, paneMap map[int]paneInfo) (string, string) {
	checkPID := pid
	for i := 0; i < 5; i++ {
		if info, ok := paneMap[checkPID]; ok {
			return info.tmuxSession, info.target
		}
		ppid, err := getParentPID(checkPID)
		if err != nil || ppid <= 1 {
			break
		}
		checkPID = ppid
	}
	return "detached", ""
}

func getParentPID(pid int) (int, error) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func loadTranscriptDetails(s *Session, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var lastUsageLine string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		// Sum input/output tokens
		if idx := strings.Index(line, `"input_tokens":`); idx >= 0 {
			s.InputTokens += extractInt(line[idx:])
		}
		if idx := strings.Index(line, `"output_tokens":`); idx >= 0 {
			s.OutputTokens += extractInt(line[idx:])
		}

		// Track last usage line for context calculation
		if strings.Contains(line, `"usage"`) {
			lastUsageLine = line
		}

		// Get model (last occurrence)
		if idx := strings.Index(line, `"model":"`); idx >= 0 {
			s.Model = extractStr(line[idx:])
		}

		// Get permission mode (last occurrence)
		if idx := strings.Index(line, `"permissionMode":"`); idx >= 0 {
			s.Mode = extractStr(line[idx:])
		}
	}

	// Context from last usage
	if lastUsageLine != "" {
		var ctxInp, ctxCacheRead, ctxCacheWrite int64
		if idx := strings.Index(lastUsageLine, `"input_tokens":`); idx >= 0 {
			ctxInp = extractInt(lastUsageLine[idx:])
		}
		if idx := strings.Index(lastUsageLine, `"cache_read_input_tokens":`); idx >= 0 {
			ctxCacheRead = extractInt(lastUsageLine[idx:])
		}
		if idx := strings.Index(lastUsageLine, `"cache_creation_input_tokens":`); idx >= 0 {
			ctxCacheWrite = extractInt(lastUsageLine[idx:])
		}
		s.CtxTokens = ctxInp + ctxCacheRead + ctxCacheWrite
	}

	// Set context limit based on model
	switch {
	case strings.Contains(s.Model, "opus"):
		s.Model = "opus"
		s.CtxLimit = 1_000_000
	case strings.Contains(s.Model, "sonnet"):
		s.Model = "sonnet"
		s.CtxLimit = 1_000_000
	case strings.Contains(s.Model, "haiku"):
		s.Model = "haiku"
		s.CtxLimit = 200_000
	default:
		s.CtxLimit = 200_000
	}

	if s.CtxLimit > 0 {
		s.CtxPct = int(s.CtxTokens * 100 / s.CtxLimit)
	}

	// Normalise mode
	switch s.Mode {
	case "bypassPermissions":
		s.Mode = "yolo"
	case "acceptEdits":
		s.Mode = "edits"
	case "dontAsk":
		s.Mode = "auto"
	case "":
		s.Mode = "?"
	}
}

func extractInt(s string) int64 {
	// Find the colon, then read digits
	idx := strings.Index(s, ":")
	if idx < 0 {
		return 0
	}
	s = s[idx+1:]
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		} else if c != ' ' {
			break
		}
	}
	return n
}

func extractStr(s string) string {
	// Find opening quote after colon
	idx := strings.Index(s, `:"`)
	if idx < 0 {
		return ""
	}
	s = s[idx+2:]
	end := strings.Index(s, `"`)
	if end < 0 {
		return ""
	}
	return s[:end]
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

func switchToSession(target string) {
	if target == "" {
		return
	}
	exec.Command("tmux", "switch-client", "-t", target).Run()
}
