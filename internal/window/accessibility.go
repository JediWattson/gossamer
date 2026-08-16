package window

import "image"

type AccessibilityRole string

const (
	AccessibilityApplication AccessibilityRole = "application"
	AccessibilityTabList     AccessibilityRole = "tab-list"
	AccessibilityTab         AccessibilityRole = "tab"
	AccessibilityButton      AccessibilityRole = "button"
	AccessibilityTextField   AccessibilityRole = "text-field"
	AccessibilityWebArea     AccessibilityRole = "web-area"
	AccessibilityGroup       AccessibilityRole = "group"
)

// AccessibilityNode is a value-only semantic projection of Graphite chrome.
// Bounds use the same top-left CSS-pixel coordinates as native input.
type AccessibilityNode struct {
	ID       string            `json:"id"`
	ParentID string            `json:"parentId,omitempty"`
	Role     AccessibilityRole `json:"role"`
	Label    string            `json:"label,omitempty"`
	Value    string            `json:"value,omitempty"`
	X        int               `json:"x"`
	Y        int               `json:"y"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	Enabled  bool              `json:"enabled"`
	Selected bool              `json:"selected,omitempty"`
	Focused  bool              `json:"focused,omitempty"`
}

type AccessibilitySnapshot struct {
	Revision uint64              `json:"revision"`
	Nodes    []AccessibilityNode `json:"nodes"`
}

// AccessibilityBackend optionally publishes Graphite's semantic control tree
// to a native accessibility bridge.
type AccessibilityBackend interface {
	UpdateAccessibility(AccessibilitySnapshot) error
}

func accessibilityNode(id, parent string, role AccessibilityRole, label, value string, bounds image.Rectangle, enabled, selected, focused bool) AccessibilityNode {
	return AccessibilityNode{
		ID: id, ParentID: parent, Role: role, Label: label, Value: value,
		X: bounds.Min.X, Y: bounds.Min.Y, Width: bounds.Dx(), Height: bounds.Dy(),
		Enabled: enabled, Selected: selected, Focused: focused,
	}
}

func (shell *graphiteShell) accessibilitySnapshot() AccessibilitySnapshot {
	if shell == nil {
		return AccessibilitySnapshot{}
	}
	layout := shell.layout()
	nodes := []AccessibilityNode{
		accessibilityNode("graphite", "", AccessibilityApplication, "Gossamer browser", "", layout.window, true, false, false),
		accessibilityNode("tabs", "graphite", AccessibilityTabList, "Tabs", "", image.Rect(0, 0, layout.rail.Min.X, graphiteTabHeight), true, false, shell.chromeFocus == shellFocusTab),
	}
	for index, tab := range layout.tabs {
		if index >= len(shell.tabs) || tab.body.Empty() {
			continue
		}
		nodes = append(nodes, accessibilityNode(
			"tab-"+fmtInt(index), "tabs", AccessibilityTab, shellTabTitle(shell.tabs[index].page), "",
			tab.body, true, index == shell.activeTab, shell.chromeFocus == shellFocusTab && index == shell.activeTab,
		))
	}
	nodes = append(nodes,
		accessibilityNode("back", "graphite", AccessibilityButton, "Back", "", layout.back, shell.canGoBack, false, shell.chromeFocus == shellFocusBack),
		accessibilityNode("forward", "graphite", AccessibilityButton, "Forward", "", layout.forward, shell.canGoForward, false, shell.chromeFocus == shellFocusForward),
		accessibilityNode("reload", "graphite", AccessibilityButton, shell.reloadAccessibilityLabel(), "", layout.reload, true, false, shell.chromeFocus == shellFocusReload),
		accessibilityNode("address", "graphite", AccessibilityTextField, "Address", shell.address, layout.address, true, false, shell.chromeFocus == shellFocusAddress),
		accessibilityNode("content", "graphite", AccessibilityWebArea, shellTabTitle(shell.activePage()), "", layout.content, true, false, shell.chromeFocus == shellFocusContent),
	)
	if !layout.newTab.Empty() {
		nodes = append(nodes, accessibilityNode("new-tab", "graphite", AccessibilityButton, "New tab", "", layout.newTab, true, false, shell.chromeFocus == shellFocusNewTab))
	}
	panelNames := [...]string{"DOM inspector", "Computed style inspector", "Layout inspector", "Network inspector", "Memory and ownership inspector"}
	for index, bounds := range layout.railPanels {
		nodes = append(nodes, accessibilityNode(
			"inspector-"+fmtInt(index), "graphite", AccessibilityButton, panelNames[index], "", bounds,
			true, shell.inspectorOpen && shell.inspectorPanel == inspectorPanel(index), shell.chromeFocus == shellFocusInspectorDOM+shellFocus(index),
		))
	}
	if shell.inspectorOpen && !layout.inspector.Empty() {
		nodes = append(nodes, accessibilityNode("inspector-panel", "graphite", AccessibilityGroup, panelNames[shell.inspectorPanel], "", layout.inspector, true, false, false))
	}
	return AccessibilitySnapshot{Revision: shell.revision, Nodes: nodes}
}

func (shell *graphiteShell) reloadAccessibilityLabel() string {
	if shell.loading {
		return "Stop loading"
	}
	return "Reload"
}

func fmtInt(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}
