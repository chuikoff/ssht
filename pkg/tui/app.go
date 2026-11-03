package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/backup"
	"sshtunnels/pkg/monitoring"
	"sshtunnels/pkg/session"
	"sshtunnels/pkg/session/vt"
	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tunnel"
	"sshtunnels/pkg/version"
)

type ViewMode int

const (
	ViewModeDashboard ViewMode = iota
	ViewModeTerminal
)

type TabIndex int

const (
	TabHosts TabIndex = iota
	TabTunnels
	TabHelp
)

type ModalType int

const (
	ModalNone ModalType = iota
	ModalAddTunnel
	ModalEditTunnel
	ModalAddHost
	ModalRenameHost
	ModalViewLogs
	ModalExport
	ModalImport
	ModalMonitor
)

type tickMsg time.Time
type sshExitedMsg struct{ err error }
type sessionOutputMsg struct{}
type sessionClosedMsg struct{ id string }
type monitorMetricsMsg struct {
	Metrics *monitoring.ServerMetrics
	Err     error
}

type hostCheckResultMsg struct {
	Key       string
	Online    bool
	LatencyMs int64
	Err       error
}

type allHostsCheckedMsg struct{}

type FooterBtn struct {
	Key    string
	Label  string
	Action string
}

type AppModel struct {
	Manager     *tunnel.Manager
	MetaStore   *sshconfig.MetadataStore
	ActiveTab   TabIndex
	HostsView   *HostsView
	TunnelsView *TunnelsView
	HelpView    *HelpView

	// Embedded SSH Session & Mode State
	ViewMode         ViewMode
	SessionMgr       *session.SessionManager
	TerminalTopZones []ButtonZone
	TerminalTabZones []ButtonZone
	SessionBarZones  []ButtonZone
	Selection        session.SelectionRange

	// Modal State
	ActiveModal ModalType
	TunnelModal *TunnelModal
	HostModal   *HostModal
	RenameModal *RenameModal
	LogModal     *LogModal
	ExportModal  *ExportModal
	ImportModal  *ImportModal
	MonitorModal *MonitorModal

	// Mouse Zones & State
	TabZones    []ButtonZone
	FooterZones []ButtonZone
	LastClick   LastClickState

	// Notification & Status
	Notification string
	IsErrorNotif bool

	Width    int
	Height   int
	Ready    bool
	Detached bool
}

func NewAppModel(m *tunnel.Manager, meta *sshconfig.MetadataStore) *AppModel {
	hv := NewHostsView(meta)
	tv := NewTunnelsView(m)
	help := NewHelpView()
	sm := session.NewSessionManager()

	app := &AppModel{
		Manager:          m,
		MetaStore:        meta,
		ActiveTab:        TabHosts,
		HostsView:        hv,
		TunnelsView:      tv,
		HelpView:         help,
		ActiveModal:      ModalNone,
		ViewMode:         ViewModeDashboard,
		SessionMgr:       sm,
		TerminalTopZones: []ButtonZone{},
		TerminalTabZones: []ButtonZone{},
		SessionBarZones:  []ButtonZone{},
	}

	sm.OnConnected = func(target, alias string) {
		meta.RecordConnection(target)
		app.notify(fmt.Sprintf("✔ Терминал подключен к '%s'", alias), false)
	}

	return app
}

func (a *AppModel) Init() tea.Cmd {
	for _, t := range a.Manager.GetTunnels() {
		if t.AutoStart {
			_ = a.Manager.StartTunnel(t.ID)
		}
	}
	a.TunnelsView.ApplyFilter()

	return tea.Batch(
		tea.EnterAltScreen,
		tickerCmd(),
		a.waitForSessionActivity(),
	)
}

func (a *AppModel) waitForSessionActivity() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-a.SessionMgr.NotifyChan():
			return sessionOutputMsg{}
		case id, ok := <-a.SessionMgr.ClosedChan():
			if !ok {
				return nil
			}
			return sessionClosedMsg{id: id}
		}
	}
}

func tickerCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (a *AppModel) notify(msg string, isError bool) {
	a.Notification = msg
	a.IsErrorNotif = isError
}

func (a *AppModel) checkAllHostsCmd() tea.Cmd {
	hosts := a.HostsView.Filtered
	if len(hosts) == 0 {
		return nil
	}

	for _, h := range hosts {
		a.MetaStore.SetStatus(h.PrimaryAlias(), sshconfig.StatusChecking, 0, "")
	}

	return func() tea.Msg {
		var wg sync.WaitGroup
		msgChan := make(chan hostCheckResultMsg, len(hosts))

		for _, h := range hosts {
			wg.Add(1)
			go func(host *sshconfig.HostConfig) {
				defer wg.Done()
				online, latency, err := sshconfig.PingHost(host, 2500*time.Millisecond)
				msgChan <- hostCheckResultMsg{
					Key:       host.PrimaryAlias(),
					Online:    online,
					LatencyMs: latency,
					Err:       err,
				}
			}(h)
		}

		wg.Wait()
		close(msgChan)

		for res := range msgChan {
			status := sshconfig.StatusOffline
			errMsg := ""
			if res.Online {
				status = sshconfig.StatusOnline
			} else if res.Err != nil {
				errMsg = res.Err.Error()
			}
			a.MetaStore.SetStatus(res.Key, status, res.LatencyMs, errMsg)
		}

		return allHostsCheckedMsg{}
	}
}

func fetchMetricsCmd(target string) tea.Cmd {
	return func() tea.Msg {
		metrics, err := monitoring.FetchMetrics(target, 8*time.Second)
		return monitorMetricsMsg{Metrics: metrics, Err: err}
	}
}

