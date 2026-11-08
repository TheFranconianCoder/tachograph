package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RetentionOptions controls how old snapshots are thinned out.
//
// The tiers are applied by age: every snapshot inside KeepAllFor is kept, then
// one per hour, then one per day, then one per week. MaxAge drops the rest;
// zero keeps the weekly tier forever. KeepMinimum always keeps the newest N
// commits, and the tip is never dropped.
type RetentionOptions struct {
	KeepMinimum int
	KeepAllFor  time.Duration
	HourlyFor   time.Duration
	DailyFor    time.Duration
	MaxAge      time.Duration
	Archive     bool
	AutoGC      bool
}

// CompactResult summarizes one project compaction.
type CompactResult struct {
	Branches         int
	Rewritten        int
	Kept             int
	Dropped          int
	GarbageCollected bool
}

type commitInfo struct {
	Hash    string
	Tree    string
	Time    time.Time
	Message string
}

// CompactProject thins the snapshot branches of one project and reclaims the
// dropped objects. It is serialized with snapshots through the repository lock.
func (s *Store) CompactProject(ctx context.Context, projectDir string, opts RetentionOptions) (CompactResult, error) {
	return s.compactProjectAt(ctx, projectDir, opts, time.Now())
}

func (s *Store) compactProjectAt(ctx context.Context, projectDir string, opts RetentionOptions, now time.Time) (CompactResult, error) {
	result := CompactResult{}
	if projectDir == "" {
		return result, fmt.Errorf("project dir is required")
	}

	lock := s.repoLock(projectDir)
	lock.Lock()
	defer lock.Unlock()

	repo := filepath.Join(s.Root, projectDir, "repo")
	if _, err := os.Stat(repo); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return result, fmt.Errorf("inspect history repo %s: %w", repo, err)
	}

	output, err := runGit(ctx, repo, "for-each-ref", "--format=%(refname)", "refs/heads/wt/")
	if err != nil {
		return result, fmt.Errorf("list history branches: %w", err)
	}

	// Old hash -> new hash for every commit that survives. Labels follow this
	// map; commits that are missing from it are gone.
	remap := make(map[string]string)

	for _, ref := range strings.Split(strings.TrimSpace(output), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		result.Branches++

		initRef := initRefForBranch(ref)
		initTarget := refValue(ctx, repo, initRef)

		commits, err := branchCommits(ctx, repo, ref)
		if err != nil {
			return result, err
		}
		if len(commits) == 0 {
			continue
		}
		plan := planRetention(commits, now, opts)
		if plan.dropped == 0 {
			for _, commit := range commits {
				remap[commit.Hash] = commit.Hash
			}
			result.Kept += len(commits)
			continue
		}

		newTip, branchRemap, err := rewriteBranch(ctx, repo, commits, plan, opts.Archive)
		if err != nil {
			return result, fmt.Errorf("rewrite %s: %w", ref, err)
		}
		oldTip := commits[len(commits)-1].Hash
		if newTip == oldTip {
			for _, commit := range commits {
				remap[commit.Hash] = commit.Hash
			}
			result.Kept += len(commits)
			continue
		}
		for oldHash, newHash := range branchRemap {
			remap[oldHash] = newHash
		}
		if _, stderr, err := runGitRaw(ctx, repo, "update-ref", ref, newTip, oldTip); err != nil {
			return result, fmt.Errorf("update %s: %w (%s)", ref, err, strings.TrimSpace(stderr))
		}
		if initTarget != "" {
			if mapped, ok := branchRemap[initTarget]; ok {
				if _, stderr, err := runGitRaw(ctx, repo, "update-ref", initRef, mapped); err != nil {
					return result, fmt.Errorf("remap %s: %w (%s)", initRef, err, strings.TrimSpace(stderr))
				}
			} else if _, stderr, err := runGitRaw(ctx, repo, "update-ref", "-d", initRef); err != nil {
				return result, fmt.Errorf("drop %s: %w (%s)", initRef, err, strings.TrimSpace(stderr))
			}
		}
		result.Rewritten++
		result.Kept += plan.kept
		result.Dropped += plan.dropped
	}

	if result.Rewritten > 0 {
		if err := remapLabels(repo, remap); err != nil {
			return result, fmt.Errorf("remap snapshot labels: %w", err)
		}
		if opts.AutoGC {
			if err := collectGarbage(ctx, repo); err != nil {
				return result, err
			}
			result.GarbageCollected = true
		}
	} else if opts.AutoGC {
		// Pack loose objects even when no history was rewritten. --auto keeps
		// this cheap when there is nothing to do.
		if _, stderr, err := runGitRaw(ctx, repo, "gc", "--auto", "--quiet"); err != nil {
			return result, fmt.Errorf("gc --auto: %w (%s)", err, strings.TrimSpace(stderr))
		}
		result.GarbageCollected = true
	}

	return result, nil
}

// branchCommits returns the branch's commits oldest first, with their tree,
// author date and subject.
func branchCommits(ctx context.Context, repo, ref string) ([]commitInfo, error) {
	output, err := runGit(ctx, repo, "log", "--reverse", "--format=%H%x00%T%x00%aI%x00%s", ref)
	if err != nil {
		return nil, fmt.Errorf("log %s: %w", ref, err)
	}
	var commits []commitInfo
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("unexpected log line for %s: %q", ref, line)
		}
		at, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			return nil, fmt.Errorf("parse commit date %q: %w", parts[2], err)
		}
		commits = append(commits, commitInfo{
			Hash:    parts[0],
			Tree:    parts[1],
			Time:    at,
			Message: parts[3],
		})
	}
	return commits, nil
}

