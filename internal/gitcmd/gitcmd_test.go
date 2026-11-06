package gitcmd

import "testing"

func TestIsWrite(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"add", "-A"}, true},
		{[]string{"commit", "-m", "snapshot"}, true},
		{[]string{"config", "--worktree", "core.worktree", "/tmp/x"}, true},
		{[]string{"config", "--worktree", "--get", "core.worktree"}, false},
		{[]string{"init", "-b", "main"}, true},
		{[]string{"worktree", "add", "--no-checkout", "/tmp/x"}, true},
		{[]string{"worktree", "list", "--porcelain"}, false},
		{[]string{"diff", "--cached", "--quiet"}, false},
		{[]string{"rev-parse", "HEAD"}, false},
		{[]string{"log", "-1"}, false},
		{[]string{"branch", "--show-current"}, false},
		{[]string{"stage", "file"}, false},
	}

	for _, test := range cases {
		if got := IsWrite(test.args); got != test.want {
			t.Errorf("IsWrite(%v) = %v, want %v", test.args, got, test.want)
		}
	}
}

func TestShouldLog(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"add", "-A"}, false},
		{[]string{"commit", "-m", "snap: test"}, true},
		{[]string{"config", "--worktree", "core.worktree", "/tmp/x"}, true},
		{[]string{"config", "--worktree", "--get", "core.worktree"}, false},
		{[]string{"init", "-b", "main"}, true},
		{[]string{"worktree", "add", "/tmp/x"}, true},
		{[]string{"worktree", "list", "--porcelain"}, false},
		{[]string{"diff", "--cached", "--quiet"}, false},
	}
	for _, test := range cases {
		if got := ShouldLog(test.args); got != test.want {
			t.Errorf("ShouldLog(%v) = %v, want %v", test.args, got, test.want)
		}
	}
}