func (a *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case sessionOutputMsg:
		return a, a.waitForSessionActivity()

	case sessionClosedMsg:
		s := a.SessionMgr.FindByID(msg.id)
		wasConnected := false
		alias := msg.id
		if s != nil {
			wasConnected = s.IsConnected()
			alias = s.Alias
		}
		a.SessionMgr.Close(msg.id)
		if a.SessionMgr.Count() == 0 {
			a.ViewMode = ViewModeDashboard
		}
		if wasConnected {
			a.notify(fmt.Sprintf("SSH сессия '%s' отключена", alias), false)
		} else {
			a.notify(fmt.Sprintf("Не удалось подключиться к '%s' (хост недоступен)", alias), true)
		}
		return a, a.waitForSessionActivity()

	case monitorMetricsMsg:
		if a.MonitorModal != nil {
			a.MonitorModal.Loading = false
			if msg.Err != nil {
				a.MonitorModal.ErrMessage = msg.Err.Error()
			} else {
				a.MonitorModal.Metrics = msg.Metrics
				a.MonitorModal.ErrMessage = ""
			}
		}
		return a, nil

	case tea.WindowSizeMsg:
		a.Width = msg.Width
		a.Height = msg.Height
		a.Ready = true
		termHeight := a.Height - 2
		if termHeight < 5 {
			termHeight = 5
		}
		a.SessionMgr.ResizeAll(a.Width, termHeight)
		return a, nil

	case tickMsg:
		a.TunnelsView.ApplyFilter()
		return a, tickerCmd()

	case hostCheckResultMsg:
		status := sshconfig.StatusOffline
		errMsg := ""
		if msg.Online {
			status = sshconfig.StatusOnline
		} else if msg.Err != nil {
			errMsg = msg.Err.Error()
		}
		a.MetaStore.SetStatus(msg.Key, status, msg.LatencyMs, errMsg)
		return a, nil

	case allHostsCheckedMsg:
		a.notify("Проверка доступности хостов завершена", false)
		return a, nil

	case sshExitedMsg:
		if msg.err != nil {
			a.notify(fmt.Sprintf("SSH сессия завершена с ошибкой: %v", msg.err), true)
		} else {
			a.notify("SSH сессия отключена", false)
		}
		return a, nil

	case tea.MouseMsg:
		return a.handleMouse(msg)

	case tea.KeyMsg:
		if a.ViewMode == ViewModeTerminal {
			switch msg.Type {
			case tea.KeyF1:
				a.ViewMode = ViewModeDashboard
				return a, nil
			case tea.KeyCtrlC:
				if a.Selection.Active {
					active := a.SessionMgr.GetActive()
					if active != nil && active.VT != nil {
						text := session.ExtractSelectedText(active.VT, a.Selection, active.ScrollOffset)
						if len(text) > 0 {
							_ = clipboard.WriteAll(text)
							a.notify("✔ Выделенный текст скопирован в буфер обмена", false)
						}
					}
					a.Selection = session.SelectionRange{}
					return a, nil
				}
			case tea.KeyEnter:
				if a.Selection.Active {
					active := a.SessionMgr.GetActive()
					if active != nil && active.VT != nil {
						text := session.ExtractSelectedText(active.VT, a.Selection, active.ScrollOffset)
						if len(text) > 0 {
							_ = clipboard.WriteAll(text)
							a.notify("✔ Выделенный текст скопирован в буфер обмена", false)
						}
					}
					a.Selection = session.SelectionRange{}
					return a, nil
				}
			case tea.KeyEsc:
				if a.Selection.Active {
					a.Selection = session.SelectionRange{}
					return a, nil
				}
			case tea.KeyCtrlW:
				active := a.SessionMgr.GetActive()
				if active != nil {
					a.SessionMgr.Close(active.ID)
					if a.SessionMgr.Count() == 0 {
						a.ViewMode = ViewModeDashboard
					}
				}
				return a, nil
			case tea.KeyCtrlPgUp:
				a.SessionMgr.PrevTab()
				a.Selection = session.SelectionRange{}
				return a, nil
			case tea.KeyCtrlPgDown:
				a.SessionMgr.NextTab()
				a.Selection = session.SelectionRange{}
				return a, nil
			}

			active := a.SessionMgr.GetActive()
			if active != nil && active.VT != nil {
				// Keyboard scrolling with PgUp / PgDown in normal shell mode
				active.VT.Lock()
				mode := active.VT.Mode()
				sbLen := active.VT.ScrollbackLen()
				active.VT.Unlock()

				if mode&vt.ModeAltScreen == 0 {
					if msg.Type == tea.KeyPgUp && sbLen > 0 {
						active.ScrollOffset += 10
						if active.ScrollOffset > sbLen {
							active.ScrollOffset = sbLen
						}
						return a, nil
					}
					if msg.Type == tea.KeyPgDown && active.ScrollOffset > 0 {
						active.ScrollOffset -= 10
						if active.ScrollOffset < 0 {
							active.ScrollOffset = 0
						}
						return a, nil
					}
				}
			}

			if msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
				tabIdx := int(msg.Runes[0] - '1')
				if tabIdx < a.SessionMgr.Count() {
					a.SessionMgr.SetActive(tabIdx)
					a.Selection = session.SelectionRange{}
				}
				return a, nil
			}

			if msg.Alt && msg.Type == tea.KeyLeft {
				a.SessionMgr.PrevTab()
				a.Selection = session.SelectionRange{}
				return a, nil
			}
			if msg.Alt && msg.Type == tea.KeyRight {
				a.SessionMgr.NextTab()
				a.Selection = session.SelectionRange{}
				return a, nil
			}

			if a.Selection.Active {
				a.Selection = session.SelectionRange{}
			}

			// Any typing snaps scrollOffset back to 0 (live view)
			if active != nil && active.ScrollOffset > 0 {
				active.ScrollOffset = 0
			}

			data := session.KeyToBytes(msg)
			if len(data) > 0 {
				_ = a.SessionMgr.WriteActive(data)
			}
			return a, nil
		}

		if a.ActiveModal != ModalNone {
			return a.handleModalKey(msg)
		}

		if a.SessionMgr.Count() > 0 {
			if msg.Type == tea.KeyF1 {
				a.ViewMode = ViewModeTerminal
				return a, nil
			}
			if msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
				tabIdx := int(msg.Runes[0] - '1')
				if tabIdx < a.SessionMgr.Count() {
					a.SessionMgr.SetActive(tabIdx)
					a.ViewMode = ViewModeTerminal
					return a, nil
				}
			}
		}

		isFiltering := (a.ActiveTab == TabHosts && a.HostsView.Filtering) ||
			(a.ActiveTab == TabTunnels && a.TunnelsView.Filtering)

		if !isFiltering {
			switch msg.String() {
			case "ctrl+c", "q":
				a.Manager.StopAll()
				a.SessionMgr.CloseAll()
				return a, tea.Quit

			case "b":
				a.Detached = true
				return a, tea.Quit

			case "tab":
				a.ActiveTab = (a.ActiveTab + 1) % 3
				return a, nil

			case "shift+tab":
				if a.ActiveTab > 0 {
					a.ActiveTab--
				} else {
					a.ActiveTab = TabHelp
				}
				return a, nil

			case "1":
				a.ActiveTab = TabHosts
				return a, nil

			case "2":
				a.ActiveTab = TabTunnels
				return a, nil

			case "3", "?":
				a.ActiveTab = TabHelp
				return a, nil

			case "x":
				a.ExportModal = NewExportModal()
				a.ActiveModal = ModalExport
				return a, nil

			case "i":
				a.ImportModal = NewImportModal()
				a.ActiveModal = ModalImport
				return a, nil
			}
		}
	}

	switch a.ActiveTab {
	case TabHosts:
		cmd := a.handleHostsTab(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	case TabTunnels:
		cmd := a.handleTunnelsTab(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	case TabHelp:
		cmd := a.HelpView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, tea.Batch(cmds...)
}

func (a *AppModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if a.ViewMode == ViewModeTerminal {
		// Mouse wheel scrolling inside terminal
		if msg.Button == tea.MouseButtonWheelUp {
			if msg.Y >= 1 && msg.Y <= a.Height-2 {
				active := a.SessionMgr.GetActive()
				if active != nil && active.VT != nil {
					active.VT.Lock()
					mode := active.VT.Mode()
					sbLen := active.VT.ScrollbackLen()
					active.VT.Unlock()

					if mode&vt.ModeAltScreen != 0 {
						// Fullscreen interactive app (vim, less, htop, mc, nano)
						if mode&vt.ModeMouseMask != 0 {
							_ = a.SessionMgr.WriteActive([]byte(fmt.Sprintf("\x1b[<64;%d;%dM", msg.X+1, msg.Y)))
						} else {
							_ = a.SessionMgr.WriteActive([]byte("\x1b[A\x1b[A\x1b[A"))
						}
					} else {
						// Standard shell (bash, zsh, cmd): scroll terminal scrollback buffer!
						if sbLen > 0 {
							active.ScrollOffset += 3
							if active.ScrollOffset > sbLen {
								active.ScrollOffset = sbLen
							}
						}
					}
				}
				return a, nil
			}
		} else if msg.Button == tea.MouseButtonWheelDown {
			if msg.Y >= 1 && msg.Y <= a.Height-2 {
				active := a.SessionMgr.GetActive()
				if active != nil && active.VT != nil {
					active.VT.Lock()
					mode := active.VT.Mode()
					active.VT.Unlock()

					if mode&vt.ModeAltScreen != 0 {
						// Fullscreen interactive app
						if mode&vt.ModeMouseMask != 0 {
							_ = a.SessionMgr.WriteActive([]byte(fmt.Sprintf("\x1b[<65;%d;%dM", msg.X+1, msg.Y)))
						} else {
							_ = a.SessionMgr.WriteActive([]byte("\x1b[B\x1b[B\x1b[B"))
						}
					} else {
						// Standard shell: scroll down towards active prompt
						if active.ScrollOffset > 0 {
							active.ScrollOffset -= 3
							if active.ScrollOffset < 0 {
								active.ScrollOffset = 0
							}
						}
					}
				}
				return a, nil
			}
		}

		// Click on collapsed top bar (Y == 0)
		if msg.Y == 0 {
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				for _, z := range a.TerminalTopZones {
					if z.Action == "close_active" && msg.X >= z.StartX && msg.X <= z.EndX {
						return a.executeFooterAction("close_active")
					}
				}
				a.ViewMode = ViewModeDashboard
				return a, nil
			}
			return a, nil
		}

		// Click on bottom session tabs bar (Y >= a.Height - 1)
		if msg.Y >= a.Height-1 {
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				for _, z := range a.TerminalTabZones {
					if msg.X >= z.StartX && msg.X <= z.EndX {
						if z.Action == "dashboard" {
							a.ViewMode = ViewModeDashboard
							return a, nil
						}
						if z.Action == "new_session" {
							a.ViewMode = ViewModeDashboard
							a.ActiveTab = TabHosts
							return a, nil
						}
						if z.Action == "close_active" {
							return a.executeFooterAction("close_active")
						}
						if strings.HasPrefix(z.Action, "tab_") {
							var idx int
							fmt.Sscanf(z.Action, "tab_%d", &idx)
							// Double-click on tab closes it!
							if a.LastClick.IsDoubleClick(TabHosts, 2000+idx) {
								list := a.SessionMgr.List()
								if idx >= 0 && idx < len(list) {
									name := list[idx].Alias
									a.SessionMgr.Close(list[idx].ID)
									if a.SessionMgr.Count() == 0 {
										a.ViewMode = ViewModeDashboard
									}
									a.notify(fmt.Sprintf("SSH сессия '%s' отключена", name), false)
								}
								return a, nil
							}
							a.LastClick.Record(TabHosts, 2000+idx)
							a.SessionMgr.SetActive(idx)
							a.Selection = session.SelectionRange{}
							return a, nil
						}
						if strings.HasPrefix(z.Action, "close_") {
							var idx int
							fmt.Sscanf(z.Action, "close_%d", &idx)
							list := a.SessionMgr.List()
							if idx >= 0 && idx < len(list) {
								name := list[idx].Alias
								a.SessionMgr.Close(list[idx].ID)
								if a.SessionMgr.Count() == 0 {
									a.ViewMode = ViewModeDashboard
								}
								a.notify(fmt.Sprintf("SSH сессия '%s' отключена", name), false)
							}
							return a, nil
						}
					}
				}
			}
			return a, nil
		}

		// Terminal content area (Y >= 1 && Y <= a.Height - 2)
		// 1. Right Click:
		// - If text is currently selected: COPY selection to clipboard and CLEAR selection (do NOT paste!)
		// - If no text is selected: PASTE from clipboard into active SSH session
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight {
			if a.Selection.Active {
				active := a.SessionMgr.GetActive()
				if active != nil && active.VT != nil {
					text := session.ExtractSelectedText(active.VT, a.Selection, active.ScrollOffset)
					if len(strings.TrimSpace(text)) > 0 {
						_ = clipboard.WriteAll(text)
						a.notify("✔ Выделенный текст скопирован в буфер обмена", false)
					}
				}
				a.Selection = session.SelectionRange{}
				return a, nil
			}

			// No selection: paste from clipboard into active SSH session
			active := a.SessionMgr.GetActive()
			if active != nil {
				text, err := clipboard.ReadAll()
				if err == nil && len(text) > 0 {
					_ = a.SessionMgr.WriteActive([]byte(text))
					a.notify("✔ Текст вставлен из буфера обмена", false)
				}
			}
			return a, nil
		}

		// 2. Left Press: Start text selection
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			termY := msg.Y - 1
			termX := msg.X
			a.Selection = session.SelectionRange{
				Active:    true,
				Selecting: true,
				StartX:    termX,
				StartY:    termY,
				EndX:      termX,
				EndY:      termY,
			}
			return a, nil
		}

		// 3. Mouse Motion (dragging with left button): Update selection
		if msg.Action == tea.MouseActionMotion && a.Selection.Selecting {
			termY := msg.Y - 1
			if termY < 0 {
				termY = 0
			}
			if termY > a.Height-3 {
				termY = a.Height - 3
			}
			termX := msg.X
			if termX < 0 {
				termX = 0
			}
			if termX >= a.Width {
				termX = a.Width - 1
			}
			a.Selection.EndX = termX
			a.Selection.EndY = termY
			a.Selection.Active = true
			return a, nil
		}

		// 4. Mouse Release: Finish selection and copy to clipboard
		if msg.Action == tea.MouseActionRelease && a.Selection.Selecting {
			a.Selection.Selecting = false
			if a.Selection.StartX == a.Selection.EndX && a.Selection.StartY == a.Selection.EndY {
				// Single click without drag: clear selection
				a.Selection.Active = false
			} else {
				active := a.SessionMgr.GetActive()
				if active != nil && active.VT != nil {
					text := session.ExtractSelectedText(active.VT, a.Selection, active.ScrollOffset)
					if len(strings.TrimSpace(text)) > 0 {
						_ = clipboard.WriteAll(text)
						a.notify("✔ Выделенный текст скопирован в буфер обмена", false)
					}
				}
			}
			return a, nil
		}

		return a, nil
	}

	// 1. If modal is active, delegate mouse to modal
	if a.ActiveModal != ModalNone {
		switch a.ActiveModal {
		case ModalAddTunnel, ModalEditTunnel:
			tun, closed := a.TunnelModal.HandleMouse(msg, a.Width, a.Height)
			if closed {
				if tun != nil {
					if a.ActiveModal == ModalAddTunnel {
						if err := a.Manager.AddTunnel(tun); err != nil {
							a.notify(fmt.Sprintf("Ошибка добавления тоннеля: %v", err), true)
						} else {
							a.notify(fmt.Sprintf("Тоннель '%s' добавлен", tun.Name), false)
						}
					} else {
						if err := a.Manager.UpdateTunnel(tun); err != nil {
							a.notify(fmt.Sprintf("Ошибка обновления: %v", err), true)
						} else {
							a.notify(fmt.Sprintf("Тоннель '%s' обновлен", tun.Name), false)
						}
					}
					a.TunnelsView.ApplyFilter()
				}
				a.ActiveModal = ModalNone
				a.TunnelModal = nil
			}
			return a, nil

		case ModalAddHost:
			host, closed := a.HostModal.HandleMouse(msg, a.Width, a.Height)
			if closed {
				if host != nil {
					if err := sshconfig.AppendHost(host); err != nil {
						a.notify(fmt.Sprintf("Ошибка записи в ~/.ssh/config: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Хост '%s' добавлен в ~/.ssh/config", host.DisplayName()), false)
						_ = a.HostsView.Reload()
					}
				}
				a.ActiveModal = ModalNone
				a.HostModal = nil
			}
			return a, nil

		case ModalRenameHost:
			newAlias, closed := a.RenameModal.HandleMouse(msg, a.Width, a.Height)
			if closed {
				if newAlias != "" {
					oldAlias := a.RenameModal.Host.PrimaryAlias()
					if err := sshconfig.UpdateHostAlias(a.RenameModal.Host, newAlias); err != nil {
						a.notify(fmt.Sprintf("Ошибка переименования: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Хост переименован в '%s'", newAlias), false)
						for _, t := range a.Manager.GetTunnels() {
							if t.Host == oldAlias {
								t.Host = newAlias
								_ = a.Manager.UpdateTunnel(t)
							}
						}
						_ = a.HostsView.Reload()
					}
				}
				a.ActiveModal = ModalNone
				a.RenameModal = nil
			}
			return a, nil

		case ModalViewLogs:
			if a.LogModal.HandleMouse(msg, a.Width, a.Height) {
				a.ActiveModal = ModalNone
				a.LogModal = nil
			}
			return a, nil

		case ModalExport:
			path, closed := a.ExportModal.HandleMouse(msg, a.Width, a.Height)
			if closed {
				if path != "" {
					savedPath, err := backup.Export(path, a.Manager, a.MetaStore)
					if err != nil {
						a.notify(fmt.Sprintf("Ошибка экспорта: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Резервная копия сохранена в %s", savedPath), false)
					}
				}
				a.ActiveModal = ModalNone
				a.ExportModal = nil
			}
			return a, nil

		case ModalImport:
			path, overwrite, closed := a.ImportModal.HandleMouse(msg, a.Width, a.Height)
			if closed {
				if path != "" {
					res, err := backup.Import(path, a.Manager, a.MetaStore, overwrite)
					if err != nil {
						a.notify(fmt.Sprintf("Ошибка импорта: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Импортировано: %d тоннелей, %d хостов", res.TunnelsImported, res.HostsAdded), false)
						_ = a.HostsView.Reload()
						a.TunnelsView.ApplyFilter()
					}
				}
				a.ActiveModal = ModalNone
				a.ImportModal = nil
			}
			return a, nil

		case ModalMonitor:
			action, closed := a.MonitorModal.HandleMouse(msg, a.Width, a.Height)
			if closed {
				a.ActiveModal = ModalNone
				a.MonitorModal = nil
			} else if action == "refresh" && a.MonitorModal != nil {
				a.MonitorModal.Loading = true
				return a, fetchMetricsCmd(a.MonitorModal.Alias)
			}
			return a, nil
		}
		return a, nil
	}

	// 2. Click on Header Tabs (Y == 0)
	if msg.Y == 0 && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		for _, z := range a.TabZones {
			if msg.X >= z.StartX && msg.X <= z.EndX {
				switch z.Action {
				case "tab_hosts":
					a.ActiveTab = TabHosts
				case "tab_tunnels":
					a.ActiveTab = TabTunnels
				case "tab_help":
					a.ActiveTab = TabHelp
				}
				return a, nil
			}
		}
	}

	// 3. Click on Dashboard Session Bar (Y == a.Height - 2 when sessions are active)
	if a.SessionMgr.Count() > 0 && msg.Y == a.Height-2 && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		for _, z := range a.SessionBarZones {
			if msg.X >= z.StartX && msg.X <= z.EndX {
				if z.Action == "goto_terminal" {
					a.ViewMode = ViewModeTerminal
					return a, nil
				}
				return a.executeFooterAction(z.Action)
			}
		}
	}

	// 4. Click on Footer Buttons (Y == a.Height - 1, or Y >= a.Height - 2 if no sessions active)
	isFooterRow := msg.Y >= a.Height-1
	if a.SessionMgr.Count() == 0 {
		isFooterRow = msg.Y >= a.Height-2
	}
	if isFooterRow && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		for _, z := range a.FooterZones {
			if msg.X >= z.StartX && msg.X <= z.EndX {
				return a.executeFooterAction(z.Action)
			}
		}
	}

	// 4. Mouse on View Bodies (row clicking & scrolling)
	switch a.ActiveTab {
	case TabHosts:
		clickedRow, wasClicked := a.HostsView.HandleMouse(msg)
		if wasClicked {
			if a.LastClick.IsDoubleClick(TabHosts, clickedRow) {
				selected := a.HostsView.SelectedHost()
				if selected != nil {
					return a, a.connectHost(selected)
				}
			} else {
				a.LastClick.Record(TabHosts, clickedRow)
			}
		}

	case TabTunnels:
		clickedRow, wasClicked := a.TunnelsView.HandleMouse(msg)
		if wasClicked {
			if a.LastClick.IsDoubleClick(TabTunnels, clickedRow) {
				selected := a.TunnelsView.SelectedTunnel()
				if selected != nil {
					_ = a.Manager.ToggleTunnel(selected.ID)
					a.TunnelsView.ApplyFilter()
				}
			} else {
				a.LastClick.Record(TabTunnels, clickedRow)
			}
		}

	case TabHelp:
		a.HelpView.HandleMouse(msg)
	}

	return a, nil
}

