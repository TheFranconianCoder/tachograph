package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TheFranconianCoder/tachograph/internal/shell"
)

func testCandidates() []shell.Candidate {
	return []shell.Candidate{
		{
			Project:  "tachograph",
			Worktree: "tachograph",
			Path:     "/code/tachograph",
			Branch:   "main",
			Watcher:  "watching",
			Main:     true,
		},
		{
			Project:  "tachograph",
			Worktree: "tachograph--feature-x",
			Path:     "/code/tachograph--feature-x",
			Branch:   "feature/x",
			Watcher:  "idle",
		},
	}
}

func TestItemTitleAndFilterValue(t *testing.T) {
	picker := newPicker(testCandidates(), "")
	items := picker.list.Items()
	if len(items) != 2 {
		t.Fatalf("picker has %d items, want 2", len(items))
	}

	main := items[0].(item)
	if !strings.Contains(main.Title(), "main") || !strings.Contains(main.Title(), "watching") {
		t.Errorf("main title = %q", main.Title())
	}

	linked := items[1].(item)
	if !strings.Contains(linked.Title(), "feature-x") {
		t.Errorf("linked title = %q", linked.Title())
	}
	filter := linked.FilterValue()
	for _, want := range []string{"tachograph", "feature-x", "feature/x", "/code/tachograph--feature-x"} {
		if !strings.Contains(filter, want) {
			t.Errorf("FilterValue() = %q, missing %q", filter, want)
		}
	}
}

func TestPickerFilters(t *testing.T) {
	picker := newPicker(testCandidates(), "")
	picker.list.SetFilterText("feature")
	if got := len(picker.list.VisibleItems()); got != 1 {
		t.Fatalf("VisibleItems() = %d, want 1", got)
	}
}

func TestPickerEnterSelects(t *testing.T) {
	picker := newPicker(testCandidates(), "")
	updated, command := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Error("Update(enter) returned no quit command")
	}

	model := updated.(pickerModel)
	if !model.chosen {
		t.Fatal("picker did not record the choice")
	}
	selected, ok := model.list.SelectedItem().(item)
	if !ok {
		t.Fatal("no selected item")
	}
	if selected.candidate.Worktree != "tachograph" {
		t.Errorf("selected worktree = %q, want tachograph", selected.candidate.Worktree)
	}
}

func TestPickerEscCancels(t *testing.T) {
	picker := newPicker(testCandidates(), "")
	updated, command := picker.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command == nil {
		t.Error("Update(esc) returned no quit command")
	}
	if updated.(pickerModel).chosen {
		t.Error("picker recorded a choice after esc")
	}
}

func TestPickerWindowSize(t *testing.T) {
	picker := newPicker(testCandidates(), "")
	updated, _ := picker.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(pickerModel)
	if model.list.Width() <= 0 || model.list.Height() <= 0 {
		t.Errorf("list size = %dx%d, want positive", model.list.Width(), model.list.Height())
	}
}
