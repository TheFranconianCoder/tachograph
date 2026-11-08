package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCandidatesFlatten(t *testing.T) {
	status := Status{Projects: []Project{
		{
			Name:         "alpha",
			CommonDir:    "/git/alpha",
			MainWorktree: "/code/alpha",
			Worktrees: []Worktree{
				{Path: "/code/alpha", Branch: "main", Watcher: "watching", ID: "a1", HistoryRepo: "/hist/a"},
				{Path: "/code/alpha--feature", Branch: "feature/x", State: "ready", ID: "a2", HistoryRepo: "/hist/a"},
				{Path: "/code/alpha--feature", Branch: "duplicate"},
			},
		},
		{
			Name:         "beta",
			CommonDir:    "/git/beta",
			MainWorktree: "/code/beta",
			Worktrees: []Worktree{
				{Path: "/code/beta", Branch: "main"},
			},
		},
	}}

	candidates := Candidates(status)
	if len(candidates) != 3 {
		t.Fatalf("Candidates() returned %d entries, want 3", len(candidates))
	}

	main := candidates[0]
	if !main.Main {
		t.Errorf("first candidate is not marked as main")
	}
	if main.Worktree != "alpha" || main.MainWorktree != "/code/alpha" {
		t.Errorf("main candidate = %+v", main)
	}
	if main.CommonDir != "/git/alpha" {
		t.Errorf("CommonDir = %q, want /git/alpha", main.CommonDir)
	}
	if main.RecorderState() != "watching" {
		t.Errorf("RecorderState() = %q, want watching", main.RecorderState())
	}

	linked := candidates[1]
	if linked.Main {
		t.Errorf("linked candidate is marked as main")
	}
	if linked.Worktree != "alpha--feature" {
		t.Errorf("Worktree = %q, want alpha--feature", linked.Worktree)
	}
	if linked.RecorderState() != "ready" {
		t.Errorf("RecorderState() = %q, want ready", linked.RecorderState())
	}

	if candidates[2].Project != "beta" {
		t.Errorf("second project candidate = %+v", candidates[2])
	}
}

func TestCandidatesEmpty(t *testing.T) {
	if got := Candidates(Status{}); len(got) != 0 {
		t.Fatalf("Candidates() = %+v, want empty", got)
	}
}

func TestLabel(t *testing.T) {
	main := Candidate{Project: "alpha", Worktree: "alpha", Branch: "main", Watcher: "watching", Main: true}
	if got := main.Label(); got != "alpha  main  watching" {
		t.Errorf("main Label() = %q", got)
	}

	linked := Candidate{Project: "alpha", Worktree: "alpha--feature", Branch: "feature/x"}
	if got := linked.Label(); got != "alpha / alpha--feature  feature/x" {
		t.Errorf("linked Label() = %q", got)
	}
}

func testCandidates(t *testing.T) ([]Candidate, string, string) {
	t.Helper()
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	feature := filepath.Join(root, "alpha--feature")
	for _, path := range []string{filepath.Join(alpha, "src", "deep"), feature} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
	}
	return []Candidate{
		{Project: "alpha", CommonDir: "/git/alpha", Worktree: "alpha", Path: alpha, Branch: "main", Main: true},
		{Project: "alpha", CommonDir: "/git/alpha", Worktree: "alpha--feature", Path: feature, Branch: "feature/x"},
	}, alpha, feature
}

func TestCurrent(t *testing.T) {
	candidates, alpha, feature := testCandidates(t)
	deep := filepath.Join(alpha, "src", "deep")

	current, ok := Current(candidates, deep)
	if !ok || current.Path != alpha {
		t.Fatalf("Current(subdirectory) = %+v, %v; want %s", current, ok, alpha)
	}

	current, ok = Current(candidates, feature)
	if !ok || current.Path != feature {
		t.Fatalf("Current(worktree) = %+v, %v; want %s", current, ok, feature)
	}

	if current, ok := Current(candidates, filepath.Dir(alpha)); ok {
		t.Fatalf("Current(unmanaged) = %+v, true; want false", current)
	}
}

func TestCurrentFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs additional privileges on Windows")
	}
	candidates, alpha, _ := testCandidates(t)
	link := filepath.Join(t.TempDir(), "alpha-link")
	if err := os.Symlink(alpha, link); err != nil {
		t.Skipf("symlink: %v", err)
	}

	current, ok := Current(candidates, link)
	if !ok || current.Path != alpha {
		t.Fatalf("Current(symlink) = %+v, %v; want %s", current, ok, alpha)
	}
}

