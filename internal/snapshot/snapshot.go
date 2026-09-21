// Package snapshot defines snapshot file naming and parsing.
package snapshot

import (
	"fmt"
	"regexp"
	"time"
)

const (
	// Suffix is the final snapshot file suffix.
	Suffix = ".tar.zst.age"
	// PartSuffix is the suffix used while an upload is in progress.
	PartSuffix = Suffix + ".part"
	// TimeLayout is the lexicographically sortable timestamp layout.
	TimeLayout = "20060102-150405"
)

var nameRE = regexp.MustCompile(`^(.+)-(\d{8})-(\d{6})` + regexp.QuoteMeta(Suffix) + `$`)

// Snapshot is a parsed snapshot filename.
type Snapshot struct {
	Machine string
	Time    time.Time
	Name    string
	Size    int64
}

// Name builds a snapshot filename for the given machine and time.
func Name(machine string, t time.Time) string {
	return fmt.Sprintf("%s-%s%s", machine, t.Format(TimeLayout), Suffix)
}

// Parse parses a snapshot filename. It reports false for names that do not
// match the tool's naming scheme (including ".part" files).
func Parse(name string) (Snapshot, bool) {
	m := nameRE.FindStringSubmatch(name)
	if m == nil {
		return Snapshot{}, false
	}
	t, err := time.ParseInLocation(TimeLayout, m[2]+"-"+m[3], time.Local)
	if err != nil {
		return Snapshot{}, false
	}
	return Snapshot{Machine: m[1], Time: t, Name: name}, true
}

// IsPart reports whether the name is an in-progress upload.
func IsPart(name string) bool {
	return len(name) > len(PartSuffix) && name[len(name)-len(PartSuffix):] == PartSuffix
}