func (a *AppModel) executeFooterAction(action string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(action, "switch_session_") {
		var idx int
		fmt.Sscanf(action, "switch_session_%d", &idx)
		a.SessionMgr.SetActive(idx)
		a.ViewMode = ViewModeTerminal
		return a, nil
	}
	if strings.HasPrefix(action, "close_session_") {
		var idx int
		fmt.Sscanf(action, "close_session_%d", &idx)
		list := a.SessionMgr.List()
		if idx >= 0 && idx < len(list) {
			name := list[idx].Alias
			a.SessionMgr.Close(list[idx].ID)
			if a.SessionMgr.Count() == 0 {
				a.ViewMode = ViewModeDashboard
			}
			a.notify(fmt.Sprintf("SSH сессия '%s' отключена", name), false)
		}
		return a, nil
	}

	switch action {
	case "close_active":
		active := a.SessionMgr.GetActive()
		if active != nil {
			name := active.Alias
			a.SessionMgr.Close(active.ID)
			if a.SessionMgr.Count() == 0 {
				a.ViewMode = ViewModeDashboard
			}
			a.notify(fmt.Sprintf("SSH сессия '%s' отключена", name), false)
		}
		return a, nil
	case "connect":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			return a, a.connectHost(selected)
		}
	case "connect_external":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			return a, a.connectExternalHost(selected)
		}
	case "goto_terminal":
		a.ViewMode = ViewModeTerminal
		return a, nil
	case "metrics":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			target := selected.PrimaryAlias()
			a.MonitorModal = NewMonitorModal(selected.DisplayName(), target)
			a.ActiveModal = ModalMonitor
			return a, fetchMetricsCmd(target)
		}
	case "delete_host":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			name := selected.DisplayName()
			if err := sshconfig.DeleteHost(selected); err != nil {
				a.notify(fmt.Sprintf("Ошибка удаления: %v", err), true)
			} else {
				a.notify(fmt.Sprintf("Хост '%s' удален из ~/.ssh/config", name), false)
				_ = a.HostsView.Reload()
			}
		}
	case "toggle_archive":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			key := selected.PrimaryAlias()
			nowArchived := a.MetaStore.ToggleArchived(key)
			if nowArchived {
				a.notify(fmt.Sprintf("Сервер '%s' перемещен в отстойник", selected.DisplayName()), false)
			} else {
				a.notify(fmt.Sprintf("Сервер '%s' восстановлен из отстойника", selected.DisplayName()), false)
			}
			a.HostsView.ApplyFilter()
		}
	case "cycle_archive_filter":
		a.HostsView.CycleFilterMode()
		modeName := "🟢 Активные"
		if a.HostsView.FilterMode == FilterArchivedOnly {
			modeName = "📦 Отстойник"
		} else if a.HostsView.FilterMode == FilterAll {
			modeName = "📋 Все серверы"
		}
		a.notify(fmt.Sprintf("Режим отображения: %s", modeName), false)
	case "check_all":
		a.notify("Проверка доступности серверов...", false)
		return a, a.checkAllHostsCmd()
	case "rename":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			a.RenameModal = NewRenameModal(selected)
			a.ActiveModal = ModalRenameHost
		}
	case "add_host":
		a.HostModal = NewHostModal()
		a.ActiveModal = ModalAddHost
	case "tunnel_host":
		selected := a.HostsView.SelectedHost()
		var hostNames []string
		for _, h := range a.HostsView.Hosts {
			hostNames = append(hostNames, h.PrimaryAlias())
		}
		var prefillHost string
		if selected != nil {
			prefillHost = selected.PrimaryAlias()
		}
		modal := NewTunnelModal(ModalModeAdd, nil, hostNames)
		if prefillHost != "" {
			modal.Inputs[fieldTunnelHost].SetValue(prefillHost)
		}
		a.TunnelModal = modal
		a.ActiveModal = ModalAddTunnel
	case "reload":
		_ = a.HostsView.Reload()
		a.notify("Конфигурация ~/.ssh/config обновлена", false)
	case "search":
		if a.ActiveTab == TabHosts {
			a.HostsView.Filtering = true
			a.HostsView.FilterInput.Focus()
		} else if a.ActiveTab == TabTunnels {
			a.TunnelsView.Filtering = true
			a.TunnelsView.FilterInput.Focus()
		}
	case "toggle":
		selected := a.TunnelsView.SelectedTunnel()
		if selected != nil {
			if err := a.Manager.ToggleTunnel(selected.ID); err != nil {
				a.notify(fmt.Sprintf("Ошибка: %v", err), true)
			}
			a.TunnelsView.ApplyFilter()
		}
	case "add_tunnel":
		var hostNames []string
		for _, h := range a.HostsView.Hosts {
			hostNames = append(hostNames, h.PrimaryAlias())
		}
		a.TunnelModal = NewTunnelModal(ModalModeAdd, nil, hostNames)
		a.ActiveModal = ModalAddTunnel
	case "edit":
		selected := a.TunnelsView.SelectedTunnel()
		if selected != nil {
			var hostNames []string
			for _, h := range a.HostsView.Hosts {
				hostNames = append(hostNames, h.PrimaryAlias())
			}
			a.TunnelModal = NewTunnelModal(ModalModeEdit, selected, hostNames)
			a.ActiveModal = ModalEditTunnel
		}
	case "delete":
		selected := a.TunnelsView.SelectedTunnel()
		if selected != nil {
			_ = a.Manager.DeleteTunnel(selected.ID)
			a.notify(fmt.Sprintf("Тоннель '%s' удален", selected.Name), false)
			a.TunnelsView.ApplyFilter()
		}
	case "logs":
		selected := a.TunnelsView.SelectedTunnel()
		if selected != nil {
			logs := a.Manager.GetLog(selected.ID)
			a.LogModal = NewLogModal(selected, logs, a.Width, a.Height)
			a.ActiveModal = ModalViewLogs
		}
	case "stop_all":
		a.Manager.StopAll()
		a.notify("Все тоннели остановлены", false)
		a.TunnelsView.ApplyFilter()
	case "open_browser":
		selected := a.TunnelsView.SelectedTunnel()
		if selected != nil {
			url := selected.BrowserURL()
			if url == "" {
				a.notify("Удаленные тоннели (-R) открываются на стороне сервера", true)
			} else {
				if selected.Status != tunnel.StatusActive {
					if err := a.Manager.StartTunnel(selected.ID); err != nil {
						a.notify(fmt.Sprintf("Не удалось запустить тоннель: %v", err), true)
						return a, nil
					}
					a.TunnelsView.ApplyFilter()
				}
				if err := tunnel.OpenBrowser(url); err != nil {
					a.notify(fmt.Sprintf("Ошибка браузера: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Открыто в браузере: %s", url), false)
				}
			}
		}
	case "detach":
		a.Detached = true
		return a, tea.Quit
	case "export":
		a.ExportModal = NewExportModal()
		a.ActiveModal = ModalExport
	case "import":
		a.ImportModal = NewImportModal()
		a.ActiveModal = ModalImport
	case "switch_tab":
		a.ActiveTab = (a.ActiveTab + 1) % 3
	case "quit":
		a.Manager.StopAll()
		return a, tea.Quit
	}
	return a, nil
}

