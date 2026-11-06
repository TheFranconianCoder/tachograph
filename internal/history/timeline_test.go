package history

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/TheFranconianCoder/tachograph/internal/difft"
)

func TestTimelineAndSnapshotDetail(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")
	store := New(filepath.Join(base, "history"))

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(project, "a.txt"), "base\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if recorder.Worktree.ID == "" {
		t.Fatal("Ensure returned an empty worktree ID")
	}

	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, entry := range known {
		if filepath.Clean(entry.Path) != filepath.Clean(worktree) {
			continue
		}
		found = true
		if entry.ID != recorder.Worktree.ID {
			t.Errorf("known worktree ID = %q, want %q", entry.ID, recorder.Worktree.ID)
		}
		rebuilt := store.RecorderFromKnown(entry)
		if rebuilt.Worktree.ID != recorder.Worktree.ID {
			t.Errorf("known recorder ID = %q, want %q", rebuilt.Worktree.ID, recorder.Worktree.ID)
		}
		if rebuilt.Project.MainWorktree != project {
			t.Errorf("known recorder main worktree = %q, want %q", rebuilt.Project.MainWorktree, project)
		}
	}
	if !found {
		t.Fatalf("List did not return the ensured worktree %s", worktree)
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	first, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "b.txt"), "new file\n")
	third, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("third snapshot: %v", err)
	}

	page, err := recorder.Timeline(ctx, 50, 0, "")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(page.Entries) != 4 {
		t.Fatalf("timeline entries = %d, want 3 snapshots + base", len(page.Entries))
	}
	if page.HasMore {
		t.Error("HasMore = true, want false")
	}
	newest := page.Entries[0]
	if newest.Kind != "snapshot" || newest.Branch != "feature" || newest.Head == "" {
		t.Errorf("newest entry = %+v, want a feature snapshot with head", newest)
	}
	if newest.Hash != third.Hash {
		t.Errorf("newest hash = %s, want %s", newest.Hash, third.Hash)
	}
	if last := page.Entries[len(page.Entries)-1]; last.Kind != "base" {
		t.Errorf("oldest entry kind = %q, want base", last.Kind)
	}

	limited, err := recorder.Timeline(ctx, 2, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Entries) != 2 || !limited.HasMore {
		t.Errorf("limited page = %d entries, hasMore=%v; want 2,true", len(limited.Entries), limited.HasMore)
	}
	next, err := recorder.Timeline(ctx, 2, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 2 || next.HasMore {
		t.Errorf("second page = %d entries, hasMore=%v; want 2,false", len(next.Entries), next.HasMore)
	}
	if next.Entries[0].Hash == limited.Entries[0].Hash {
		t.Error("offset did not advance the timeline")
	}

	filtered, err := recorder.Timeline(ctx, 50, 0, "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Entries) != 1 || filtered.Entries[0].Hash != third.Hash {
		t.Errorf("b.txt timeline = %+v, want only the commit that added b.txt", filtered.Entries)
	}

	detail, err := recorder.SnapshotDetail(ctx, third.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail: %v", err)
	}
	if detail.Kind != "snapshot" || len(detail.Files) != 1 || detail.Files[0].Path != "b.txt" {
		t.Errorf("detail = %+v, want one added b.txt", detail)
	}
	if !strings.Contains(detail.Diff, "+new file") {
		t.Errorf("diff = %q, want the added line", detail.Diff)
	}

	firstDetail, err := recorder.SnapshotDetail(ctx, first.Hash, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(firstDetail.Files) != 1 || firstDetail.Files[0].Status != "A" {
		t.Errorf("first snapshot files = %+v, want an added a.txt", firstDetail.Files)
	}

	baseDetail, err := recorder.SnapshotDetail(ctx, page.Entries[len(page.Entries)-1].Hash, "")
	if err != nil {
		t.Fatal(err)
	}
	if baseDetail.Kind != "base" || len(baseDetail.Files) != 0 || baseDetail.Diff != "" {
		t.Errorf("base detail = %+v, want no files and no diff", baseDetail)
	}

	if _, err := recorder.SnapshotDetail(ctx, "../../etc/passwd", ""); err == nil {
		t.Error("SnapshotDetail accepted an invalid hash")
	}
	if _, err := recorder.SnapshotDetail(ctx, strings.Repeat("0", 40), ""); !errors.Is(err, ErrSnapshotNotFound) {
		t.Errorf("SnapshotDetail(missing) error = %v, want ErrSnapshotNotFound", err)
	}
}

func TestSnapshotDetailUsesDifftAndFallsBackToGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}
	ctx := t.Context()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")
	store := New(filepath.Join(base, "history"))

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(project, "a.txt"), "base\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	fake := filepath.Join(base, "difft")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '\\033[91mSTRUCTURAL DIFF\\033[0m\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	store.Diff = difft.Tool{Name: "difft", Path: fake}

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	detail, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail: %v", err)
	}
	if detail.DiffTool != "difft" {
		t.Errorf("DiffTool = %q, want difft", detail.DiffTool)
	}
	if !strings.Contains(detail.Diff, "STRUCTURAL DIFF") {
		t.Errorf("diff = %q, want the external diff output", detail.Diff)
	}
	if !strings.Contains(detail.Diff, "\x1b[91m") {
		t.Errorf("diff = %q, want the ANSI colour codes preserved", detail.Diff)
	}

	// A missing binary must not break the timeline: Git's unified diff is the
	// fallback.
	recorder.Diff = difft.Tool{Name: "difft", Path: filepath.Join(base, "missing")}
	fallback, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail fallback: %v", err)
	}
	if fallback.DiffTool != "git" {
		t.Errorf("fallback DiffTool = %q, want git", fallback.DiffTool)
	}
	if !strings.Contains(fallback.Diff, "+one") {
		t.Errorf("fallback diff = %q, want the git patch", fallback.Diff)
	}
}

