package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanRetentionTiers(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	ages := []time.Duration{
		100 * 24 * time.Hour,
		40 * 24 * time.Hour,
		8*24*time.Hour + 2*time.Hour,
		8*24*time.Hour + time.Hour,
		8 * 24 * time.Hour,
		31 * time.Hour,
		30 * time.Hour,
		29*time.Hour + 50*time.Minute,
		0,
	}
	commits := make([]commitInfo, len(ages))
	for i, age := range ages {
		commits[i] = commitInfo{Time: now.Add(-age)}
	}

	opts := RetentionOptions{
		KeepAllFor: 24 * time.Hour,
		HourlyFor:  7 * 24 * time.Hour,
		DailyFor:   30 * 24 * time.Hour,
		MaxAge:     90 * 24 * time.Hour,
	}
	plan := planRetention(commits, now, opts)

	// 40d is the weekly survivor, 8d the daily survivor, 31h and 29h50m are the
	// hourly survivors, 0 is the tip. The same-hour/same-day duplicates and the
	// entry beyond maxAge are dropped.
	want := []bool{false, true, false, false, true, true, false, true, true}
	if len(plan.keep) != len(want) {
		t.Fatalf("keep length = %d, want %d", len(plan.keep), len(want))
	}
	for i := range want {
		if plan.keep[i] != want[i] {
			t.Errorf("commit %d (age %s) keep = %v, want %v",
				i, ages[i], plan.keep[i], want[i])
		}
	}
	if plan.kept != 5 || plan.dropped != 4 {
		t.Errorf("kept/dropped = %d/%d, want 5/4", plan.kept, plan.dropped)
	}
	if plan.firstKept != 1 || plan.leadingDropped != 1 {
		t.Errorf("firstKept/leadingDropped = %d/%d, want 1/1",
			plan.firstKept, plan.leadingDropped)
	}
}

func TestPlanRetentionKeepsMinimumAndTip(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	commits := make([]commitInfo, 6)
	for i := range commits {
		commits[i] = commitInfo{Time: now.Add(-time.Duration(len(commits)-i) * 24 * time.Hour)}
	}
	opts := RetentionOptions{
		KeepMinimum: 2,
		MaxAge:      time.Nanosecond,
		Archive:     true,
	}
	plan := planRetention(commits, now, opts)

	kept := 0
	for _, keep := range plan.keep {
		if keep {
			kept++
		}
	}
	if kept != 2 {
		t.Fatalf("kept = %d, want 2 (the minimum)", kept)
	}
	if !plan.keep[len(plan.keep)-1] {
		t.Error("tip was dropped")
	}
	if plan.firstKept != len(commits)-2 {
		t.Errorf("firstKept = %d, want %d", plan.firstKept, len(commits)-2)
	}
}

func TestPlanRetentionKeepsArchiveRoot(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	commits := []commitInfo{
		{Time: now.Add(-100 * 24 * time.Hour), Message: "archive: 2026-07-29T12:00:00Z"},
		{Time: now.Add(-2 * 24 * time.Hour), Message: "snap: old"},
		{Time: now.Add(-24 * time.Hour), Message: "snap: middle"},
		{Time: now, Message: "snap: tip"},
	}
	plan := planRetention(commits, now, RetentionOptions{
		KeepMinimum: 1,
		MaxAge:      time.Nanosecond,
	})
	if !plan.keep[0] {
		t.Error("archive root was dropped")
	}
	if !plan.keep[len(plan.keep)-1] {
		t.Error("tip was dropped")
	}
	if plan.kept != 2 || plan.dropped != 2 {
		t.Errorf("kept/dropped = %d/%d, want 2/2", plan.kept, plan.dropped)
	}
}

func TestCompactProjectDropsOldCommitsAndKeepsTip(t *testing.T) {
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
	snapshots := make([]Snapshot, 0, 5)
	for i := 0; i < 5; i++ {
		writeFile(t, filepath.Join(worktree, "a.txt"), fmt.Sprintf("value %d\n", i))
		snapshot, err := recorder.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot %d: %v", i, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if _, err := recorder.SetLabel(ctx, snapshots[0].Hash, "old"); err != nil {
		t.Fatalf("SetLabel old: %v", err)
	}
	if _, err := recorder.SetLabel(ctx, snapshots[len(snapshots)-1].Hash, "keep me"); err != nil {
		t.Fatalf("SetLabel tip: %v", err)
	}

	repo := filepath.Join(store.Root, recorder.Project.Dir, "repo")
	branch := recorder.Worktree.HistoryBranch
	beforeTree := strings.TrimSpace(mustGit(t, repo, "rev-parse", branch+"^{tree}"))

	result, err := store.compactProjectAt(ctx, recorder.Project.Dir, RetentionOptions{
		KeepMinimum: 2,
		MaxAge:      time.Nanosecond,
		Archive:     true,
		AutoGC:      true,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("compactProjectAt: %v", err)
	}
	if result.Branches != 1 || result.Rewritten != 1 {
		t.Fatalf("result = %+v, want one rewritten branch", result)
	}
	if result.Dropped == 0 {
		t.Fatalf("result = %+v, want dropped commits", result)
	}

	summary, err := recorder.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Snapshots != 2 {
		t.Errorf("snapshots after compaction = %d, want 2", summary.Snapshots)
	}

	count := strings.TrimSpace(mustGit(t, repo, "rev-list", "--count", branch))
	if count != "3" {
		t.Errorf("branch commits = %s, want 3 (archive + two kept snapshots)", count)
	}

	afterTree := strings.TrimSpace(mustGit(t, repo, "rev-parse", branch+"^{tree}"))
	if afterTree != beforeTree {
		t.Errorf("tip tree changed: %s -> %s", beforeTree, afterTree)
	}

	// Labels follow the rewritten commits; the dropped snapshot loses its own.
	newTip := strings.TrimSpace(mustGit(t, repo, "rev-parse", branch))
	labels, err := readLabels(labelPath(repo))
	if err != nil {
		t.Fatalf("readLabels: %v", err)
	}
	if labels[newTip] != "keep me" {
		t.Errorf("labels = %v, want %q on the new tip %s", labels, "keep me", newTip)
	}
	if len(labels) != 1 {
		t.Errorf("labels = %v, want only the kept snapshot label", labels)
	}
	detail, err := recorder.SnapshotDetail(ctx, newTip, "")
	if err != nil {
		t.Fatalf("SnapshotDetail: %v", err)
	}
	if detail.Label != "keep me" {
		t.Errorf("snapshot label after compaction = %q, want %q", detail.Label, "keep me")
	}

	// A second run with the same policy must not rewrite the compacted branch
	// again (the archive root is preserved).
	second, err := store.compactProjectAt(ctx, recorder.Project.Dir, RetentionOptions{
		KeepMinimum: 2,
		MaxAge:      time.Nanosecond,
		Archive:     true,
		AutoGC:      true,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("second compactProjectAt: %v", err)
	}
	if second.Rewritten != 0 {
		t.Errorf("second compaction rewrote the branch again: %+v", second)
	}

	// The placeholder must still accept new snapshots on the rewritten branch.
	writeFile(t, filepath.Join(worktree, "a.txt"), "after compaction\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot after compaction: %v", err)
	}
	summary, err = recorder.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary after new snapshot: %v", err)
	}
	if summary.Snapshots != 3 {
		t.Errorf("snapshots after new snapshot = %d, want 3", summary.Snapshots)
	}
}
