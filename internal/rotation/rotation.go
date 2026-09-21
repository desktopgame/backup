// Package rotation implements the daily/weekly/monthly retention policy.
package rotation

import (
	"math"
	"sort"
	"time"

	"github.com/desktopgame/backup/internal/snapshot"
)

// Policy describes how many representative snapshots to keep per period.
type Policy struct {
	Daily   int
	Weekly  int
	Monthly int
}

// PlanSnapshots selects which snapshots to keep and which to remove.
//
// The newest snapshot is always kept. Within a period bucket only the newest
// snapshot is retained. The result is deterministic for a given set of
// snapshots and reference time.
func PlanSnapshots(snaps []snapshot.Snapshot, policy Policy, ref time.Time) (keep, remove []snapshot.Snapshot) {
	if len(snaps) == 0 {
		return nil, nil
	}

	sorted := make([]snapshot.Snapshot, len(snaps))
	copy(sorted, snaps)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time.After(sorted[j].Time)
	})

	refDate := dateOnly(ref)
	buckets := map[string]snapshot.Snapshot{}
	keepNames := map[string]bool{}

	for _, s := range sorted {
		d := dateOnly(s.Time)
		days := dayDiff(refDate, d)
		if days < 0 {
			days = 0
		}
		weeks := dayDiff(weekStart(refDate), weekStart(d)) / 7
		if weeks < 0 {
			weeks = 0
		}
		months := monthDiff(refDate, d)
		if months < 0 {
			months = 0
		}

		var key string
		switch {
		case policy.Daily > 0 && days < policy.Daily:
			key = "d:" + d.Format("20060102")
		case policy.Weekly > 0 && weeks < policy.Weekly:
			key = "w:" + weekStart(d).Format("20060102")
		case policy.Monthly > 0 && months < policy.Monthly:
			key = "m:" + d.Format("200601")
		default:
			continue
		}

		if _, ok := buckets[key]; !ok {
			buckets[key] = s
			keepNames[s.Name] = true
		}
	}

	// The newest snapshot must never be removed.
	keepNames[sorted[0].Name] = true

	for _, s := range sorted {
		if keepNames[s.Name] {
			keep = append(keep, s)
		} else {
			remove = append(remove, s)
		}
	}
	return keep, remove
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func dayDiff(later, earlier time.Time) int {
	return int(math.Round(later.Sub(earlier).Hours() / 24))
}

func weekStart(t time.Time) time.Time {
	d := dateOnly(t)
	offset := (int(d.Weekday()) + 6) % 7 // Monday == 0
	return d.AddDate(0, 0, -offset)
}

func monthDiff(later, earlier time.Time) int {
	return (later.Year()-earlier.Year())*12 + int(later.Month()-earlier.Month())
}
