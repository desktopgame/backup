package snapshot

import (
	"testing"
	"time"
)

func TestNameAndParse(t *testing.T) {
	loc := time.Local
	when := time.Date(2026, 9, 21, 17, 0, 0, 0, loc)
	name := Name("desktop", when)
	if name != "desktop-20260921-170000.tar.zst.age" {
		t.Fatalf("Name = %q", name)
	}

	s, ok := Parse(name)
	if !ok {
		t.Fatalf("Parse(%q) failed", name)
	}
	if s.Machine != "desktop" {
		t.Errorf("Machine = %q", s.Machine)
	}
	if !s.Time.Equal(when) {
		t.Errorf("Time = %v, want %v", s.Time, when)
	}
	if s.Name != name {
		t.Errorf("Name = %q", s.Name)
	}
}

func TestParseMachineWithDashes(t *testing.T) {
	name := "my-host-2-20260101-000000.tar.zst.age"
	s, ok := Parse(name)
	if !ok {
		t.Fatal("expected parse success")
	}
	if s.Machine != "my-host-2" {
		t.Errorf("Machine = %q, want my-host-2", s.Machine)
	}
}

func TestParseRejectsForeignNames(t *testing.T) {
	bad := []string{
		"backup.tar.zst.age",
		"desktop-20260921-170000.tar.zst.age.part",
		"desktop-20260921-170000.tar.zst",
		"desktop-20260921-170000.tar.gz.age",
		"readme.txt",
	}
	for _, name := range bad {
		if _, ok := Parse(name); ok {
			t.Errorf("Parse(%q) should fail", name)
		}
	}
}

func TestIsPart(t *testing.T) {
	if !IsPart("desktop-20260921-170000.tar.zst.age.part") {
		t.Error("expected .part detection")
	}
	if IsPart("desktop-20260921-170000.tar.zst.age") {
		t.Error("final snapshot must not be a .part")
	}
}
