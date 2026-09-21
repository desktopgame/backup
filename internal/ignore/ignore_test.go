package ignore

import "testing"

func TestMatch(t *testing.T) {
	m := New([]string{
		"# comment",
		"",
		"node_modules/",
		".venv/",
		"venv/",
		"__pycache__/",
		"dist/",
		"build/",
		"*.pyc",
		"!keep.pyc",
		"/rootonly",
		"docs/**/*.tmp",
	})

	cases := []struct {
		path string
		dir  bool
		want bool
	}{
		{"node_modules", true, true},
		{"node_modules/x.js", false, true},
		{"a/b/node_modules", true, true},
		{"a/b/node_modules/x.js", false, true},
		{"src/main.go", false, false},
		{"src/__pycache__/x.pyc", false, true},
		{"foo.pyc", false, true},
		{"foo.pyc.bak", false, false},
		{"keep.pyc", false, false},
		{"rootonly", false, true},
		{"sub/rootonly", false, false},
		{"docs/a/b.tmp", false, true},
		{"docs/b.tmp", false, true},
		{"docs/a/b.txt", false, false},
		{".venv/lib/python", false, true},
	}

	for _, tc := range cases {
		if got := m.Match(tc.path, tc.dir); got != tc.want {
			t.Errorf("Match(%q, dir=%v) = %v, want %v", tc.path, tc.dir, got, tc.want)
		}
	}
}

func TestNegationOrder(t *testing.T) {
	m := New([]string{"*.log", "!important.log"})
	if !m.Match("debug.log", false) {
		t.Error("debug.log should be ignored")
	}
	if m.Match("important.log", false) {
		t.Error("important.log should not be ignored")
	}
}

func TestDirOnlyDoesNotMatchFile(t *testing.T) {
	m := New([]string{"build/"})
	if m.Match("build", false) {
		t.Error("a file named build should not match build/")
	}
	if !m.Match("build", true) {
		t.Error("a directory named build should match build/")
	}
}

func TestEmptyMatcher(t *testing.T) {
	m := New(nil)
	if m.Match("anything", false) {
		t.Error("empty matcher must not match")
	}
	if !m.Empty() {
		t.Error("expected empty matcher")
	}
}

func TestParseCount(t *testing.T) {
	m := New([]string{"# c", "", "a/", "!b", "c"})
	if m.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", m.Len())
	}
}