func TestRelated(t *testing.T) {
	candidates := []Candidate{
		{Project: "alpha", CommonDir: "/git/alpha", Path: "/code/alpha", Main: true},
		{Project: "alpha", CommonDir: "/git/alpha", Path: "/code/alpha--feature"},
		{Project: "alpha", CommonDir: "/git/other", Path: "/code/other-alpha"},
		{Project: "beta", CommonDir: "/git/beta", Path: "/code/beta", Main: true},
	}

	related := Related(candidates, candidates[1])
	if len(related) != 2 {
		t.Fatalf("Related() returned %d worktrees, want 2: %+v", len(related), related)
	}
	if related[0].Path != "/code/alpha" || related[1].Path != "/code/alpha--feature" {
		t.Errorf("Related() = %+v", related)
	}

	fallback := Related(
		[]Candidate{{Project: "alpha", Path: "/a"}, {Project: "beta", Path: "/b"}},
		Candidate{Project: "alpha"},
	)
	if len(fallback) != 1 || fallback[0].Path != "/a" {
		t.Errorf("Related(name fallback) = %+v", fallback)
	}
}

func TestChildren(t *testing.T) {
	main := Candidate{Project: "alpha", CommonDir: "/git/alpha", Path: "/code/alpha", Main: true}
	first := Candidate{Project: "alpha", CommonDir: "/git/alpha", Path: "/code/alpha--one"}
	second := Candidate{Project: "alpha", CommonDir: "/git/alpha", Path: "/code/alpha--two"}
	other := Candidate{Project: "beta", CommonDir: "/git/beta", Path: "/code/beta", Main: true}

	children := Children([]Candidate{main, first, second, other}, main)
	if len(children) != 2 || children[0].Path != first.Path || children[1].Path != second.Path {
		t.Fatalf("Children() = %+v, want the two linked worktrees", children)
	}

	// From a child the root is still excluded; the caller uses Children to
	// decide whether there is any target besides the current project root.
	children = Children([]Candidate{main, first, second}, first)
	if len(children) != 2 {
		t.Fatalf("Children(from child) = %+v, want both linked worktrees", children)
	}
}

func TestMatch(t *testing.T) {
	candidates, alpha, feature := testCandidates(t)
	deep := filepath.Join(alpha, "src", "deep")

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"absolute path", alpha, []string{alpha}},
		{"subdirectory", deep, []string{alpha}},
		{"project worktree", "alpha/alpha--feature", []string{feature}},
		{"directory name", "alpha--feature", []string{feature}},
		{"directory name case-insensitive", "ALPHA--FEATURE", []string{feature}},
		{"branch", "feature/x", []string{feature}},
		{"substring", "featur", []string{feature}},
		{"ambiguous substring", "alph", []string{alpha, feature}},
		{"no match", "nope", nil},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			matches := Match(candidates, test.query)
			if len(matches) != len(test.want) {
				t.Fatalf("Match(%q) returned %d matches, want %d", test.query, len(matches), len(test.want))
			}
			for index, path := range test.want {
				if matches[index].Path != path {
					t.Errorf("Match(%q)[%d] = %s, want %s", test.query, index, matches[index].Path, path)
				}
			}
		})
	}
}

func TestMatchRelativePath(t *testing.T) {
	candidates, alpha, _ := testCandidates(t)
	root := filepath.Dir(alpha)
	t.Chdir(root)

	matches := Match(candidates, filepath.Join("alpha", "src"))
	if len(matches) != 1 || matches[0].Path != alpha {
		t.Fatalf("Match(relative subdirectory) = %+v, want alpha", matches)
	}
}

func TestMatchBranchInsideWorktree(t *testing.T) {
	candidates, alpha, feature := testCandidates(t)
	t.Chdir(filepath.Join(alpha, "src"))

	// feature/x does not exist relative to the worktree, so it must be treated
	// as a branch instead of a path inside alpha.
	matches := Match(candidates, "feature/x")
	if len(matches) != 1 || matches[0].Path != feature {
		t.Fatalf("Match(branch from worktree) = %+v, want feature", matches)
	}
}

func TestMatchHomePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir does not follow HOME on Windows")
	}
	candidates, alpha, _ := testCandidates(t)
	t.Setenv("HOME", filepath.Dir(alpha))

	matches := Match(candidates, "~/alpha")
	if len(matches) != 1 || matches[0].Path != alpha {
		t.Fatalf("Match(~/alpha) = %+v, want alpha", matches)
	}
}

func TestMatchEmptyQuery(t *testing.T) {
	candidates, _, _ := testCandidates(t)
	if got := Match(candidates, "   "); len(got) != 0 {
		t.Fatalf("Match(empty) = %+v, want none", got)
	}
}

func TestPathWithin(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if !pathWithin(root, child) {
		t.Errorf("pathWithin(%q, %q) = false, want true", root, child)
	}
	if pathWithin(child, root) {
		t.Errorf("pathWithin(%q, %q) = true, want false", child, root)
	}
	if !pathWithin(root, root) {
		t.Errorf("pathWithin(%q, %q) = false, want true", root, root)
	}
}