func (a *AppModel) handleHostsTab(msg tea.Msg) tea.Cmd {
	if a.HostsView.Filtering {
		return a.HostsView.Update(msg)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			selected := a.HostsView.SelectedHost()
			if selected != nil {
				return a.connectHost(selected)
			}
		case "o", "O":
			selected := a.HostsView.SelectedHost()
			if selected != nil {
				return a.connectExternalHost(selected)
			}
		case "p", "c":
			a.notify("Проверка доступности серверов...", false)
			return a.checkAllHostsCmd()
		case "m":
			selected := a.HostsView.SelectedHost()
			if selected != nil {
				target := selected.PrimaryAlias()
				a.MonitorModal = NewMonitorModal(selected.DisplayName(), target)
				a.ActiveModal = ModalMonitor
				return fetchMetricsCmd(target)
			}
		case "d":
			selected := a.HostsView.SelectedHost()
			if selected != nil {
				name := selected.DisplayName()
				if err := sshconfig.DeleteHost(selected); err != nil {
					a.notify(fmt.Sprintf("Ошибка удаления: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Хост '%s' удален из ~/.ssh/config", name), false)
					_ = a.HostsView.Reload()
				}
			}
			return nil
		case "z":
			selected := a.HostsView.SelectedHost()
			if selected != nil {
				key := selected.PrimaryAlias()
				nowArchived := a.MetaStore.ToggleArchived(key)
				if nowArchived {
					a.notify(fmt.Sprintf("Сервер '%s' перемещен в отстойник", selected.DisplayName()), false)
				} else {
					a.notify(fmt.Sprintf("Сервер '%s' восстановлен из отстойника", selected.DisplayName()), false)
				}
				a.HostsView.ApplyFilter()
				return nil
			}
		case "Z":
			a.HostsView.CycleFilterMode()
			modeName := "🟢 Активные"
			if a.HostsView.FilterMode == FilterArchivedOnly {
				modeName = "📦 Отстойник"
			} else if a.HostsView.FilterMode == FilterAll {
				modeName = "📋 Все серверы"
			}
			a.notify(fmt.Sprintf("Режим отображения: %s", modeName), false)
			return nil
		case "n":
			selected := a.HostsView.SelectedHost()
			if selected != nil {
				a.RenameModal = NewRenameModal(selected)
				a.ActiveModal = ModalRenameHost
				return nil
			}
		case "a":
			a.HostModal = NewHostModal()
			a.ActiveModal = ModalAddHost
			return nil
		case "t":
			selected := a.HostsView.SelectedHost()
			var hostNames []string
			for _, h := range a.HostsView.Hosts {
				hostNames = append(hostNames, h.PrimaryAlias())
			}
			var prefillHost string
			if selected != nil {
				prefillHost = selected.PrimaryAlias()
			}
			modal := NewTunnelModal(ModalModeAdd, nil, hostNames)
			if prefillHost != "" {
				modal.Inputs[fieldTunnelHost].SetValue(prefillHost)
			}
			a.TunnelModal = modal
			a.ActiveModal = ModalAddTunnel
			return nil
		}
	}

	return a.HostsView.Update(msg)
}

