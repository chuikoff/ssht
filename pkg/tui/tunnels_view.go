package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/tunnel"
)

type TunnelsView struct {
	Manager       *tunnel.Manager
	Filtered      []*tunnel.Tunnel
	SelectedIndex int
	FilterInput   textinput.Model
	Filtering     bool
	StartIdx      int
	MaxRows       int
	HeaderOffset  int
	Width         int
	Height        int
}

func NewTunnelsView(m *tunnel.Manager) *TunnelsView {
	ti := textinput.New()
	ti.Placeholder = "Поиск тоннелей (название, хост, порт)..."
	ti.Prompt = "/ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(AccentColor).Bold(true)

	tv := &TunnelsView{
		Manager:      m,
		FilterInput:  ti,
		HeaderOffset: 3,
	}
	tv.ApplyFilter()
	return tv
}

func (v *TunnelsView) ApplyFilter() {
	all := v.Manager.GetTunnels()
	query := strings.ToLower(strings.TrimSpace(v.FilterInput.Value()))

	if query == "" {
		v.Filtered = all
	} else {
		var filtered []*tunnel.Tunnel
		for _, t := range all {
			if strings.Contains(strings.ToLower(t.Name), query) ||
				strings.Contains(strings.ToLower(t.Host), query) ||
				strings.Contains(strconv.Itoa(t.LocalPort), query) ||
				strings.Contains(strconv.Itoa(t.RemotePort), query) ||
				strings.Contains(strings.ToLower(string(t.Type)), query) {
				filtered = append(filtered, t)
			}
		}
		v.Filtered = filtered
	}

	if v.SelectedIndex >= len(v.Filtered) {
		v.SelectedIndex = len(v.Filtered) - 1
	}
	if v.SelectedIndex < 0 {
		v.SelectedIndex = 0
	}
}

func (v *TunnelsView) SelectedTunnel() *tunnel.Tunnel {
	if len(v.Filtered) == 0 || v.SelectedIndex < 0 || v.SelectedIndex >= len(v.Filtered) {
		return nil
	}
	return v.Filtered[v.SelectedIndex]
}

func (v *TunnelsView) HandleMouse(msg tea.MouseMsg) (int, bool) {
	if len(v.Filtered) == 0 {
		return -1, false
	}

	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if v.SelectedIndex > 0 {
				v.SelectedIndex--
			}
			return v.SelectedIndex, false

		case tea.MouseButtonWheelDown:
			if v.SelectedIndex < len(v.Filtered)-1 {
				v.SelectedIndex++
			}
			return v.SelectedIndex, false

		case tea.MouseButtonLeft:
			rowOffset := msg.Y - v.HeaderOffset
			if rowOffset >= 0 && rowOffset < v.MaxRows {
				targetIdx := v.StartIdx + rowOffset
				if targetIdx >= 0 && targetIdx < len(v.Filtered) {
					v.SelectedIndex = targetIdx
					return targetIdx, true
				}
			}
		}
	}
	return -1, false
}

func (v *TunnelsView) Update(msg tea.Msg) tea.Cmd {
	if v.Filtering {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "enter":
				v.Filtering = false
				v.FilterInput.Blur()
				return nil
			}
		}
		var cmd tea.Cmd
		v.FilterInput, cmd = v.FilterInput.Update(msg)
		v.ApplyFilter()
		return cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.SelectedIndex > 0 {
				v.SelectedIndex--
			}
		case "down", "j":
			if v.SelectedIndex < len(v.Filtered)-1 {
				v.SelectedIndex++
			}
		case "home", "g":
			v.SelectedIndex = 0
		case "end", "G":
			if len(v.Filtered) > 0 {
				v.SelectedIndex = len(v.Filtered) - 1
			}
		case "/":
			v.Filtering = true
			v.FilterInput.Focus()
			return textinput.Blink
		}
	}

	return nil
}

