package dialog

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

type (
	RuntimeResumeMsg struct {
		Response        runtime.ResumeType
		ApproveToolName string
		ApproveShellCmd string
	}
)

// ToolConfirmationResponse represents the user's response to tool confirmation
type ToolConfirmationResponse struct {
	Response string // "approve", "reject", or "approve-session"
}

type toolConfirmationDialog struct {
	width, height int
	msg           *runtime.ToolCallConfirmationEvent
	keyMap        toolConfirmationKeyMap
	sessionState  *service.SessionState
	scrollView    messages.Model
	shellCommand  string
}

// SetSize implements [Dialog].
func (d *toolConfirmationDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height

	// Calculate dialog dimensions
	dialogWidth := width * 70 / 100
	contentWidth := dialogWidth - 6
	maxDialogHeight := (height * 80) / 100

	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")
	titleHeight := lipgloss.Height(title)

	separatorWidth := max(contentWidth-10, 20)
	separator := styles.DialogSeparatorStyle.
		Align(lipgloss.Center).
		Width(contentWidth).
		Render(strings.Repeat("─", separatorWidth))
	separatorHeight := lipgloss.Height(separator)

	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	questionHeight := lipgloss.Height(question)

	optionsText := "[Y]es    [N]o    [T]ool    [A]ll (approve all tools this session)"
	if d.shellCommand != "" {
		optionsText = fmt.Sprintf("[Y]es    [N]o    [T]ool    [C]md (%s)    [A]ll (approve all tools this session)", d.shellCommand)
	}

	options := styles.DialogOptionsStyle.Width(contentWidth).Render(optionsText)
	optionsHeight := lipgloss.Height(options)

	// Calculate available height for scroll view
	// Total = maxDialogHeight - title - separator - 2 empty lines - question - empty line - options - 4 (dialog padding/border)
	availableHeight := max(maxDialogHeight-titleHeight-separatorHeight-2-questionHeight-1-optionsHeight-4, 5)
	d.scrollView.SetSize(contentWidth, availableHeight)

	return nil
}

// toolConfirmationKeyMap defines key bindings for tool confirmation dialog
type toolConfirmationKeyMap struct {
	Yes     key.Binding
	No      key.Binding
	All     key.Binding
	Tool    key.Binding
	Command key.Binding
}

// defaultToolConfirmationKeyMap returns default key bindings
func defaultToolConfirmationKeyMap() toolConfirmationKeyMap {
	return toolConfirmationKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("Y", "approve"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("N", "reject"),
		),
		Tool: key.NewBinding(
			key.WithKeys("t", "T"),
			key.WithHelp("T", "approve tool"),
		),
		Command: key.NewBinding(
			key.WithKeys("c", "C"),
			key.WithHelp("C", "approve cmd"),
		),
		All: key.NewBinding(
			key.WithKeys("a", "A"),
			key.WithHelp("A", "approve all"),
		),
	}
}

// NewToolConfirmationDialog creates a new tool confirmation dialog
func NewToolConfirmationDialog(msg *runtime.ToolCallConfirmationEvent, sessionState *service.SessionState) Dialog {
	// Create scrollable view with initial size (will be updated in SetSize)
	scrollView := messages.NewScrollableView(100, 20, sessionState)

	// Add the tool call message to the view
	scrollView.AddOrUpdateToolCall(
		"", // agentName - empty for dialog context
		msg.ToolCall,
		msg.ToolDefinition,
		types.ToolStatusConfirmation,
	)

	return &toolConfirmationDialog{
		msg:          msg,
		sessionState: sessionState,
		keyMap:       defaultToolConfirmationKeyMap(),
		scrollView:   scrollView,
		shellCommand: extractShellCommand(msg.ToolCall.Function.Arguments, msg.ToolCall.Function.Name),
	}
}

// Init initializes the tool confirmation dialog
func (d *toolConfirmationDialog) Init() tea.Cmd {
	return d.scrollView.Init()
}

// Update handles messages for the tool confirmation dialog
func (d *toolConfirmationDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Yes):
			return d, tea.Sequence(core.CmdHandler(CloseDialogMsg{}), core.CmdHandler(RuntimeResumeMsg{Response: runtime.ResumeTypeApprove}))
		case key.Matches(msg, d.keyMap.Tool):
			return d, tea.Sequence(core.CmdHandler(CloseDialogMsg{}), core.CmdHandler(RuntimeResumeMsg{
				Response:        runtime.ResumeTypeApprove,
				ApproveToolName: d.msg.ToolCall.Function.Name,
			}))
		case key.Matches(msg, d.keyMap.Command) && d.shellCommand != "":
			return d, tea.Sequence(core.CmdHandler(CloseDialogMsg{}), core.CmdHandler(RuntimeResumeMsg{
				Response:        runtime.ResumeTypeApprove,
				ApproveShellCmd: d.shellCommand,
			}))
		case key.Matches(msg, d.keyMap.No):
			return d, tea.Sequence(core.CmdHandler(CloseDialogMsg{}), core.CmdHandler(RuntimeResumeMsg{Response: runtime.ResumeTypeReject}))
		case key.Matches(msg, d.keyMap.All):
			return d, tea.Sequence(core.CmdHandler(CloseDialogMsg{}), core.CmdHandler(RuntimeResumeMsg{Response: runtime.ResumeTypeApproveSession}))
		}

		if msg.String() == "ctrl+c" {
			return d, tea.Quit
		}

		// Forward scrolling keys to the scroll view
		switch msg.String() {
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			updatedScrollView, cmd := d.scrollView.Update(msg)
			d.scrollView = updatedScrollView.(messages.Model)
			return d, cmd
		}

	case tea.MouseWheelMsg:
		// Forward mouse wheel events to scroll view
		updatedScrollView, cmd := d.scrollView.Update(msg)
		d.scrollView = updatedScrollView.(messages.Model)
		return d, cmd
	}

	return d, nil
}

// View renders the tool confirmation dialog
func (d *toolConfirmationDialog) View() string {
	dialogWidth := d.width * 70 / 100

	// Content width (accounting for padding and borders)
	contentWidth := dialogWidth - 6

	dialogStyle := styles.DialogStyle.Width(dialogWidth)

	// Title
	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")

	// Separator
	separatorWidth := max(contentWidth-10, 20)
	separator := styles.DialogSeparatorStyle.
		Align(lipgloss.Center).
		Width(contentWidth).
		Render(strings.Repeat("─", separatorWidth))

	// Get scrollable tool call view
	argumentsSection := d.scrollView.View()

	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	options := styles.DialogOptionsStyle.Width(contentWidth).Render("[Y]es    [N]o    [A]ll (approve all tools this session)")

	// Combine all parts with proper spacing
	parts := []string{title, separator}

	if argumentsSection != "" {
		parts = append(parts, "", argumentsSection)
	}

	parts = append(parts, "", question, "", options)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return dialogStyle.Render(content)
}

// Position calculates the position to center the dialog
func (d *toolConfirmationDialog) Position() (row, col int) {
	dialogWidth := d.width * 70 / 100

	// Calculate actual dialog height by rendering it
	renderedDialog := d.View()
	dialogHeight := lipgloss.Height(renderedDialog)

	// Ensure dialog stays on screen
	row = max(0, (d.height-dialogHeight)/2)
	col = max(0, (d.width-dialogWidth)/2)
	return row, col
}

func extractShellCommand(arguments, toolName string) string {
	if toolName != builtin.ToolNameShell {
		return ""
	}

	var payload struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}

	fields := strings.Fields(payload.Cmd)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}