func (a *AppModel) handleTunnelsTab(msg tea.Msg) tea.Cmd {
	if a.TunnelsView.Filtering {
		return a.TunnelsView.Update(msg)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case " ", "enter":
			selected := a.TunnelsView.SelectedTunnel()
			if selected != nil {
				if err := a.Manager.ToggleTunnel(selected.ID); err != nil {
					a.notify(fmt.Sprintf("Ошибка переключения '%s': %v", selected.Name, err), true)
				} else {
					if selected.Status == tunnel.StatusActive {
						a.notify(fmt.Sprintf("Тоннель '%s' запущен на порту %d", selected.Name, selected.LocalPort), false)
					} else {
						a.notify(fmt.Sprintf("Тоннель '%s' остановлен", selected.Name), false)
					}
				}
				a.TunnelsView.ApplyFilter()
			}
			return nil

		case "a":
			var hostNames []string
			for _, h := range a.HostsView.Hosts {
				hostNames = append(hostNames, h.PrimaryAlias())
			}
			a.TunnelModal = NewTunnelModal(ModalModeAdd, nil, hostNames)
			a.ActiveModal = ModalAddTunnel
			return nil

		case "e":
			selected := a.TunnelsView.SelectedTunnel()
			if selected != nil {
				var hostNames []string
				for _, h := range a.HostsView.Hosts {
					hostNames = append(hostNames, h.PrimaryAlias())
				}
				a.TunnelModal = NewTunnelModal(ModalModeEdit, selected, hostNames)
				a.ActiveModal = ModalEditTunnel
			}
			return nil

		case "d":
			selected := a.TunnelsView.SelectedTunnel()
			if selected != nil {
				name := selected.Name
				if err := a.Manager.DeleteTunnel(selected.ID); err != nil {
					a.notify(fmt.Sprintf("Ошибка удаления: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Тоннель '%s' удален", name), false)
				}
				a.TunnelsView.ApplyFilter()
			}
			return nil

		case "s":
			a.Manager.StopAll()
			a.notify("Все тоннели остановлены", false)
			a.TunnelsView.ApplyFilter()
			return nil

		case "l":
			selected := a.TunnelsView.SelectedTunnel()
			if selected != nil {
				logs := a.Manager.GetLog(selected.ID)
				a.LogModal = NewLogModal(selected, logs, a.Width, a.Height)
				a.ActiveModal = ModalViewLogs
			}
			return nil

		case "o":
			selected := a.TunnelsView.SelectedTunnel()
			if selected != nil {
				url := selected.BrowserURL()
				if url == "" {
					a.notify("Удаленные тоннели (-R) открываются на стороне сервера", true)
				} else {
					if selected.Status != tunnel.StatusActive {
						if err := a.Manager.StartTunnel(selected.ID); err != nil {
							a.notify(fmt.Sprintf("Не удалось запустить тоннель: %v", err), true)
							return nil
						}
						a.TunnelsView.ApplyFilter()
					}
					if err := tunnel.OpenBrowser(url); err != nil {
						a.notify(fmt.Sprintf("Ошибка браузера: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Открыто в браузере: %s", url), false)
					}
				}
			}
			return nil
		}
	}

	return a.TunnelsView.Update(msg)
}