func (v *TunnelsView) View(width, height int) string {
	v.Width = width
	v.Height = height

	var sb strings.Builder

	v.HeaderOffset = 3
	if v.Filtering || v.FilterInput.Value() != "" {
		v.HeaderOffset = 5
		sb.WriteString(lipgloss.NewStyle().MarginBottom(1).Render(v.FilterInput.View()))
		sb.WriteString("\n")
	}

	wPrefix := 2
	wStatus := 14
	wName := 22
	wType := 10
	wHost := 20
	wPid := 8
	wRule := width - wPrefix - wStatus - wName - wType - wHost - wPid - 10
	if wRule < 20 {
		wRule = 20
	}

	thStyle := TableHeaderStyle
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top,
		thStyle.Width(wPrefix).Render(" "),
		thStyle.Width(wStatus).Render("СТАТУС"),
		thStyle.Width(wName).Render("НАЗВАНИЕ ТОННЕЛЯ"),
		thStyle.Width(wType).Render("ТИП"),
		thStyle.Width(wRule).Render("ПРАВИЛО ПРОБРОСА"),
		thStyle.Width(wHost).Render("SSH ХОСТ"),
		thStyle.Width(wPid).Render("PID"),
	)
	sb.WriteString(headerRow)
	sb.WriteString("\n")

	if len(v.Filtered) == 0 {
		emptyMsg := "Нет настроенных тоннелей. Нажмите [a], чтобы создать новый SSH тоннель."
		sb.WriteString("\n" + lipgloss.NewStyle().Foreground(TextMuted).PaddingLeft(2).Render(emptyMsg) + "\n")
		return sb.String()
	}

	maxRows := height - 10
	if maxRows < 5 {
		maxRows = 5
	}
	v.MaxRows = maxRows

	startIdx := 0
	if v.SelectedIndex >= maxRows {
		startIdx = v.SelectedIndex - maxRows + 1
	}
	endIdx := startIdx + maxRows
	if endIdx > len(v.Filtered) {
		endIdx = len(v.Filtered)
	}
	v.StartIdx = startIdx

	for i := startIdx; i < endIdx; i++ {
		t := v.Filtered[i]
		isSelected := i == v.SelectedIndex

		statusBadge := StatusInactiveStyle.Render()
		switch t.Status {
		case tunnel.StatusActive:
			statusBadge = StatusActiveStyle.Render()
		case tunnel.StatusStarting:
			statusBadge = StatusStartingStyle.Render()
		case tunnel.StatusReconnecting:
			statusBadge = StatusReconnectingStyle.Render()
		case tunnel.StatusFailed:
			statusBadge = StatusFailedStyle.Render()
		}

		name := t.Name
		tunType := strings.ToUpper(string(t.Type))
		rule := t.Summary()
		host := t.Host

		pidStr := "-"
		if t.PID > 0 {
			pidStr = fmt.Sprintf("%d", t.PID)
		}

		prefix := "  "
		if isSelected {
			prefix = "> "
		}

		baseStyle := TableRowNormalStyle
		if isSelected {
			baseStyle = TableRowSelectedStyle
		}

		rowContent := lipgloss.JoinHorizontal(lipgloss.Top,
			baseStyle.Width(wPrefix).Render(prefix),
			baseStyle.Width(wStatus).Render(statusBadge),
			baseStyle.Width(wName).MaxWidth(wName).Render(name),
			baseStyle.Width(wType).MaxWidth(wType).Render(tunType),
			baseStyle.Width(wRule).MaxWidth(wRule).Render(rule),
			baseStyle.Width(wHost).MaxWidth(wHost).Render(host),
			baseStyle.Width(wPid).MaxWidth(wPid).Render(pidStr),
		)

		sb.WriteString(rowContent)
		sb.WriteString("\n")
	}

	allTunnels := v.Manager.GetTunnels()
	activeCount := v.Manager.ActiveCount()
	footerCount := fmt.Sprintf(
		"Показано %d из %d тоннелей | Активно: %d | Остановлено: %d",
		len(v.Filtered), len(allTunnels), activeCount, len(allTunnels)-activeCount,
	)
	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(SubtleColor).PaddingLeft(2).Render(footerCount))

	return sb.String()
}
