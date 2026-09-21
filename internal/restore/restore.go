// Package restore decrypts, decompresses and extracts a snapshot.
package restore

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"

	"backup/internal/storage"
)

// Restore streams the named snapshot from st into outDir.
func Restore(ctx context.Context, st storage.Storage, name, outDir string, identities []age.Identity) error {
	if len(identities) == 0 {
		return fmt.Errorf("no age identity supplied (use --identity or BACKUP_AGE_IDENTITY)")
	}

	rc, err := st.Open(ctx, name)
	if err != nil {
		return err
	}
	defer rc.Close()

	decrypted, err := age.Decrypt(rc, identities...)
	if err != nil {
		return fmt.Errorf("decrypting %s: %w", name, err)
	}
	zr, err := zstd.NewReader(decrypted)
	if err != nil {
		return fmt.Errorf("decompressing %s: %w", name, err)
	}
	defer zr.Close()

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	base, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		if err := extract(base, hdr, tr); err != nil {
			return err
		}
	}
	return nil
}

// LoadIdentities parses one or more age identity files.
func LoadIdentities(paths []string) ([]age.Identity, error) {
	var identities []age.Identity
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("cannot read identity %s: %w", p, err)
		}
		ids, err := age.ParseIdentities(f)
		closeErr := f.Close()
		if err != nil {
			return nil, fmt.Errorf("invalid identity %s: %w", p, err)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		identities = append(identities, ids...)
	}
	return identities, nil
}

func extract(base string, hdr *tar.Header, r io.Reader) error {
	target, err := secureJoin(base, hdr.Name)
	if err != nil {
		return err
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := checkNoSymlinks(base, target); err != nil {
			return err
		}
		if err := os.MkdirAll(target, fs.FileMode(hdr.Mode).Perm()|0o700); err != nil {
			return err
		}
		_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)

	case tar.TypeReg:
		if err := checkNoSymlinks(base, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeFile(target, hdr, r); err != nil {
			return err
		}

	case tar.TypeSymlink:
		if err := checkNoSymlinks(base, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(hdr.Linkname, target); err != nil {
			return fmt.Errorf("creating symlink %s: %w", target, err)
		}

	case tar.TypeLink:
		linkSrc, err := secureJoin(base, hdr.Linkname)
		if err != nil {
			return err
		}
		if err := checkNoSymlinks(base, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Link(linkSrc, target); err != nil {
			return fmt.Errorf("creating hard link %s: %w", target, err)
		}

	default:
		// Character devices, FIFOs, etc. are not restored.
	}
	return nil
}

func writeFile(target string, hdr *tar.Header, r io.Reader) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode).Perm())
	if err != nil {
		return fmt.Errorf("creating %s: %w", target, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", target, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
	return nil
}

// secureJoin joins an archive entry name onto base, rejecting absolute paths
// and traversal outside base.
func secureJoin(base, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("archive contains an empty entry name")
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || filepath.VolumeName(cleaned) != "" {
		return "", fmt.Errorf("refusing absolute path in archive: %s", name)
	}
	target := filepath.Join(base, cleaned)
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing path traversal in archive: %s", name)
	}
	return target, nil
}

// checkNoSymlinks ensures no existing path component between base and target
// is a symbolic link, preventing symlink-based escape during extraction.
func checkNoSymlinks(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	cur := base
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink: %s", cur)
		}
	}
	return nil
}
