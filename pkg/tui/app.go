package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshtunnels/pkg/backup"
	"sshtunnels/pkg/monitoring"
	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tunnel"
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

	return &AppModel{
		Manager:     m,
		MetaStore:   meta,
		ActiveTab:   TabHosts,
		HostsView:   hv,
		TunnelsView: tv,
		HelpView:    help,
		ActiveModal: ModalNone,
	}
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
	)
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
		if a.ActiveModal != ModalNone {
			return a.handleModalKey(msg)
		}

		isFiltering := (a.ActiveTab == TabHosts && a.HostsView.Filtering) ||
			(a.ActiveTab == TabTunnels && a.TunnelsView.Filtering)

		if !isFiltering {
			switch msg.String() {
			case "ctrl+c", "q":
				a.Manager.StopAll()
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

	// 3. Click on Footer Buttons (Y == a.Height - 1)
	if msg.Y == a.Height-1 && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
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
					target := selected.PrimaryAlias()
					a.MetaStore.RecordConnection(target)
					c := exec.Command("ssh", target)
					return a, tea.ExecProcess(c, func(err error) tea.Msg {
						return sshExitedMsg{err: err}
					})
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
	switch action {
	case "connect":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			target := selected.PrimaryAlias()
			a.MetaStore.RecordConnection(target)
			c := exec.Command("ssh", target)
			return a, tea.ExecProcess(c, func(err error) tea.Msg {
				return sshExitedMsg{err: err}
			})
		}
	case "metrics":
		selected := a.HostsView.SelectedHost()
		if selected != nil {
			target := selected.PrimaryAlias()
			a.MonitorModal = NewMonitorModal(selected.DisplayName(), target)
			a.ActiveModal = ModalMonitor
			return a, fetchMetricsCmd(target)
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
				target := selected.PrimaryAlias()
				a.MetaStore.RecordConnection(target)
				c := exec.Command("ssh", target)
				return tea.ExecProcess(c, func(err error) tea.Msg {
					return sshExitedMsg{err: err}
				})
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

	// 1. Header (Title + Active Tunnels Counter + Clickable Tabs)
	var headerSb strings.Builder
	headerSb.WriteString(AppTitleStyle.Render(" SSHT "))
	curX := 8

	activeCount := a.Manager.ActiveCount()
	totalCount := len(a.Manager.GetTunnels())
	badgeText := fmt.Sprintf("○ 0/%d АКТИВНО", totalCount)
	if activeCount > 0 {
		badgeText = fmt.Sprintf("● %d/%d АКТИВНО", activeCount, totalCount)
		headerSb.WriteString(ActiveCountBadge.Render(badgeText))
	} else {
		headerSb.WriteString(InactiveCountBadge.Render(badgeText))
	}
	curX += 1 + utf8.RuneCountInString(badgeText) + 2 + 3
	headerSb.WriteString("   ")

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
	if contentHeight < 8 {
		contentHeight = 8
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
			{Key: "m", Label: "Метрики", Action: "metrics"},
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

	mainView := fmt.Sprintf("%s\n%s\n%s", headerSb.String(), bodyBox, footerLine)

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
