package app

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/desktopgame/backup/internal/pipeline"
	"github.com/desktopgame/backup/internal/snapshot"
)

func TestEndToEnd(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	srcDir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "node_modules", "skip.js"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}

	storeDir := filepath.Join(root, "store")
	cfgPath := filepath.Join(root, ".backup")
	cfgBody := fmt.Sprintf(`
machine = "testhost"
recipient = %q

[storage]
type = "local"
path = %q

[rotation]
daily = 7
weekly = 4
monthly = 6

[[path]]
name = "data"
source = %q
`, id.Recipient().String(), storeDir, srcDir)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}

	idPath := filepath.Join(root, "id.txt")
	if err := os.WriteFile(idPath, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ignorePath := filepath.Join(root, "ignore-missing")
	if err := os.WriteFile(ignorePath, []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 9, 21, 17, 0, 0, 0, time.Local)
	name := snapshot.Name("testhost", fixed)

	newApp := func() (*App, *bytes.Buffer) {
		buf := &bytes.Buffer{}
		a := New()
		a.Stdout = buf
		a.Stderr = buf
		a.now = func() time.Time { return fixed }
		return a, buf
	}

	// run
	a, buf := newApp()
	if code := a.Main([]string{"run", "-c", cfgPath, "--ignore", ignorePath}); code != 0 {
		t.Fatalf("run exit %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(storeDir, name)); err != nil {
		t.Fatalf("snapshot not stored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storeDir, name+".part")); err == nil {
		t.Fatal("part file should have been renamed away")
	}

	// check
	a, buf = newApp()
	if code := a.Main([]string{"check", "-c", cfgPath, "--ignore", ignorePath}); code != 0 {
		t.Fatalf("check exit %d: %s", code, buf.String())
	}

	// list
	a, buf = newApp()
	if code := a.Main([]string{"list", "-c", cfgPath}); code != 0 {
		t.Fatalf("list exit %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), name) {
		t.Fatalf("list output missing snapshot: %s", buf.String())
	}

	// restore
	restoreDir := filepath.Join(root, "restore")
	a, buf = newApp()
	args := []string{"restore", name, "-c", cfgPath, "--output", restoreDir, "--identity", idPath}
	if code := a.Main(args); code != 0 {
		t.Fatalf("restore exit %d: %s", code, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(restoreDir, "data", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Errorf("restored content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "data", "node_modules", "skip.js")); err == nil {
		t.Error("ignored file should not be restored")
	}
}

func writeSnapshotFile(t *testing.T, id *age.X25519Identity, path string) {
	t.Helper()
	var buf bytes.Buffer
	err := pipeline.Encrypt(&buf, []age.Recipient{id.Recipient()}, func(w io.Writer) error {
		tw := tar.NewWriter(w)
		dir := &tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o755}
		if err := tw.WriteHeader(dir); err != nil {
			return err
		}
		file := &tar.Header{Name: "data/x.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len("hello"))}
		if err := tw.WriteHeader(file); err != nil {
			return err
		}
		if _, err := tw.Write([]byte("hello")); err != nil {
			return err
		}
		return tw.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreLocalFileWithoutConfig verifies that restoring a local snapshot
// file does not require a configuration file.
func TestRestoreLocalFileWithoutConfig(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	snapPath := filepath.Join(root, "vps-20260921-203000.tar.zst.age")
	writeSnapshotFile(t, id, snapPath)

	idPath := filepath.Join(root, "id.txt")
	if err := os.WriteFile(idPath, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "restore")
	buf := &bytes.Buffer{}
	a := New()
	a.Stdout = buf
	a.Stderr = buf

	// Point -c at a nonexistent config to prove it is not read for local files.
	args := []string{
		"restore", snapPath,
		"--identity", idPath,
		"--output", out,
		"-c", filepath.Join(root, "does-not-exist"),
	}
	if code := a.Main(args); code != 0 {
		t.Fatalf("restore exit %d: %s", code, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(out, "data", "x.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("restored content = %q", got)
	}
}

func TestPruneDryRunAndDelete(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".backup")
	cfgBody := fmt.Sprintf(`
machine = "testhost"
recipient = %q

[storage]
type = "local"
path = %q

[[path]]
name = "data"
source = %q
`, id.Recipient().String(), storeDir, root)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	fresh := snapshot.Name("testhost", time.Date(2026, 9, 21, 8, 0, 0, 0, time.Local))
	old := snapshot.Name("testhost", time.Date(2020, 1, 1, 8, 0, 0, 0, time.Local))
	for _, n := range []string{fresh, old} {
		if err := os.WriteFile(filepath.Join(storeDir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	foreign := "notes.txt"
	if err := os.WriteFile(filepath.Join(storeDir, foreign), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	newApp := func() (*App, *bytes.Buffer) {
		buf := &bytes.Buffer{}
		a := New()
		a.Stdout = buf
		a.Stderr = buf
		a.now = func() time.Time { return fixed }
		return a, buf
	}

	a, buf := newApp()
	if code := a.Main([]string{"prune", "-c", cfgPath, "--dry-run"}); code != 0 {
		t.Fatalf("prune dry-run exit %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), old) {
		t.Fatalf("dry-run should list %s: %s", old, buf.String())
	}
	if _, err := os.Stat(filepath.Join(storeDir, old)); err != nil {
		t.Fatal("dry-run must not delete anything")
	}

	a, buf = newApp()
	if code := a.Main([]string{"prune", "-c", cfgPath}); code != 0 {
		t.Fatalf("prune exit %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(storeDir, old)); err == nil {
		t.Fatal("old snapshot should have been pruned")
	}
	if _, err := os.Stat(filepath.Join(storeDir, fresh)); err != nil {
		t.Fatal("recent snapshot must be kept")
	}
	if _, err := os.Stat(filepath.Join(storeDir, foreign)); err != nil {
		t.Fatal("non-snapshot files must never be deleted")
	}
}
