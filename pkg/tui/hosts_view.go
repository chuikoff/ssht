package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/sshconfig"
)

type HostFilterMode int

const (
	FilterActiveOnly HostFilterMode = iota // Активные (по умолчанию)
	FilterArchivedOnly                     // Отстойник (нерабочие серверы)
	FilterAll                              // Все серверы
)

type HostsView struct {
	Hosts         []*sshconfig.HostConfig
	Filtered      []*sshconfig.HostConfig
	MetaStore     *sshconfig.MetadataStore
	FilterMode    HostFilterMode
	SelectedIndex int
	FilterInput   textinput.Model
	Filtering     bool
	StartIdx      int
	MaxRows       int
	HeaderOffset  int
	Width         int
	Height        int
}

func NewHostsView(meta *sshconfig.MetadataStore) *HostsView {
	ti := textinput.New()
	ti.Placeholder = "Поиск хостов (имя, ip, пользователь)..."
	ti.Prompt = "/ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(AccentColor).Bold(true)

	hv := &HostsView{
		MetaStore:    meta,
		FilterInput:  ti,
		HeaderOffset: 3,
	}
	_ = hv.Reload()
	return hv
}

func (v *HostsView) Reload() error {
	hosts, err := sshconfig.LoadSSHConfig()
	if err != nil {
		return err
	}
	v.Hosts = hosts
	v.applyFilter()
	return nil
}

func (v *HostsView) CycleFilterMode() {
	v.FilterMode = (v.FilterMode + 1) % 3
	v.applyFilter()
}

func (v *HostsView) ApplyFilter() {
	v.applyFilter()
}

func (v *HostsView) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(v.FilterInput.Value()))
	var filtered []*sshconfig.HostConfig

	for _, h := range v.Hosts {
		key := h.PrimaryAlias()
		isArchived := v.MetaStore.IsArchived(key)

		if v.FilterMode == FilterActiveOnly && isArchived {
			continue
		}
		if v.FilterMode == FilterArchivedOnly && !isArchived {
			continue
		}

		if query != "" {
			match := false
			for _, a := range h.Aliases {
				if strings.Contains(strings.ToLower(a), query) {
					match = true
					break
				}
			}
			if strings.Contains(strings.ToLower(h.HostName), query) ||
				strings.Contains(strings.ToLower(h.User), query) ||
				strings.Contains(strings.ToLower(h.DisplayName()), query) {
				match = true
			}
			if !match {
				continue
			}
		}

		filtered = append(filtered, h)
	}
	v.Filtered = filtered

	if v.SelectedIndex >= len(v.Filtered) {
		v.SelectedIndex = len(v.Filtered) - 1
	}
	if v.SelectedIndex < 0 {
		v.SelectedIndex = 0
	}
}

func (v *HostsView) SelectedHost() *sshconfig.HostConfig {
	if len(v.Filtered) == 0 || v.SelectedIndex < 0 || v.SelectedIndex >= len(v.Filtered) {
		return nil
	}
	return v.Filtered[v.SelectedIndex]
}

func (v *HostsView) HandleMouse(msg tea.MouseMsg) (int, bool) {
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

func (v *HostsView) Update(msg tea.Msg) tea.Cmd {
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
		v.applyFilter()
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
		case "Z":
			v.CycleFilterMode()
		case "/":
			v.Filtering = true
			v.FilterInput.Focus()
			return textinput.Blink
		case "r":
			_ = v.Reload()
		}
	}

	return nil
}

