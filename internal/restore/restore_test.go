package restore

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"

	"backup/internal/pipeline"
	"backup/internal/storage"
)

func buildEncrypted(t *testing.T, id *age.X25519Identity, build func(*tar.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := pipeline.Encrypt(&buf, []age.Recipient{id.Recipient()}, func(w io.Writer) error {
		tw := tar.NewWriter(w)
		build(tw)
		return tw.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func putSnapshot(t *testing.T, id *age.X25519Identity, name string, build func(*tar.Writer)) (*storage.Local, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := buildEncrypted(t, id, build)
	if err := st.Put(context.Background(), name, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func writeHeader(t *testing.T, tw *tar.Writer, name string, typ byte, mode int64, size int64, link string) {
	t.Helper()
	hdr := &tar.Header{Name: name, Typeflag: typ, Mode: mode, Size: size, Linkname: link}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	name := "h-20260921-170000.tar.zst.age"
	st, _ := putSnapshot(t, id, name, func(tw *tar.Writer) {
		writeHeader(t, tw, "vps/", tar.TypeDir, 0o755, 0, "")
		writeHeader(t, tw, "vps/a.txt", tar.TypeReg, 0o644, int64(len("hello")), "")
		if _, err := tw.Write([]byte("hello")); err != nil {
			t.Fatal(err)
		}
		writeHeader(t, tw, "vps/sub/", tar.TypeDir, 0o755, 0, "")
		writeHeader(t, tw, "vps/sub/b.txt", tar.TypeReg, 0o644, int64(len("world")), "")
		if _, err := tw.Write([]byte("world")); err != nil {
			t.Fatal(err)
		}
	})

	out := filepath.Join(t.TempDir(), "restore")
	if err := Restore(context.Background(), st, name, out, []age.Identity{id}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "vps", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("a.txt = %q", got)
	}
	got, err = os.ReadFile(filepath.Join(out, "vps", "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "world" {
		t.Errorf("b.txt = %q", got)
	}
}

func TestRestoreRejectsTraversal(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	name := "h-20260921-170000.tar.zst.age"
	st, _ := putSnapshot(t, id, name, func(tw *tar.Writer) {
		writeHeader(t, tw, "../evil.txt", tar.TypeReg, 0o644, int64(len("x")), "")
		if _, err := tw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	})

	base := t.TempDir()
	out := filepath.Join(base, "restore")
	if err := Restore(context.Background(), st, name, out, []age.Identity{id}); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(base, "evil.txt")); err == nil {
		t.Fatal("traversal escaped the output directory")
	}
}

func TestRestoreRejectsAbsolutePath(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	name := "h-20260921-170000.tar.zst.age"
	st, _ := putSnapshot(t, id, name, func(tw *tar.Writer) {
		writeHeader(t, tw, "/tmp/evil.txt", tar.TypeReg, 0o644, int64(len("x")), "")
		if _, err := tw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	})

	out := filepath.Join(t.TempDir(), "restore")
	if err := Restore(context.Background(), st, name, out, []age.Identity{id}); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}

func TestRestoreRejectsSymlinkEscape(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	name := "h-20260921-170000.tar.zst.age"
	st, _ := putSnapshot(t, id, name, func(tw *tar.Writer) {
		writeHeader(t, tw, "link", tar.TypeSymlink, 0o777, 0, "..")
		writeHeader(t, tw, "link/evil.txt", tar.TypeReg, 0o644, int64(len("x")), "")
		if _, err := tw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	})

	out := filepath.Join(t.TempDir(), "restore")
	if err := Restore(context.Background(), st, name, out, []age.Identity{id}); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestSecureJoin(t *testing.T) {
	base := t.TempDir()
	if _, err := secureJoin(base, "../x"); err == nil {
		t.Error("expected ../x to be rejected")
	}
	if _, err := secureJoin(base, "/x"); err == nil {
		t.Error("expected /x to be rejected")
	}
	got, err := secureJoin(base, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "a", "b.txt"); got != want {
		t.Errorf("secureJoin = %q, want %q", got, want)
	}
}