func (a *AppModel) handleModalKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch a.ActiveModal {
	case ModalAddTunnel, ModalEditTunnel:
		tun, closed, cmd := a.TunnelModal.Update(msg)
		if closed {
			if tun != nil {
				if a.ActiveModal == ModalAddTunnel {
					if err := a.Manager.AddTunnel(tun); err != nil {
						a.notify(fmt.Sprintf("Ошибка добавления тоннеля: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Тоннель '%s' добавлен", tun.Name), false)
					}
				} else {
					if err := a.Manager.UpdateTunnel(tun); err != nil {
						a.notify(fmt.Sprintf("Ошибка обновления тоннеля: %v", err), true)
					} else {
						a.notify(fmt.Sprintf("Тоннель '%s' обновлен", tun.Name), false)
					}
				}
				a.TunnelsView.ApplyFilter()
			}
			a.ActiveModal = ModalNone
			a.TunnelModal = nil
		}
		return a, cmd

	case ModalAddHost:
		host, closed, cmd := a.HostModal.Update(msg)
		if closed {
			if host != nil {
				if err := sshconfig.AppendHost(host); err != nil {
					a.notify(fmt.Sprintf("Ошибка записи в ~/.ssh/config: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Хост '%s' добавлен в ~/.ssh/config", host.DisplayName()), false)
					_ = a.HostsView.Reload()
				}
			}
			a.ActiveModal = ModalNone
			a.HostModal = nil
		}
		return a, cmd

	case ModalRenameHost:
		newAlias, closed, cmd := a.RenameModal.Update(msg)
		if closed {
			if newAlias != "" {
				oldAlias := a.RenameModal.Host.PrimaryAlias()
				if err := sshconfig.UpdateHostAlias(a.RenameModal.Host, newAlias); err != nil {
					a.notify(fmt.Sprintf("Ошибка переименования: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Хост переименован в '%s'", newAlias), false)
					for _, t := range a.Manager.GetTunnels() {
						if t.Host == oldAlias {
							t.Host = newAlias
							_ = a.Manager.UpdateTunnel(t)
						}
					}
					_ = a.HostsView.Reload()
				}
			}
			a.ActiveModal = ModalNone
			a.RenameModal = nil
		}
		return a, cmd

	case ModalViewLogs:
		closed, cmd := a.LogModal.Update(msg)
		if closed {
			a.ActiveModal = ModalNone
			a.LogModal = nil
		}
		return a, cmd

	case ModalExport:
		path, closed, cmd := a.ExportModal.Update(msg)
		if closed {
			if path != "" {
				savedPath, err := backup.Export(path, a.Manager, a.MetaStore)
				if err != nil {
					a.notify(fmt.Sprintf("Ошибка экспорта: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Резервная копия сохранена в %s", savedPath), false)
				}
			}
			a.ActiveModal = ModalNone
			a.ExportModal = nil
		}
		return a, cmd

	case ModalImport:
		path, overwrite, closed, cmd := a.ImportModal.Update(msg)
		if closed {
			if path != "" {
				res, err := backup.Import(path, a.Manager, a.MetaStore, overwrite)
				if err != nil {
					a.notify(fmt.Sprintf("Ошибка импорта: %v", err), true)
				} else {
					a.notify(fmt.Sprintf("Импортировано: %d тоннелей, %d хостов", res.TunnelsImported, res.HostsAdded), false)
					_ = a.HostsView.Reload()
					a.TunnelsView.ApplyFilter()
				}
			}
			a.ActiveModal = ModalNone
			a.ImportModal = nil
		}
		return a, cmd

	case ModalMonitor:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "esc", "q", "enter":
				a.ActiveModal = ModalNone
				a.MonitorModal = nil
				return a, nil
			case "r":
				if a.MonitorModal != nil {
					a.MonitorModal.Loading = true
					return a, fetchMetricsCmd(a.MonitorModal.Alias)
				}
			}
		}
		return a, nil
	}

	return a, nil
}