type retentionPlan struct {
	keep           []bool
	firstKept      int
	leadingDropped int
	kept           int
	dropped        int
}

func planRetention(commits []commitInfo, now time.Time, opts RetentionOptions) retentionPlan {
	plan := retentionPlan{keep: make([]bool, len(commits)), firstKept: len(commits)}

	minimum := opts.KeepMinimum
	if minimum < 0 {
		minimum = 0
	}
	if minimum > len(commits) {
		minimum = len(commits)
	}
	for i := len(commits) - minimum; i < len(commits); i++ {
		plan.keep[i] = true
	}
	if len(commits) > 0 {
		// The tip is the current state; it is never dropped.
		plan.keep[len(commits)-1] = true
	}

	hours := make(map[string]bool)
	days := make(map[string]bool)
	weeks := make(map[string]bool)

	for i := len(commits) - 1; i >= 0; i-- {
		if plan.keep[i] {
			continue
		}
		// An archive root is the deliberate base of a compacted branch. It is
		// older than maxAge by design and must not be rewritten on every run.
		if strings.HasPrefix(commits[i].Message, "archive:") {
			plan.keep[i] = true
			continue
		}
		at := commits[i].Time
		age := now.Sub(at)
		if age < 0 {
			plan.keep[i] = true
			continue
		}

		keep := false
		switch {
		case opts.KeepAllFor > 0 && age <= opts.KeepAllFor:
			keep = true
		case opts.HourlyFor > 0 && age <= opts.HourlyFor:
			key := at.UTC().Format("2006-01-02T15")
			if !hours[key] {
				hours[key] = true
				keep = true
			}
		case opts.MaxAge > 0 && age > opts.MaxAge:
			keep = false
		case opts.DailyFor > 0 && age <= opts.DailyFor:
			key := at.UTC().Format("2006-01-02")
			if !days[key] {
				days[key] = true
				keep = true
			}
		default:
			key := weekKey(at)
			if !weeks[key] {
				weeks[key] = true
				keep = true
			}
		}
		if keep {
			plan.keep[i] = true
		}
	}

	for i, keep := range plan.keep {
		if keep {
			if plan.firstKept == len(commits) {
				plan.firstKept = i
			}
			plan.kept++
		} else {
			plan.dropped++
		}
	}
	if plan.firstKept == len(commits) {
		// Defensive: never produce an empty branch.
		plan.keep[len(commits)-1] = true
		plan.firstKept = len(commits) - 1
		plan.kept = 1
		plan.dropped = len(commits) - 1
	}
	plan.leadingDropped = plan.firstKept
	return plan
}

func weekKey(at time.Time) string {
	year, week := at.UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// initRefForBranch maps a history branch to its initialization baseline ref.
func initRefForBranch(ref string) string {
	dir := strings.TrimPrefix(ref, "refs/heads/wt/")
	if dir == ref {
		dir = filepath.Base(ref)
	}
	return initRefPrefix + dir
}

// refValue returns a ref's commit hash, or an empty string when it does not
// exist or cannot be read.
func refValue(ctx context.Context, repo, ref string) string {
	hash, err := runGit(ctx, repo, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return hash
}

// rewriteBranch rebuilds the branch from the kept commits. With Archive, the
// state immediately before the first kept commit becomes an archive root, so
// the pre-retention state survives even when its snapshots do not.
func rewriteBranch(ctx context.Context, repo string, commits []commitInfo, plan retentionPlan, archive bool) (string, map[string]string, error) {
	parent := ""
	if archive && plan.leadingDropped > 0 && plan.firstKept > 0 {
		base := commits[plan.firstKept-1]
		message := "archive: " + base.Time.UTC().Format(time.RFC3339)
		hash, err := commitTree(ctx, repo, base.Tree, "", base.Time, message)
		if err != nil {
			return "", nil, err
		}
		parent = hash
	}

	remap := make(map[string]string)
	for i := plan.firstKept; i < len(commits); i++ {
		if !plan.keep[i] {
			continue
		}
		hash, err := commitTree(ctx, repo, commits[i].Tree, parent, commits[i].Time, commits[i].Message)
		if err != nil {
			return "", nil, err
		}
		remap[commits[i].Hash] = hash
		parent = hash
	}
	if parent == "" {
		return "", nil, fmt.Errorf("retention kept no commits")
	}
	return parent, remap, nil
}

func commitTree(ctx context.Context, repo, tree, parent string, at time.Time, message string) (string, error) {
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)
	date := at.UTC().Format(time.RFC3339)
	env := []string{
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
	}
	hash, err := runGitEnv(ctx, repo, env, args...)
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

// collectGarbage drops the rewritten commits from the reflog and prunes their
// objects. The repository lock is held by the caller, so no snapshot can race
// with the prune.
func collectGarbage(ctx context.Context, repo string) error {
	if _, stderr, err := runGitRaw(ctx, repo, "reflog", "expire", "--expire=now", "--all"); err != nil {
		return fmt.Errorf("expire reflog: %w (%s)", err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := runGitRaw(ctx, repo, "gc", "--prune=now", "--quiet"); err != nil {
		return fmt.Errorf("gc: %w (%s)", err, strings.TrimSpace(stderr))
	}
	return nil
}