func TestSnapshotDetailScopesDiffToFile(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")
	store := New(filepath.Join(base, "history"))

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(project, "a.txt"), "a1\n")
	writeFile(t, filepath.Join(project, "b.txt"), "b1\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "a2\n")
	writeFile(t, filepath.Join(worktree, "b.txt"), "b2\n")
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	full, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail: %v", err)
	}
	if len(full.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(full.Files))
	}
	if !strings.Contains(full.Diff, "a/a.txt") || !strings.Contains(full.Diff, "b/b.txt") {
		t.Errorf("full diff does not contain both files: %q", full.Diff)
	}

	scoped, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "a.txt")
	if err != nil {
		t.Fatalf("SnapshotDetail(a.txt): %v", err)
	}
	if scoped.File != "a.txt" {
		t.Errorf("File = %q, want a.txt", scoped.File)
	}
	if len(scoped.Files) != 2 {
		t.Errorf("scoped files = %d, want the complete list of 2", len(scoped.Files))
	}
	if !strings.Contains(scoped.Diff, "+a2") {
		t.Errorf("scoped diff = %q, want the a.txt change", scoped.Diff)
	}
	if strings.Contains(scoped.Diff, "b/b.txt") || strings.Contains(scoped.Diff, "+b2") {
		t.Errorf("scoped diff leaked b.txt: %q", scoped.Diff)
	}

	if _, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "missing.txt"); !errors.Is(err, ErrFileNotInSnapshot) {
		t.Errorf("SnapshotDetail(missing.txt) error = %v, want ErrFileNotInSnapshot", err)
	}
}

func TestClassifySubject(t *testing.T) {
	cases := []struct {
		subject string
		kind    string
		branch  string
		head    string
	}{
		{"snap: 2026-11-06T12:00:00Z [feature@1a2b3c4]", "snapshot", "feature", "1a2b3c4"},
		{"snap: 2026-11-06T12:00:00Z [main@abc]", "snapshot", "main", "abc"},
		{"base: history initialized", "base", "", ""},
		{"archive: 2026-07-01T12:00:00Z", "archive", "", ""},
		{"something else", "commit", "", ""},
	}
	for _, test := range cases {
		kind, branch, head := classifySubject(test.subject)
		if kind != test.kind || branch != test.branch || head != test.head {
			t.Errorf("classifySubject(%q) = %q,%q,%q; want %q,%q,%q",
				test.subject, kind, branch, head, test.kind, test.branch, test.head)
		}
	}
}