func (a *AppModel) View() string {
	if !a.Ready {
		return "\n  Инициализация менеджера SSH..."
	}

	width := a.Width
	if width < 60 {
		width = 60
	}
	height := a.Height
	if height < 15 {
		height = 15
	}

	// 0. Embedded Terminal View Mode (collapsed app header & bottom session tabs)
	if a.ViewMode == ViewModeTerminal {
		active := a.SessionMgr.GetActive()
		var termContent string
		if active != nil {
			termContent = session.RenderVTWithSelection(active.VT, a.Selection, active.ScrollOffset)
		} else {
			termContent = "\n  Нет активных SSH сессий. Нажмите F1 для перехода в Дашборд."
		}

		headerLine, topZones := a.renderCollapsedHeader(width)
		footerLine, bottomZones := a.renderTerminalTabs(width)
		a.TerminalTopZones = topZones
		a.TerminalTabZones = bottomZones

		termHeight := height - 2
		if termHeight < 5 {
			termHeight = 5
		}

		termBox := lipgloss.NewStyle().
			Width(width).
			Height(termHeight).
			Render(termContent)

		return fmt.Sprintf("%s\n%s\n%s", headerLine, termBox, footerLine)
	}

	// 1. Header (Title + Version + Active Tunnels Counter + Clickable Tabs)
	var headerSb strings.Builder
	titleText := fmt.Sprintf("SSHT v%s", version.Current)
	headerSb.WriteString(AppTitleStyle.Render(titleText))
	headerSb.WriteString(" ")
	curX := utf8.RuneCountInString(titleText) + 3

	activeCount := a.Manager.ActiveCount()
	totalCount := len(a.Manager.GetTunnels())
	badgeText := fmt.Sprintf("○ 0/%d АКТИВНО", totalCount)
	if activeCount > 0 {
		badgeText = fmt.Sprintf("● %d/%d АКТИВНО", activeCount, totalCount)
		headerSb.WriteString(ActiveCountBadge.Render(badgeText))
	} else {
		headerSb.WriteString(InactiveCountBadge.Render(badgeText))
	}
	headerSb.WriteString("   ")
	curX += utf8.RuneCountInString(badgeText) + 2 + 3

	a.TabZones = []ButtonZone{}

	tab1Title := "1. SSH Подключения"
	tab1Width := utf8.RuneCountInString(tab1Title) + 4
	tab1Style := TabInactiveStyle
	if a.ActiveTab == TabHosts {
		tab1Style = TabActiveStyle
	}
	a.TabZones = append(a.TabZones, ButtonZone{
		StartX: curX,
		EndX:   curX + tab1Width,
		Action: "tab_hosts",
	})
	headerSb.WriteString(tab1Style.Render(tab1Title))
	headerSb.WriteString(" ")
	curX += tab1Width + 1

	tab2Title := "2. SSH Тоннели"
	tab2Width := utf8.RuneCountInString(tab2Title) + 4
	tab2Style := TabInactiveStyle
	if a.ActiveTab == TabTunnels {
		tab2Style = TabActiveStyle
	}
	a.TabZones = append(a.TabZones, ButtonZone{
		StartX: curX,
		EndX:   curX + tab2Width,
		Action: "tab_tunnels",
	})
	headerSb.WriteString(tab2Style.Render(tab2Title))
	headerSb.WriteString(" ")
	curX += tab2Width + 1

	tab3Title := "3. Справка"
	tab3Width := utf8.RuneCountInString(tab3Title) + 4
	tab3Style := TabInactiveStyle
	if a.ActiveTab == TabHelp {
		tab3Style = TabActiveStyle
	}
	a.TabZones = append(a.TabZones, ButtonZone{
		StartX: curX,
		EndX:   curX + tab3Width,
		Action: "tab_help",
	})
	headerSb.WriteString(tab3Style.Render(tab3Title))

	if a.Notification != "" {
		if a.IsErrorNotif {
			headerSb.WriteString(ErrorNotificationStyle.Render("  " + a.Notification))
		} else {
			headerSb.WriteString(NotificationStyle.Render("  ✔ " + a.Notification))
		}
	}

	// 2. Tab Body
	var body string
	contentHeight := height - 5
	if a.SessionMgr.Count() > 0 {
		contentHeight = height - 6
	}
	if contentHeight < 6 {
		contentHeight = 6
	}

	switch a.ActiveTab {
	case TabHosts:
		body = a.HostsView.View(width, contentHeight)
	case TabTunnels:
		body = a.TunnelsView.View(width, contentHeight)
	case TabHelp:
		body = a.HelpView.View(width, contentHeight)
	}

	bodyBox := PanelStyle.Width(width - 2).Height(contentHeight).Render(body)

	// 3. Footer with clickable hotkey buttons
	var btns []FooterBtn
	switch a.ActiveTab {
	case TabHosts:
		archiveLabel := "Отстойник"
		sel := a.HostsView.SelectedHost()
		if sel != nil && a.MetaStore.IsArchived(sel.PrimaryAlias()) {
			archiveLabel = "Восстановить"
		}
		btns = []FooterBtn{
			{Key: "Enter", Label: "Войти", Action: "connect"},
			{Key: "O", Label: "Внешний", Action: "connect_external"},
			{Key: "m", Label: "Метрики", Action: "metrics"},
			{Key: "d", Label: "Удалить", Action: "delete_host"},
			{Key: "p", Label: "Проверить", Action: "check_all"},
			{Key: "z", Label: archiveLabel, Action: "toggle_archive"},
			{Key: "Z", Label: "Фильтр", Action: "cycle_archive_filter"},
			{Key: "n", Label: "Псевдоним", Action: "rename"},
			{Key: "a", Label: "Добавить", Action: "add_host"},
			{Key: "t", Label: "Тоннель", Action: "tunnel_host"},
			{Key: "b", Label: "В фон", Action: "detach"},
			{Key: "q", Label: "Выход", Action: "quit"},
		}
	case TabTunnels:
		btns = []FooterBtn{
			{Key: "Space", Label: "Вкл/Выкл", Action: "toggle"},
			{Key: "o", Label: "Браузер", Action: "open_browser"},
			{Key: "a", Label: "Добавить", Action: "add_tunnel"},
			{Key: "e", Label: "Изменить", Action: "edit"},
			{Key: "d", Label: "Удалить", Action: "delete"},
			{Key: "l", Label: "Логи", Action: "logs"},
			{Key: "s", Label: "Стоп все", Action: "stop_all"},
			{Key: "b", Label: "В фон", Action: "detach"},
			{Key: "Tab", Label: "Вкладка", Action: "switch_tab"},
			{Key: "q", Label: "Выход", Action: "quit"},
		}
	case TabHelp:
		btns = []FooterBtn{
			{Key: "x", Label: "Экспорт", Action: "export"},
			{Key: "i", Label: "Импорт", Action: "import"},
			{Key: "b", Label: "В фон", Action: "detach"},
			{Key: "Tab", Label: "Сменить вкладку", Action: "switch_tab"},
			{Key: "q", Label: "Выход", Action: "quit"},
		}
	}

	footerLine, footerZones := renderFooter(btns, width)
	a.FooterZones = footerZones

	var mainView string
	if a.SessionMgr.Count() > 0 {
		sessionBarLine, sessionZones := a.renderDashboardSessionBar(width)
		a.SessionBarZones = sessionZones
		mainView = fmt.Sprintf("%s\n%s\n%s\n%s", headerSb.String(), bodyBox, sessionBarLine, footerLine)
	} else {
		a.SessionBarZones = nil
		mainView = fmt.Sprintf("%s\n%s\n%s", headerSb.String(), bodyBox, footerLine)
	}

	if a.ActiveModal != ModalNone {
		var modalView string
		switch a.ActiveModal {
		case ModalAddTunnel, ModalEditTunnel:
			modalView = a.TunnelModal.View()
		case ModalAddHost:
			modalView = a.HostModal.View()
		case ModalRenameHost:
			modalView = a.RenameModal.View()
		case ModalViewLogs:
			modalView = a.LogModal.View()
		case ModalExport:
			modalView = a.ExportModal.View()
		case ModalImport:
			modalView = a.ImportModal.View()
		case ModalMonitor:
			modalView = a.MonitorModal.View()
		}

		return placeOverlay(modalView, mainView, width, height)
	}

	return mainView
}

func (a *AppModel) connectHost(host *sshconfig.HostConfig) tea.Cmd {
	if host == nil {
		return nil
	}
	target := host.PrimaryAlias()
	displayName := host.DisplayName()

	// If already open, switch to it
	idx, existing := a.SessionMgr.FindByTarget(target)
	if existing != nil && !existing.IsClosed() {
		a.SessionMgr.SetActive(idx)
		a.ViewMode = ViewModeTerminal
		a.notify(fmt.Sprintf("Переключено на сессию '%s'", displayName), false)
		return nil
	}

	cols := a.Width
	if cols < 20 {
		cols = 80
	}
	rows := a.Height - 2
	if rows < 5 {
		rows = 24
	}

	_, err := a.SessionMgr.StartSession(displayName, target, cols, rows)
	if err != nil {
		a.notify(fmt.Sprintf("Ошибка запуска терминала: %v", err), true)
		return nil
	}

	a.ViewMode = ViewModeTerminal
	a.notify(fmt.Sprintf("Подключение к '%s'...", displayName), false)
	return nil
}

func (a *AppModel) connectExternalHost(host *sshconfig.HostConfig) tea.Cmd {
	if host == nil {
		return nil
	}
	target := host.PrimaryAlias()
	a.MetaStore.RecordConnection(target)
	c := exec.Command("ssh", target)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return sshExitedMsg{err: err}
	})
}

func (a *AppModel) renderCollapsedHeader(width int) (string, []ButtonZone) {
	var zones []ButtonZone

	appTitle := fmt.Sprintf("▲ SSHT v%s", version.Current)
	appPart := AppTitleStyle.Render(appTitle)
	plainLeft := appTitle + " "

	var hostPart string
	active := a.SessionMgr.GetActive()
	if active != nil {
		statusText := active.StatusText()
		var statusStyle lipgloss.Style
		switch active.GetState() {
		case session.SessionStateConnected:
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
		case session.SessionStateClosed:
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true)
		default: // SessionStateConnecting
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b")).Bold(true)
		}
		hostStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38bdf8")).Bold(true)
		hostPart = fmt.Sprintf(" │ %s: %s (%s)", statusStyle.Render(statusText), hostStyle.Render(active.Alias), active.Target)
		plainLeft += fmt.Sprintf(" │ %s: %s (%s)", statusText, active.Alias, active.Target)
	}

	actTun := a.Manager.ActiveCount()
	var tunPart string
	if actTun > 0 {
		tunBadge := fmt.Sprintf("● %d Тонн.", actTun)
		tunPart = "  " + ActiveCountBadge.Render(tunBadge)
		plainLeft += "  " + tunBadge
	}

	var scrollPart string
	if active != nil && active.ScrollOffset > 0 {
		scrollBadge := fmt.Sprintf(" ▲ Скролл: %d ", active.ScrollOffset)
		scrollPart = " " + lipgloss.NewStyle().
			Background(lipgloss.Color("#78350f")).
			Foreground(lipgloss.Color("#fde68a")).
			Bold(true).
			Render(scrollBadge)
		plainLeft += " " + scrollBadge
	}

	leftRendered := appPart + hostPart + tunPart + scrollPart

	dashBtnText := " [▲ F1 Дашборд] "
	dashBtn := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e293b")).
		Foreground(lipgloss.Color("#f59e0b")).
		Bold(true).
		Render(dashBtnText)

	closeBtnText := " [✕ Закрыть SSH] "
	closeBtn := lipgloss.NewStyle().
		Background(lipgloss.Color("#7f1d1d")).
		Foreground(lipgloss.Color("#fecaca")).
		Bold(true).
		Render(closeBtnText)

	rightRendered := dashBtn + " " + closeBtn
	plainRight := dashBtnText + " " + closeBtnText

	leftLen := utf8.RuneCountInString(plainLeft)
	rightLen := utf8.RuneCountInString(plainRight)
	gap := width - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}

	dashStartX := leftLen + gap
	dashEndX := dashStartX + utf8.RuneCountInString(dashBtnText)
	closeStartX := dashEndX + 1
	closeEndX := width

	zones = append(zones, ButtonZone{
		StartX: 0,
		EndX:   dashEndX,
		Action: "dashboard",
	})
	zones = append(zones, ButtonZone{
		StartX: closeStartX,
		EndX:   closeEndX,
		Action: "close_active",
	})

	line := leftRendered + strings.Repeat(" ", gap) + rightRendered
	topBar := lipgloss.NewStyle().
		Background(lipgloss.Color("#0f172a")).
		Render(line)

	return topBar, zones
}

