package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Panel focus
const (
	panelSessions = iota
	panelDetail
)

// Styles
var (
	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("69"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("69")).
			PaddingLeft(1)

	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	whiteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cyanStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	yellowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	blueStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	magentaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// TMUXGroup groups Claude sessions by tmux session
type TMUXGroup struct {
	Name     string
	Sessions []Session
}

func (g TMUXGroup) FilterValue() string { return g.Name }
func (g TMUXGroup) Title() string {
	return fmt.Sprintf("%-16s (%d)", g.Name, len(g.Sessions))
}
func (g TMUXGroup) Description() string { return "" }

type tickMsg time.Time

type model struct {
	groups      []TMUXGroup
	sessions    []Session
	sessionList list.Model
	focus       int
	detailIdx   int // selected claude session within group
	width       int
	height      int
}

func groupSessions(sessions []Session) []TMUXGroup {
	grouped := make(map[string][]Session)
	var order []string
	for _, s := range sessions {
		if _, exists := grouped[s.TMUXSession]; !exists {
			order = append(order, s.TMUXSession)
		}
		grouped[s.TMUXSession] = append(grouped[s.TMUXSession], s)
	}
	sort.Strings(order)

	groups := make([]TMUXGroup, 0, len(order))
	for _, name := range order {
		groups = append(groups, TMUXGroup{Name: name, Sessions: grouped[name]})
	}
	return groups
}

func newModel() model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	l := list.New(nil, delegate, 0, 0)
	l.Title = "TMUX Sessions"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle

	m := model{
		sessionList: l,
		focus:       panelSessions,
	}
	m.refresh()
	return m
}

func (m *model) refresh() {
	m.sessions = loadSessions()
	m.groups = groupSessions(m.sessions)

	items := make([]list.Item, len(m.groups))
	for i, g := range m.groups {
		items[i] = g
	}
	m.sessionList.SetItems(items)
	m.detailIdx = 0
}

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case tickMsg:
		cursor := m.sessionList.Index()
		m.refresh()
		if cursor < len(m.groups) {
			m.sessionList.Select(cursor)
		}
		return m, tickCmd()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.focus == panelDetail {
				m.focus = panelSessions
				return m, nil
			}
			return m, tea.Quit
		case "tab", "l":
			if m.focus == panelSessions && len(m.selectedGroup().Sessions) > 0 {
				m.focus = panelDetail
				m.detailIdx = 0
			}
			return m, nil
		case "h":
			if m.focus == panelDetail {
				m.focus = panelSessions
			}
			return m, nil
		case "enter":
			return m, m.handleEnter()
		case "r":
			cursor := m.sessionList.Index()
			m.refresh()
			if cursor < len(m.groups) {
				m.sessionList.Select(cursor)
			}
			return m, nil
		case "j", "down":
			if m.focus == panelDetail {
				g := m.selectedGroup()
				if m.detailIdx < len(g.Sessions)-1 {
					m.detailIdx++
				}
				return m, nil
			}
		case "k", "up":
			if m.focus == panelDetail {
				if m.detailIdx > 0 {
					m.detailIdx--
				}
				return m, nil
			}
		}
	}

	// Only pass to list when sessions panel is focused
	if m.focus == panelSessions {
		var cmd tea.Cmd
		m.sessionList, cmd = m.sessionList.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) selectedGroup() TMUXGroup {
	idx := m.sessionList.Index()
	if idx >= 0 && idx < len(m.groups) {
		return m.groups[idx]
	}
	return TMUXGroup{}
}

func (m model) handleEnter() tea.Cmd {
	if m.focus == panelDetail {
		g := m.selectedGroup()
		if m.detailIdx >= 0 && m.detailIdx < len(g.Sessions) {
			target := g.Sessions[m.detailIdx].PaneTarget
			if target != "" {
				switchToSession(target)
			}
		}
	} else {
		// In sessions panel, switch to first claude session in group
		g := m.selectedGroup()
		if len(g.Sessions) > 0 {
			target := g.Sessions[0].PaneTarget
			if target != "" {
				switchToSession(target)
			}
		}
	}
	return tea.Quit
}

