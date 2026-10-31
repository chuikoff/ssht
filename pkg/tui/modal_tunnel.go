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

type TunnelModalMode int

const (
	ModalModeAdd TunnelModalMode = iota
	ModalModeEdit
)

type TunnelModal struct {
	Mode           TunnelModalMode
	EditingID      string
	FocusIndex     int
	Inputs         []textinput.Model
	TunnelTypes    []tunnel.TunnelType
	TypeIndex      int
	AutoStart      bool
	AvailableHosts []string
	ErrMessage     string
	Width          int
	Height         int
}

const (
	fieldTunnelName = iota
	fieldTunnelType
	fieldTunnelHost
	fieldLocalPort
	fieldRemoteHost
	fieldRemotePort
	fieldBindAddress
	fieldAutoStart
	fieldTunnelDesc
	fieldCount
)

func NewTunnelModal(mode TunnelModalMode, t *tunnel.Tunnel, hosts []string) *TunnelModal {
	inputs := make([]textinput.Model, fieldCount)

	for i := range inputs {
		ti := textinput.New()
		ti.Cursor.Style = lipgloss.NewStyle().Foreground(AccentColor)
		ti.CharLimit = 120
		inputs[i] = ti
	}

	inputs[fieldTunnelName].Placeholder = "например: Postgres Dev, Web Proxy, Grafana"
	inputs[fieldTunnelHost].Placeholder = "Алиас из ~/.ssh/config или user@ip"
	inputs[fieldLocalPort].Placeholder = "например: 5432, 8080"
	inputs[fieldRemoteHost].Placeholder = "127.0.0.1 (или локальный IP в сети сервера)"
	inputs[fieldRemotePort].Placeholder = "например: 5432, 80"
	inputs[fieldBindAddress].Placeholder = "127.0.0.1"
	inputs[fieldTunnelDesc].Placeholder = "Необязательное описание"

	types := []tunnel.TunnelType{tunnel.TypeLocal, tunnel.TypeRemote, tunnel.TypeDynamic}
	typeIdx := 0
	autoStart := false
	editingID := ""

	if t != nil {
		editingID = t.ID
		inputs[fieldTunnelName].SetValue(t.Name)
		inputs[fieldTunnelHost].SetValue(t.Host)
		if t.LocalPort > 0 {
			inputs[fieldLocalPort].SetValue(strconv.Itoa(t.LocalPort))
		}
		inputs[fieldRemoteHost].SetValue(t.RemoteHost)
		if t.RemotePort > 0 {
			inputs[fieldRemotePort].SetValue(strconv.Itoa(t.RemotePort))
		}
		inputs[fieldBindAddress].SetValue(t.BindAddress)
		inputs[fieldTunnelDesc].SetValue(t.Description)
		autoStart = t.AutoStart

		for i, tt := range types {
			if tt == t.Type {
				typeIdx = i
				break
			}
		}
	} else {
		inputs[fieldBindAddress].SetValue("127.0.0.1")
		inputs[fieldRemoteHost].SetValue("127.0.0.1")
		if len(hosts) > 0 {
			inputs[fieldTunnelHost].SetValue(hosts[0])
		}
	}

	inputs[0].Focus()

	return &TunnelModal{
		Mode:           mode,
		EditingID:      editingID,
		FocusIndex:     0,
		Inputs:         inputs,
		TunnelTypes:    types,
		TypeIndex:      typeIdx,
		AutoStart:      autoStart,
		AvailableHosts: hosts,
	}
}

func (m *TunnelModal) setFocus(idx int) {
	if idx < 0 || idx >= fieldCount {
		return
	}
	m.Inputs[m.FocusIndex].Blur()
	m.FocusIndex = idx
	if m.FocusIndex != fieldTunnelType && m.FocusIndex != fieldAutoStart {
		m.Inputs[m.FocusIndex].Focus()
	}
}