func (a *AppModel) renderTerminalTabs(width int) (string, []ButtonZone) {
	var sb strings.Builder
	var zones []ButtonZone
	curX := 0

	// 1. Dashboard toggle button
	dashText := " [F1 Дашборд] "
	dashLen := utf8.RuneCountInString(dashText)
	dashStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e293b")).
		Foreground(lipgloss.Color("#f59e0b")).
		Bold(true)
	sb.WriteString(dashStyle.Render(dashText))
	zones = append(zones, ButtonZone{
		StartX: curX,
		EndX:   curX + dashLen,
		Action: "dashboard",
	})
	curX += dashLen

	sb.WriteString(" ")
	curX++

	// 2. Open SSH sessions
	sessions := a.SessionMgr.List()
	activeIdx := a.SessionMgr.ActiveIndex()

	for i, s := range sessions {
		isActive := (i == activeIdx)

		statusIcon := s.StatusIcon()

		tabTitle := fmt.Sprintf(" %d: %s %s ", i+1, s.Alias, statusIcon)
		titleLen := utf8.RuneCountInString(tabTitle)
		closeText := "[✕] "
		closeLen := utf8.RuneCountInString(closeText)

		var titleStyle, closeStyle lipgloss.Style
		if isActive {
			titleStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#0284c7")).
				Foreground(lipgloss.Color("#ffffff")).
				Bold(true)
			closeStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#0284c7")).
				Foreground(lipgloss.Color("#fca5a5")).
				Bold(true)
		} else {
			titleStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e293b")).
				Foreground(lipgloss.Color("#94a3b8"))
			closeStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e293b")).
				Foreground(lipgloss.Color("#ef4444")).
				Bold(true)
		}

		// Zone for switching to tab
		zones = append(zones, ButtonZone{
			StartX: curX,
			EndX:   curX + titleLen,
			Action: fmt.Sprintf("tab_%d", i),
		})
		sb.WriteString(titleStyle.Render(tabTitle))
		curX += titleLen

		// Zone for closing this specific tab & terminating its SSH process
		zones = append(zones, ButtonZone{
			StartX: curX,
			EndX:   curX + closeLen,
			Action: fmt.Sprintf("close_%d", i),
		})
		sb.WriteString(closeStyle.Render(closeText))
		curX += closeLen

		sb.WriteString(" ")
		curX++
	}

	// 3. [+ Новое] button
	newText := " [+ Новое] "
	newLen := utf8.RuneCountInString(newText)
	newStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e293b")).
		Foreground(lipgloss.Color("#10b981")).
		Bold(true)
	zones = append(zones, ButtonZone{
		StartX: curX,
		EndX:   curX + newLen,
		Action: "new_session",
	})
	sb.WriteString(newStyle.Render(newText))
	curX += newLen

	sb.WriteString(" ")
	curX++

	// 4. Dedicated active tab close button: [✕ Закрыть SSH (Ctrl+W)]
	closeActiveText := " [✕ Закрыть SSH (Ctrl+W)] "
	closeActiveLen := utf8.RuneCountInString(closeActiveText)
	closeActiveStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#7f1d1d")).
		Foreground(lipgloss.Color("#fecaca")).
		Bold(true)
	zones = append(zones, ButtonZone{
		StartX: curX,
		EndX:   curX + closeActiveLen,
		Action: "close_active",
	})
	sb.WriteString(closeActiveStyle.Render(closeActiveText))
	curX += closeActiveLen

	// 5. Fill remaining space and show hotkey hints
	hintText := " [Alt+1..9: Вкладки] "
	hintLen := utf8.RuneCountInString(hintText)
	remaining := width - curX - hintLen
	if remaining > 0 {
		sb.WriteString(strings.Repeat(" ", remaining))
	}
	if remaining >= 0 {
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#64748b"))
		sb.WriteString(hintStyle.Render(hintText))
	}

	tabBar := lipgloss.NewStyle().
		Background(lipgloss.Color("#0f172a")).
		Render(sb.String())

	return tabBar, zones
}

func (a *AppModel) renderDashboardSessionBar(width int) (string, []ButtonZone) {
	var sb strings.Builder
	var zones []ButtonZone
	curX := 1

	termBtn := "[▲ F1: Вернуться в Терминал]"
	termLen := utf8.RuneCountInString(termBtn)
	termStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#0284c7")).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)

	sb.WriteString(termStyle.Render(termBtn))
	zones = append(zones, ButtonZone{
		StartX: curX,
		EndX:   curX + termLen,
		Action: "goto_terminal",
	})
	curX += termLen

	sb.WriteString("  ")
	curX += 2

	label := "Открытые SSH: "
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render(label))
	curX += utf8.RuneCountInString(label)

	for i, s := range a.SessionMgr.List() {
		statusIcon := s.StatusIcon()
		tabText := fmt.Sprintf("[%d: %s %s] ", i+1, s.Alias, statusIcon)
		tabLen := utf8.RuneCountInString(tabText)

		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#38bdf8")).
			Bold(true)

		zones = append(zones, ButtonZone{
			StartX: curX,
			EndX:   curX + tabLen,
			Action: fmt.Sprintf("switch_session_%d", i),
		})

		sb.WriteString(style.Render(tabText))
		curX += tabLen
	}

	closeActiveBtn := "[✕ Отключить активный]"
	closeActiveLen := utf8.RuneCountInString(closeActiveBtn)
	closeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#7f1d1d")).
		Foreground(lipgloss.Color("#fca5a5")).
		Bold(true)

	sb.WriteString(" ")
	curX++
	zones = append(zones, ButtonZone{
		StartX: curX,
		EndX:   curX + closeActiveLen,
		Action: "close_active",
	})
	sb.WriteString(closeStyle.Render(closeActiveBtn))

	bar := lipgloss.NewStyle().
		Background(lipgloss.Color("#0f172a")).
		Render(sb.String())

	return bar, zones
}

func renderFooter(btns []FooterBtn, width int) (string, []ButtonZone) {
	var sb strings.Builder
	var zones []ButtonZone
	curX := 1

	for i, b := range btns {
		if i > 0 {
			sb.WriteString("  ")
			curX += 2
		}
		btnRendered := RenderKeyHelp(b.Key, b.Label)
		visWidth := utf8.RuneCountInString(b.Key) + 3 + utf8.RuneCountInString(b.Label)
		zones = append(zones, ButtonZone{
			StartX: curX,
			EndX:   curX + visWidth,
			Action: b.Action,
		})
		sb.WriteString(btnRendered)
		curX += visWidth
	}

	return FooterStyle.Width(width).Render(sb.String()), zones
}

func placeOverlay(modal, background string, width, height int) string {
	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(SubtleColor),
	)
}
