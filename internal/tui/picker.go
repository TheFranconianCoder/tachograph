// Package tui renders the small worktree picker. It follows the list-based
// approach of the studio CLIs: a bubbles list with the default delegate and no
// extra chrome.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/TheFranconianCoder/tachograph/internal/shell"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type item struct {
	candidate shell.Candidate
}

func (i item) Title() string       { return i.candidate.Label() }
func (i item) Description() string { return "" }
func (i item) FilterValue() string {
	c := i.candidate
	return strings.Join([]string{c.Project, c.Worktree, c.Branch, c.Path, c.RecorderState()}, " ")
}

type pickerModel struct {
	list   list.Model
	chosen bool
}

func newPicker(candidates []shell.Candidate, filter string) pickerModel {
	items := make([]list.Item, len(candidates))
	for index, candidate := range candidates {
		items[index] = item{candidate: candidate}
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	model := list.New(items, delegate, 0, 14)
	model.Title = "Select a worktree"
	model.DisableQuitKeybindings()
	helpKeys := func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	}
	model.AdditionalShortHelpKeys = helpKeys
	model.AdditionalFullHelpKeys = helpKeys
	if filter != "" {
		model.SetFilterText(filter)
	}
	return pickerModel{list: model}
}

// Pick runs the picker and returns the chosen worktree. The boolean is false
// when the user cancelled.
func Pick(candidates []shell.Candidate, filter string) (shell.Candidate, bool, error) {
	program := tea.NewProgram(newPicker(candidates, filter), tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return shell.Candidate{}, false, err
	}

	picker, ok := final.(pickerModel)
	if !ok {
		return shell.Candidate{}, false, fmt.Errorf("unexpected picker model %T", final)
	}
	if !picker.chosen {
		return shell.Candidate{}, false, nil
	}

	selected, ok := picker.list.SelectedItem().(item)
	if !ok {
		return shell.Candidate{}, false, fmt.Errorf("no worktree selected")
	}
	return selected.candidate, true, nil
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// While the list is filtering, keys belong to the filter input.
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "enter":
			m.chosen = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		horizontal, vertical := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-horizontal, msg.Height-vertical)
	}

	var command tea.Cmd
	m.list, command = m.list.Update(msg)
	return m, command
}

func (m pickerModel) View() string {
	return docStyle.Render(m.list.View())
}