func (m *TunnelModal) HandleMouse(msg tea.MouseMsg, screenWidth, screenHeight int) (*tunnel.Tunnel, bool) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}

	modalHeight := 16
	modalWidth := 74
	topY := (screenHeight - modalHeight) / 2
	leftX := (screenWidth - modalWidth) / 2

	relY := msg.Y - topY - 2
	if relY >= 0 && relY < fieldCount {
		if relY == fieldTunnelType {
			m.TypeIndex = (m.TypeIndex + 1) % len(m.TunnelTypes)
			return nil, false
		}
		if relY == fieldAutoStart {
			m.AutoStart = !m.AutoStart
			return nil, false
		}
		m.setFocus(relY)
		return nil, false
	}

	if msg.Y >= topY+modalHeight-3 && msg.Y <= topY+modalHeight {
		relX := msg.X - leftX
		if relX > 30 && relX < 55 {
			tun, err := m.Validate()
			if err != nil {
				m.ErrMessage = err.Error()
				return nil, false
			}
			return tun, true
		}
		if relX >= 55 && relX < 72 {
			return nil, true
		}
	}

	return nil, false
}

func (m *TunnelModal) Update(msg tea.Msg) (*tunnel.Tunnel, bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return nil, true, nil
		case "tab", "down":
			m.nextField()
			return nil, false, nil
		case "shift+tab", "up":
			m.prevField()
			return nil, false, nil
		case "left", "h":
			if m.FocusIndex == fieldTunnelType {
				if m.TypeIndex > 0 {
					m.TypeIndex--
				} else {
					m.TypeIndex = len(m.TunnelTypes) - 1
				}
				return nil, false, nil
			}
			if m.FocusIndex == fieldAutoStart {
				m.AutoStart = !m.AutoStart
				return nil, false, nil
			}
		case "right", "l":
			if m.FocusIndex == fieldTunnelType {
				if m.TypeIndex < len(m.TunnelTypes)-1 {
					m.TypeIndex++
				} else {
					m.TypeIndex = 0
				}
				return nil, false, nil
			}
			if m.FocusIndex == fieldAutoStart {
				m.AutoStart = !m.AutoStart
				return nil, false, nil
			}
		case " ":
			if m.FocusIndex == fieldTunnelType {
				m.TypeIndex = (m.TypeIndex + 1) % len(m.TunnelTypes)
				return nil, false, nil
			}
			if m.FocusIndex == fieldAutoStart {
				m.AutoStart = !m.AutoStart
				return nil, false, nil
			}
		case "enter":
			tun, err := m.Validate()
			if err != nil {
				m.ErrMessage = err.Error()
				return nil, false, nil
			}
			return tun, true, nil
		}
	}

	var cmds []tea.Cmd
	for i := range m.Inputs {
		if i != fieldTunnelType && i != fieldAutoStart {
			var cmd tea.Cmd
			m.Inputs[i], cmd = m.Inputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return nil, false, tea.Batch(cmds...)
}

func (m *TunnelModal) nextField() {
	m.Inputs[m.FocusIndex].Blur()
	m.FocusIndex = (m.FocusIndex + 1) % fieldCount
	if m.FocusIndex != fieldTunnelType && m.FocusIndex != fieldAutoStart {
		m.Inputs[m.FocusIndex].Focus()
	}
}

func (m *TunnelModal) prevField() {
	m.Inputs[m.FocusIndex].Blur()
	m.FocusIndex--
	if m.FocusIndex < 0 {
		m.FocusIndex = fieldCount - 1
	}
	if m.FocusIndex != fieldTunnelType && m.FocusIndex != fieldAutoStart {
		m.Inputs[m.FocusIndex].Focus()
	}
}

func (m *TunnelModal) Validate() (*tunnel.Tunnel, error) {
	name := strings.TrimSpace(m.Inputs[fieldTunnelName].Value())
	if name == "" {
		return nil, fmt.Errorf("укажите название тоннеля")
	}

	host := strings.TrimSpace(m.Inputs[fieldTunnelHost].Value())
	if host == "" {
		return nil, fmt.Errorf("укажите SSH хост")
	}

	tunType := m.TunnelTypes[m.TypeIndex]

	localPortStr := strings.TrimSpace(m.Inputs[fieldLocalPort].Value())
	localPort, err := strconv.Atoi(localPortStr)
	if err != nil || localPort <= 0 || localPort > 65535 {
		return nil, fmt.Errorf("некорректный локальный порт (1-65535)")
	}

	remoteHost := strings.TrimSpace(m.Inputs[fieldRemoteHost].Value())
	if remoteHost == "" && tunType != tunnel.TypeDynamic {
		remoteHost = "127.0.0.1"
	}

	remotePort := 0
	if tunType != tunnel.TypeDynamic {
		remotePortStr := strings.TrimSpace(m.Inputs[fieldRemotePort].Value())
		var err error
		remotePort, err = strconv.Atoi(remotePortStr)
		if err != nil || remotePort <= 0 || remotePort > 65535 {
			return nil, fmt.Errorf("некорректный удаленный порт (1-65535)")
		}
	}

	bindAddr := strings.TrimSpace(m.Inputs[fieldBindAddress].Value())
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	desc := strings.TrimSpace(m.Inputs[fieldTunnelDesc].Value())

	id := m.EditingID
	if id == "" {
		id = tunnel.GenerateID()
	}

	return &tunnel.Tunnel{
		ID:          id,
		Name:        name,
		Type:        tunType,
		Host:        host,
		BindAddress: bindAddr,
		LocalPort:   localPort,
		RemoteHost:  remoteHost,
		RemotePort:  remotePort,
		AutoStart:   m.AutoStart,
		Description: desc,
	}, nil
}

func (m *TunnelModal) View() string {
	title := "  СОЗДАТЬ SSH ТОННЕЛЬ  "
	if m.Mode == ModalModeEdit {
		title = "  РЕДАКТИРОВАТЬ SSH ТОННЕЛЬ  "
	}

	labels := []string{
		"Название:       ",
		"Тип тоннеля:    ",
		"SSH Хост:       ",
		"Локальный порт: ",
		"Удаленный хост: ",
		"Удаленный порт: ",
		"Адрес привязки: ",
		"Автозапуск:     ",
		"Описание:       ",
	}

	var sb strings.Builder
	sb.WriteString(ModalTitleStyle.Render(title))
	sb.WriteString("\n\n")

	for i := 0; i < fieldCount; i++ {
		isFocused := m.FocusIndex == i

		labelStyle := lipgloss.NewStyle().Foreground(TextMuted).Width(18)
		if isFocused {
			labelStyle = labelStyle.Bold(true).Foreground(AccentColor)
		}

		sb.WriteString(labelStyle.Render(labels[i]))

		switch i {
		case fieldTunnelType:
			curType := m.TunnelTypes[m.TypeIndex]
			typeStr := fmt.Sprintf("< %s > (Клик или Пробел для выбора)", strings.ToUpper(string(curType)))
			if isFocused {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(WarningColor).Render(typeStr))
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(TextLight).Render(typeStr))
			}
		case fieldAutoStart:
			val := "Нет"
			if m.AutoStart {
				val = "Да"
			}
			autoStr := fmt.Sprintf("< %s > (Клик или Пробел для выбора)", val)
			if isFocused {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(WarningColor).Render(autoStr))
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(TextLight).Render(autoStr))
			}
		default:
			sb.WriteString(m.Inputs[i].View())
		}
		sb.WriteString("\n")
	}

	if m.ErrMessage != "" {
		sb.WriteString("\n")
		sb.WriteString(ErrorNotificationStyle.Render("Ошибка: " + m.ErrMessage))
		sb.WriteString("\n")
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(SubtleColor).Render(
		"[Tab/Вниз] След.  [Shift+Tab/Вверх] Пред.  [Enter/Клик] Сохранить  [Esc/Клик] Отмена",
	))

	content := sb.String()
	return ModalContainerStyle.Render(content)
}