func (m *model) updateLayout() {
	// Leave room for footer
	listH := m.height - 3
	if listH < 3 {
		listH = 3
	}
	sideW := m.sideWidth()
	m.sessionList.SetSize(sideW-2, listH)
}

func (m model) sideWidth() int {
	w := m.width / 3
	if w < 24 {
		w = 24
	}
	if w > 30 {
		w = 30
	}
	return w
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	sideW := m.sideWidth()
	detailW := m.width - sideW
	contentH := m.height - 3 // footer

	// Left panel: tmux sessions
	var leftBorder lipgloss.Style
	if m.focus == panelSessions {
		leftBorder = activeBorderStyle
	} else {
		leftBorder = borderStyle
	}
	leftPanel := leftBorder.
		Width(sideW - 2).
		Height(contentH - 2).
		Render(m.sessionList.View())

	// Right panel: detail
	var rightBorder lipgloss.Style
	if m.focus == panelDetail {
		rightBorder = activeBorderStyle
	} else {
		rightBorder = borderStyle
	}
	detailContent := m.renderDetail()
	rightPanel := rightBorder.
		Width(detailW - 2).
		Height(contentH - 2).
		Render(detailContent)

	// Compose
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	footer := footerStyle.Render(m.renderFooter())

	return body + "\n" + footer
}

func (m model) renderDetail() string {
	g := m.selectedGroup()
	if len(g.Sessions) == 0 {
		return dimStyle.Render("  No Claude sessions")
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Claude Sessions - " + g.Name))
	b.WriteString("\n\n")

	for i, s := range g.Sessions {
		selected := m.focus == panelDetail && i == m.detailIdx
		prefix := "  "
		if selected {
			prefix = cyanStyle.Render("> ")
		}

		// Session name or fallback
		label := s.Name
		if label == "" {
			label = s.Project
		}
		if selected {
			b.WriteString(prefix + whiteStyle.Bold(true).Render(label) + "\n")
		} else {
			b.WriteString(prefix + whiteStyle.Render(label) + "\n")
		}

		// Details
		indent := "    "
		b.WriteString(indent + renderModelMode(s.Model, s.Mode))
		b.WriteString("  " + dimStyle.Render(formatUptime(s.Uptime)) + "\n")

		b.WriteString(indent + dimStyle.Render("in ") + whiteStyle.Render(fmt.Sprintf("%-7s", formatTokens(s.InputTokens))))
		b.WriteString(dimStyle.Render("out ") + whiteStyle.Render(formatTokens(s.OutputTokens)) + "\n")

		b.WriteString(indent + dimStyle.Render("ctx ") + renderContextBar(s.CtxPct, 15))
		b.WriteString("  " + dimStyle.Render(fmt.Sprintf("%s / %s",
			formatTokens(s.CtxTokens), formatTokens(s.CtxLimit))) + "\n")

		b.WriteString("\n")
	}

	return b.String()
}

func renderModelMode(model, mode string) string {
	ms := dimStyle.Render(model)

	var modeStr string
	switch mode {
	case "yolo":
		modeStr = redStyle.Render(mode)
	case "plan":
		modeStr = blueStyle.Render(mode)
	case "edits":
		modeStr = greenStyle.Render(mode)
	case "auto":
		modeStr = yellowStyle.Render(mode)
	default:
		modeStr = dimStyle.Render(mode)
	}

	return ms + " " + modeStr
}

func renderContextBar(pct int, width int) string {
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	var style lipgloss.Style
	switch {
	case pct <= 30:
		style = greenStyle
	case pct <= 60:
		style = yellowStyle
	default:
		style = redStyle
	}

	return fmt.Sprintf("%s %3d%%", style.Render(bar), pct)
}

func (m model) renderFooter() string {
	total := len(m.sessions)
	if m.focus == panelDetail {
		return fmt.Sprintf(" %d session%s  j/k navigate  h back  enter switch  esc back  q quit",
			total, plural(total))
	}
	return fmt.Sprintf(" %d session%s  j/k navigate  l/tab detail  enter switch  q quit  r refresh",
		total, plural(total))
}

func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