func (v *HostsView) View(width, height int) string {
	v.Width = width
	v.Height = height

	var sb strings.Builder

	v.HeaderOffset = 4
	if v.Filtering || v.FilterInput.Value() != "" {
		v.HeaderOffset = 6
		sb.WriteString(lipgloss.NewStyle().MarginBottom(1).Render(v.FilterInput.View()))
		sb.WriteString("\n")
	}

	modeName := "🟢 Активные"
	if v.FilterMode == FilterArchivedOnly {
		modeName = "📦 Отстойник"
	} else if v.FilterMode == FilterAll {
		modeName = "📋 Все серверы"
	}

	archivedCount := v.MetaStore.ArchivedCount()
	activeCount := len(v.Hosts) - archivedCount
	if activeCount < 0 {
		activeCount = 0
	}

	filterBar := fmt.Sprintf("Режим: %s [Z для смены]  •  Активных: %d  •  В отстойнике: %d",
		lipgloss.NewStyle().Bold(true).Foreground(WarningColor).Render("["+modeName+"]"),
		activeCount,
		archivedCount,
	)
	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(filterBar))
	sb.WriteString("\n")

	wPrefix := 2
	wStatus := 14
	wAlias := 24
	wHost := 20
	wUser := 15
	wPort := 7
	wLast := width - wPrefix - wStatus - wAlias - wHost - wUser - wPort - 10
	if wLast < 16 {
		wLast = 16
	}

	// Header row rendered with Lipgloss cells for perfect alignment
	thStyle := TableHeaderStyle
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top,
		thStyle.Width(wPrefix).Render(" "),
		thStyle.Width(wStatus).Render("СТАТУС"),
		thStyle.Width(wAlias).Render("ХОСТ / ПСЕВДОНИМ"),
		thStyle.Width(wHost).Render("АДРЕС (IP)"),
		thStyle.Width(wUser).Render("ПОЛЬЗОВАТЕЛЬ"),
		thStyle.Width(wPort).Render("ПОРТ"),
		thStyle.Width(wLast).Render("ПОСЛ. ВХОД"),
	)
	sb.WriteString(headerRow)
	sb.WriteString("\n")

	if len(v.Filtered) == 0 {
		emptyMsg := "В ~/.ssh/config не найдено хостов. Нажмите [a], чтобы добавить новый хост."
		if v.FilterMode == FilterArchivedOnly {
			emptyMsg = "Отстойник пуст. Нажмите [z] на нерабочем сервере, чтобы отправить его в отстойник."
		} else if v.FilterMode == FilterActiveOnly {
			emptyMsg = "Все серверы находятся в отстойнике. Нажмите [Z], чтобы переключить режим."
		}
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
		h := v.Filtered[i]
		isSelected := i == v.SelectedIndex

		key := h.PrimaryAlias()
		meta := v.MetaStore.Get(key)

		// Status Badge
		statusStr := HostUnknownStyle.Render("○ -")
		if meta.Archived {
			statusStr = lipgloss.NewStyle().Foreground(TextMuted).Bold(true).Render("📦 ОТСТОЙНИК")
		} else {
			switch meta.Status {
			case sshconfig.StatusOnline:
				statusStr = HostOnlineStyle.Render(fmt.Sprintf("● %dms", meta.LatencyMs))
			case sshconfig.StatusOffline:
				statusStr = HostOfflineStyle.Render("✖ ОФФЛАЙН")
			case sshconfig.StatusChecking:
				statusStr = HostCheckingStyle.Render("◐ ПИНГ...")
			}
		}

		alias := h.DisplayName()
		if meta.Archived {
			alias = alias + " [АРХИВ]"
		}
		dest := h.HostName
		if dest == "" {
			dest = "-"
		}
		user := h.User
		if user == "" {
			user = "(по умолч.)"
		}
		portStr := fmt.Sprintf("%d", h.Port)
		lastConnStr := sshconfig.FormatLastConnected(meta.LastConnected)

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
			baseStyle.Width(wStatus).Render(statusStr),
			baseStyle.Width(wAlias).MaxWidth(wAlias).Render(alias),
			baseStyle.Width(wHost).MaxWidth(wHost).Render(dest),
			baseStyle.Width(wUser).MaxWidth(wUser).Render(user),
			baseStyle.Width(wPort).MaxWidth(wPort).Render(portStr),
			baseStyle.Width(wLast).MaxWidth(wLast).Render(lastConnStr),
		)

		sb.WriteString(rowContent)
		sb.WriteString("\n")
	}

	footerCount := fmt.Sprintf("Показано %d из %d хостов", len(v.Filtered), len(v.Hosts))
	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(SubtleColor).PaddingLeft(2).Render(footerCount))

	return sb.String()
}
