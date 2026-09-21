package rotation

import (
	"testing"
	"time"

	"github.com/desktopgame/backup/internal/snapshot"
)

func snap(t *testing.T, machine string, y int, mo time.Month, d, h int) snapshot.Snapshot {
	t.Helper()
	when := time.Date(y, mo, d, h, 0, 0, 0, time.Local)
	return snapshot.Snapshot{Machine: machine, Time: when, Name: snapshot.Name(machine, when)}
}

func TestPlanSnapshots(t *testing.T) {
	ref := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	policy := Policy{Daily: 7, Weekly: 4, Monthly: 6}

	snaps := []snapshot.Snapshot{
		snap(t, "h", 2026, 9, 21, 8), // newest, today
		snap(t, "h", 2026, 9, 21, 7), // same day -> removed
		snap(t, "h", 2026, 9, 20, 8), // daily
		snap(t, "h", 2026, 9, 15, 8), // daily (6 days)
		snap(t, "h", 2026, 9, 14, 8), // weekly (7 days)
		snap(t, "h", 2026, 9, 7, 8),  // weekly
		snap(t, "h", 2026, 8, 31, 8), // weekly
		snap(t, "h", 2026, 8, 24, 8), // monthly (4 weeks)
		snap(t, "h", 2026, 4, 1, 8),  // monthly (5 months)
		snap(t, "h", 2026, 3, 1, 8),  // 6 months -> removed
		snap(t, "h", 2025, 1, 1, 8),  // too old -> removed
	}

	keep, remove := PlanSnapshots(snaps, policy, ref)

	keepSet := map[string]bool{}
	for _, s := range keep {
		keepSet[s.Name] = true
	}
	removeSet := map[string]bool{}
	for _, s := range remove {
		removeSet[s.Name] = true
	}

	wantKeep := []snapshot.Snapshot{snaps[0], snaps[2], snaps[3], snaps[4], snaps[5], snaps[6], snaps[7], snaps[8]}
	for _, s := range wantKeep {
		if !keepSet[s.Name] {
			t.Errorf("expected %s to be kept", s.Name)
		}
	}
	wantRemove := []snapshot.Snapshot{snaps[1], snaps[9], snaps[10]}
	for _, s := range wantRemove {
		if !removeSet[s.Name] {
			t.Errorf("expected %s to be removed", s.Name)
		}
	}
	if len(keep)+len(remove) != len(snaps) {
		t.Errorf("partition mismatch: keep=%d remove=%d total=%d", len(keep), len(remove), len(snaps))
	}
}

func TestNewestAlwaysKept(t *testing.T) {
	ref := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	snaps := []snapshot.Snapshot{
		snap(t, "h", 2026, 9, 21, 8),
		snap(t, "h", 2020, 1, 1, 8),
	}
	keep, remove := PlanSnapshots(snaps, Policy{}, ref)
	if len(keep) != 1 || keep[0].Name != snaps[0].Name {
		t.Fatalf("keep = %v, want only the newest", keep)
	}
	if len(remove) != 1 || remove[0].Name != snaps[1].Name {
		t.Fatalf("remove = %v, want only the old snapshot", remove)
	}
}

func TestKeepNewestPerBucket(t *testing.T) {
	ref := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	newest := snap(t, "h", 2026, 9, 20, 20)
	olderSameDay := snap(t, "h", 2026, 9, 20, 6)
	keep, remove := PlanSnapshots([]snapshot.Snapshot{newest, olderSameDay}, Policy{Daily: 7}, ref)
	if len(keep) != 1 || keep[0].Name != newest.Name {
		t.Fatalf("keep = %v, want newest in bucket", keep)
	}
	if len(remove) != 1 || remove[0].Name != olderSameDay.Name {
		t.Fatalf("remove = %v, want older same-day snapshot", remove)
	}
}
