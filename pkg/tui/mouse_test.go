package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"sshtunnels/pkg/sshconfig"
	"sshtunnels/pkg/tunnel"
)

func TestTerminalTabsZones(t *testing.T) {
	metaStore, _ := sshconfig.NewMetadataStore()
	tunMgr, _ := tunnel.NewManager()
	app := NewAppModel(tunMgr, metaStore)

	// Inject 2 mock sessions
	app.SessionMgr.InjectMockSessionForTest("id-1", "Server1", "s1")
	app.SessionMgr.InjectMockSessionForTest("id-2", "Server2", "s2")

	line, zones := app.renderTerminalTabs(120)
	if len(line) == 0 {
		t.Fatalf("expected non-empty tab line")
	}

	hasTab0, hasClose0, hasTab1, hasClose1, hasDash, hasNew := false, false, false, false, false, false
	for _, z := range zones {
		switch z.Action {
		case "dashboard":
			hasDash = true
		case "tab_0":
			hasTab0 = true
		case "close_0":
			hasClose0 = true
		case "tab_1":
			hasTab1 = true
		case "close_1":
			hasClose1 = true
		case "new_session":
			hasNew = true
		}
	}

	if !hasDash || !hasTab0 || !hasClose0 || !hasTab1 || !hasClose1 || !hasNew {
		t.Fatalf("missing expected tab zones: dash=%v, tab0=%v, close0=%v, tab1=%v, close1=%v, new=%v",
			hasDash, hasTab0, hasClose0, hasTab1, hasClose1, hasNew)
	}

	// Verify zones do not overlap
	for i := 0; i < len(zones)-1; i++ {
		if zones[i].EndX > zones[i+1].StartX {
			t.Errorf("zones overlap: zone %d (%s: %d..%d) and zone %d (%s: %d..%d)",
				i, zones[i].Action, zones[i].StartX, zones[i].EndX,
				i+1, zones[i+1].Action, zones[i+1].StartX, zones[i+1].EndX)
		}
	}
}

func TestMouseTabSwitching(t *testing.T) {
	metaStore, _ := sshconfig.NewMetadataStore()
	tunMgr, _ := tunnel.NewManager()
	app := NewAppModel(tunMgr, metaStore)
	app.Width = 120
	app.Height = 30
	app.Ready = true
	app.ViewMode = ViewModeTerminal

	app.SessionMgr.InjectMockSessionForTest("id-1", "Server1", "s1")
	app.SessionMgr.InjectMockSessionForTest("id-2", "Server2", "s2")
	app.SessionMgr.SetActive(0)

	// Render view to populate zones
	_ = app.View()

	// Find zone for tab_1
	var tab1Zone *ButtonZone
	for _, z := range app.TerminalTabZones {
		if z.Action == "tab_1" {
			tab1Zone = &z
			break
		}
	}
	if tab1Zone == nil {
		t.Fatalf("tab_1 zone not found in TerminalTabZones")
	}

	// Simulate mouse click on tab_1
	clickMsg := tea.MouseMsg{
		X:      (tab1Zone.StartX + tab1Zone.EndX) / 2,
		Y:      app.Height - 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}

	_, _ = app.handleMouse(clickMsg)

	if app.SessionMgr.ActiveIndex() != 1 {
		t.Errorf("expected active tab index 1, got %d", app.SessionMgr.ActiveIndex())
	}
}