func TestSnapshotRange(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")
	store := New(filepath.Join(base, "history"))

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(project, "a.txt"), "base\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	snapshots := make([]Snapshot, 0, 4)
	for i, content := range []string{"one\n", "two\n"} {
		writeFile(t, filepath.Join(worktree, "a.txt"), content)
		snapshot, err := recorder.Snapshot(ctx)
		if err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	writeFile(t, filepath.Join(worktree, "b.txt"), "new\n")
	if snapshot, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("snapshot b: %v", err)
	} else {
		snapshots = append(snapshots, snapshot)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "three\n")
	if snapshot, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("snapshot three: %v", err)
	} else {
		snapshots = append(snapshots, snapshot)
	}

	// snapshots[1] through snapshots[3] are three contiguous changes.
	detail, err := recorder.RangeDetail(ctx, snapshots[1].Hash, snapshots[3].Hash, "")
	if err != nil {
		t.Fatalf("RangeDetail: %v", err)
	}
	if detail.Kind != "range" || detail.From != snapshots[1].Hash || detail.Hash != snapshots[3].Hash {
		t.Errorf("detail = %+v, want a range from snapshots[1] to snapshots[3]", detail)
	}
	if detail.Count != 3 {
		t.Errorf("count = %d, want 3", detail.Count)
	}
	if len(detail.Files) != 2 {
		t.Errorf("files = %+v, want a.txt and b.txt", detail.Files)
	}
	if !strings.Contains(detail.Diff, "+three") || !strings.Contains(detail.Diff, "b.txt") {
		t.Errorf("range diff = %q, want the combined change", detail.Diff)
	}
	if strings.Contains(detail.Diff, "+one") || strings.Contains(detail.Diff, "+two") {
		t.Errorf("range diff includes changes from before the range: %q", detail.Diff)
	}

	scoped, err := recorder.RangeDetail(ctx, snapshots[1].Hash, snapshots[3].Hash, "b.txt")
	if err != nil {
		t.Fatalf("RangeDetail(b.txt): %v", err)
	}
	if len(scoped.Files) != 2 {
		t.Errorf("scoped files = %+v, want the complete list", scoped.Files)
	}
	if !strings.Contains(scoped.Diff, "b.txt") || strings.Contains(scoped.Diff, "a.txt") {
		t.Errorf("scoped range diff = %q, want only b.txt", scoped.Diff)
	}

	// A single selected snapshot is the same range with one change.
	single, err := recorder.RangeDetail(ctx, snapshots[2].Hash, snapshots[2].Hash, "")
	if err != nil {
		t.Fatalf("RangeDetail(single): %v", err)
	}
	if single.Count != 1 {
		t.Errorf("single count = %d, want 1", single.Count)
	}

	// A range that starts at the root includes the root itself.
	page, err := recorder.Timeline(ctx, 50, 0, "")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	root := page.Entries[len(page.Entries)-1]
	fromRoot, err := recorder.RangeDetail(ctx, root.Hash, snapshots[0].Hash, "")
	if err != nil {
		t.Fatalf("RangeDetail(root): %v", err)
	}
	if fromRoot.Count != 2 {
		t.Errorf("root range count = %d, want 2 (base + first snapshot)", fromRoot.Count)
	}
	if !strings.Contains(fromRoot.Diff, "+one") {
		t.Errorf("root range diff = %q, want the first snapshot change", fromRoot.Diff)
	}

	// Reversed endpoints are not a timeline.
	if _, err := recorder.RangeDetail(ctx, snapshots[3].Hash, snapshots[1].Hash, ""); err == nil {
		t.Error("RangeDetail accepted reversed endpoints")
	}
}
