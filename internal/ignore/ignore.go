// Package ignore implements a gitignore-like matcher used to exclude files
// from backups.
package ignore

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"
)

type pattern struct {
	negated bool
	dirOnly bool
	segs    []string
}

// Matcher evaluates an ordered list of ignore patterns.
type Matcher struct {
	patterns []pattern
}

// New builds a matcher from raw pattern lines.
func New(lines []string) *Matcher {
	m := &Matcher{}
	for _, line := range lines {
		if p, ok := parse(line); ok {
			m.patterns = append(m.patterns, p)
		}
	}
	return m
}

// LoadFile reads a gitignore-style file. A missing file yields an empty matcher.
func LoadFile(path string) (*Matcher, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Matcher{}, nil
		}
		return nil, fmt.Errorf("cannot read ignore file %s: %w", path, err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("cannot read ignore file %s: %w", path, err)
	}
	return New(lines), nil
}

// Empty reports whether the matcher has no patterns.
func (m *Matcher) Empty() bool { return m == nil || len(m.patterns) == 0 }

// Len returns the number of active patterns.
func (m *Matcher) Len() int {
	if m == nil {
		return 0
	}
	return len(m.patterns)
}

// Match reports whether the given path (relative to a backup root, using
// forward slashes) should be ignored.
func (m *Matcher) Match(rel string, isDir bool) bool {
	if m == nil {
		return false
	}
	rel = strings.Trim(strings.ReplaceAll(rel, `\`, "/"), "/")
	if rel == "" {
		return false
	}
	ignored := false
	for _, p := range m.patterns {
		if p.matches(rel, isDir) {
			ignored = !p.negated
		}
	}
	return ignored
}

func parse(line string) (pattern, bool) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return pattern{}, false
	}
	if strings.HasPrefix(line, "#") {
		return pattern{}, false
	}

	var p pattern
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	} else if strings.HasPrefix(line, "!") {
		p.negated = true
		line = line[1:]
	}

	line = trimTrailingSpaces(line)
	if line == "" {
		return pattern{}, false
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if line == "" {
		return pattern{}, false
	}

	anchored := strings.HasPrefix(line, "/")
	line = strings.TrimPrefix(line, "/")

	segs := strings.Split(line, "/")
	// A pattern with an interior slash is anchored to the root; otherwise it
	// matches the basename at any depth.
	if !anchored && len(segs) == 1 {
		segs = append([]string{"**"}, segs...)
	}
	p.segs = segs
	return p, true
}

func trimTrailingSpaces(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		if len(s) >= 2 && s[len(s)-2] == '\\' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func (p pattern) matches(rel string, isDir bool) bool {
	if !p.dirOnly {
		if p.matchPath(rel) {
			return true
		}
	} else if isDir && p.matchPath(rel) {
		return true
	}
	// An ignored parent directory implies every child is ignored.
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if p.matchPath(strings.Join(parts[:i], "/")) {
			return true
		}
	}
	return false
}

func (p pattern) matchPath(rel string) bool {
	return matchSegs(p.segs, strings.Split(rel, "/"))
}

func matchSegs(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		if matchSegs(pat[1:], name) {
			return true
		}
		return len(name) > 0 && matchSegs(pat, name[1:])
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegs(pat[1:], name[1:])
}