func TestMouseSelection(t *testing.T) {
	metaStore, _ := sshconfig.NewMetadataStore()
	tunMgr, _ := tunnel.NewManager()
	app := NewAppModel(tunMgr, metaStore)
	app.Width = 120
	app.Height = 30
	app.Ready = true
	app.ViewMode = ViewModeTerminal

	app.SessionMgr.InjectMockSessionForTest("id-1", "Server1", "s1")

	// 1. Mouse down inside terminal (Y = 5, X = 10)
	pressMsg := tea.MouseMsg{
		X:      10,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	_, _ = app.handleMouse(pressMsg)

	if !app.Selection.Selecting || !app.Selection.Active {
		t.Fatalf("expected selection to be active and selecting")
	}
	if app.Selection.StartX != 10 || app.Selection.StartY != 4 { // termY = Y - 1 = 4
		t.Fatalf("expected selection start at (10, 4), got (%d, %d)", app.Selection.StartX, app.Selection.StartY)
	}

	// 2. Mouse drag to (X = 25, Y = 5)
	motionMsg := tea.MouseMsg{
		X:      25,
		Y:      5,
		Action: tea.MouseActionMotion,
	}
	_, _ = app.handleMouse(motionMsg)

	if app.Selection.EndX != 25 || app.Selection.EndY != 4 {
		t.Fatalf("expected selection end at (25, 4), got (%d, %d)", app.Selection.EndX, app.Selection.EndY)
	}

	// 3. Mouse release
	releaseMsg := tea.MouseMsg{
		X:      25,
		Y:      5,
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
	}
	_, _ = app.handleMouse(releaseMsg)

	if app.Selection.Selecting {
		t.Fatalf("expected selection selecting to be false after release")
	}
	if !app.Selection.Active {
		t.Fatalf("expected selection active to remain true for highlighted text")
	}

	// 4. Right-click while selection is active: COPIES text and CLEARS selection (does NOT paste!)
	rightClickMsg := tea.MouseMsg{
		X:      15,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonRight,
	}
	_, _ = app.handleMouse(rightClickMsg)

	if app.Selection.Active {
		t.Fatalf("expected selection to be cleared after right-click copy")
	}

	// 5. Next Right-click when nothing is selected: PASTES from clipboard
	// (handled without crashing or error)
	_, _ = app.handleMouse(rightClickMsg)
}

func TestMouseWheelScrollback(t *testing.T) {
	metaStore, _ := sshconfig.NewMetadataStore()
	tunMgr, _ := tunnel.NewManager()
	app := NewAppModel(tunMgr, metaStore)
	app.Width = 120
	app.Height = 30
	app.Ready = true
	app.ViewMode = ViewModeTerminal

	sess := app.SessionMgr.InjectMockSessionForTest("id-1", "Server1", "s1")

	// Write 100 lines so that scrollback buffer fills with lines
	for i := 1; i <= 100; i++ {
		sess.VT.Write([]byte("Output log line\r\n"))
	}

	sbLen := sess.VT.ScrollbackLen()
	if sbLen == 0 {
		t.Fatalf("expected non-zero scrollback buffer length")
	}

	// 1. Wheel up inside terminal content area
	wheelUpMsg := tea.MouseMsg{
		X:      20,
		Y:      10,
		Button: tea.MouseButtonWheelUp,
	}
	_, _ = app.handleMouse(wheelUpMsg)

	if sess.ScrollOffset != 3 {
		t.Errorf("expected ScrollOffset == 3 after one wheel up, got %d", sess.ScrollOffset)
	}

	_, _ = app.handleMouse(wheelUpMsg)
	if sess.ScrollOffset != 6 {
		t.Errorf("expected ScrollOffset == 6 after two wheel ups, got %d", sess.ScrollOffset)
	}

	// 2. Wheel down inside terminal content area
	wheelDownMsg := tea.MouseMsg{
		X:      20,
		Y:      10,
		Button: tea.MouseButtonWheelDown,
	}
	_, _ = app.handleMouse(wheelDownMsg)

	if sess.ScrollOffset != 3 {
		t.Errorf("expected ScrollOffset == 3 after one wheel down, got %d", sess.ScrollOffset)
	}

	// 3. Typing any key snaps ScrollOffset back to 0 (live prompt)
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	_, _ = app.Update(keyMsg)

	if sess.ScrollOffset != 0 {
		t.Errorf("expected ScrollOffset == 0 after typing key, got %d", sess.ScrollOffset)
	}
}

func TestMouseCloseTabAndDashboard(t *testing.T) {
	metaStore, _ := sshconfig.NewMetadataStore()
	tunMgr, _ := tunnel.NewManager()
	app := NewAppModel(tunMgr, metaStore)
	app.Width = 120
	app.Height = 30
	app.Ready = true
	app.ViewMode = ViewModeTerminal

	app.SessionMgr.InjectMockSessionForTest("id-1", "Server1", "s1")
	app.SessionMgr.InjectMockSessionForTest("id-2", "Server2", "s2")
	_ = app.View()

	// 1. Click close_0 button
	var close0Zone *ButtonZone
	for _, z := range app.TerminalTabZones {
		if z.Action == "close_0" {
			close0Zone = &z
			break
		}
	}
	if close0Zone == nil {
		t.Fatalf("close_0 zone not found")
	}

	clickCloseMsg := tea.MouseMsg{
		X:      (close0Zone.StartX + close0Zone.EndX) / 2,
		Y:      app.Height - 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	_, _ = app.handleMouse(clickCloseMsg)

	if app.SessionMgr.Count() != 1 {
		t.Fatalf("expected 1 session after close_0, got %d", app.SessionMgr.Count())
	}
	if app.SessionMgr.GetActive().Alias != "Server2" {
		t.Fatalf("expected remaining active session to be Server2, got %s", app.SessionMgr.GetActive().Alias)
	}

	// Re-render
	_ = app.View()

	// 2. Click dashboard button
	var dashZone *ButtonZone
	for _, z := range app.TerminalTabZones {
		if z.Action == "dashboard" {
			dashZone = &z
			break
		}
	}
	if dashZone == nil {
		t.Fatalf("dashboard zone not found")
	}

	clickDashMsg := tea.MouseMsg{
		X:      (dashZone.StartX + dashZone.EndX) / 2,
		Y:      app.Height - 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	_, _ = app.handleMouse(clickDashMsg)

	if app.ViewMode != ViewModeDashboard {
		t.Fatalf("expected ViewModeDashboard after clicking dashboard button, got %v", app.ViewMode)
	}
}

func TestLastClickDoubleClick(t *testing.T) {
	lc := LastClickState{}
	lc.Record(TabHosts, 5)

	if !lc.IsDoubleClick(TabHosts, 5) {
		t.Errorf("immediate second click should be detected as double-click")
	}
	if lc.IsDoubleClick(TabHosts, 6) {
		t.Errorf("click on different row should not be double-click")
	}

	// Wait 500ms
	time.Sleep(500 * time.Millisecond)
	if lc.IsDoubleClick(TabHosts, 5) {
		t.Errorf("click after timeout should not be double-click")
	}
}
