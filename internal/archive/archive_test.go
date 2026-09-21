package archive

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/desktopgame/backup/internal/ignore"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnumerateAndWriteTar(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "hello")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "world")
	writeFile(t, filepath.Join(root, "node_modules", "skip.js"), "nope")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	ign := ignore.New([]string{"node_modules/"})
	entries, stats, err := Enumerate([]Source{{Name: "vps", Root: root}}, ign)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Errorf("Files = %d, want 2", stats.Files)
	}

	var buf bytes.Buffer
	if err := WriteTar(&buf, entries); err != nil {
		t.Fatal(err)
	}

	names := map[string]string{}
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		names[hdr.Name] = string(data)
	}

	for _, want := range []string{"vps/", "vps/a.txt", "vps/sub/", "vps/sub/b.txt", "vps/empty/"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing archive entry %q; have %v", want, keys(names))
		}
	}
	if _, ok := names["vps/node_modules/skip.js"]; ok {
		t.Error("ignored node_modules content should not be archived")
	}
	if names["vps/a.txt"] != "hello" {
		t.Errorf("a.txt content = %q", names["vps/a.txt"])
	}
}

func TestEnumerateSingleFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "config")
	writeFile(t, file, "Host *\n")

	entries, stats, err := Enumerate([]Source{{Name: "ssh-config", Root: file}}, ignore.New(nil))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 1 {
		t.Fatalf("Files = %d, want 1", stats.Files)
	}

	var buf bytes.Buffer
	if err := WriteTar(&buf, entries); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&buf)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "ssh-config" {
		t.Errorf("Name = %q, want ssh-config", hdr.Name)
	}
	data, _ := io.ReadAll(tr)
	if string(data) != "Host *\n" {
		t.Errorf("content = %q", data)
	}
}

func TestEnumerateMissingPath(t *testing.T) {
	_, _, err := Enumerate([]Source{{Name: "gone", Root: filepath.Join(t.TempDir(), "missing")}}, ignore.New(nil))
	if err == nil {
		t.Fatal("expected an error for a missing source path")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
