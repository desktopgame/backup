// Package archive enumerates backup sources and writes them as a tar stream.
package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backup/internal/ignore"
)

// Type describes the kind of archived entry.
type Type int

const (
	TypeReg Type = iota
	TypeDir
	TypeSymlink
)

// Entry is a single item to be written into the archive.
type Entry struct {
	Name       string // slash-separated path inside the archive
	Path       string // source filesystem path
	LinkTarget string
	Size       int64
	Mode       fs.FileMode
	ModTime    time.Time
	Type       Type
}

// Source is a configured backup root.
type Source struct {
	Name string
	Root string
}

// Stats summarizes an enumeration.
type Stats struct {
	Files int
	Bytes int64
}

// Enumerate walks every source and returns the entries to archive, honoring
// the ignore matcher.
func Enumerate(sources []Source, ign *ignore.Matcher) ([]Entry, Stats, error) {
	var entries []Entry
	var stats Stats

	for _, s := range sources {
		info, err := os.Lstat(s.Root)
		if err != nil {
			return nil, stats, fmt.Errorf("configured path does not exist: %s: %w", s.Root, err)
		}

		if !info.IsDir() {
			if ign.Match(filepath.Base(filepath.ToSlash(s.Root)), false) {
				continue
			}
			e, ok, err := entryFor(s.Name, s.Root, info)
			if err != nil {
				return nil, stats, err
			}
			if ok {
				entries = append(entries, e)
				if e.Type == TypeReg {
					stats.Files++
					stats.Bytes += e.Size
				}
			}
			continue
		}

		err = filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, relErr := filepath.Rel(s.Root, p)
			if relErr != nil {
				return relErr
			}
			relSlash := filepath.ToSlash(rel)
			if rel == "." {
				relSlash = ""
			}
			if relSlash != "" && ign.Match(relSlash, d.IsDir()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			fi, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			name := s.Name
			if relSlash != "" {
				name = s.Name + "/" + relSlash
			}
			e, ok, entryErr := entryFor(name, p, fi)
			if entryErr != nil {
				return entryErr
			}
			if !ok {
				return nil
			}
			entries = append(entries, e)
			if e.Type == TypeReg {
				stats.Files++
				stats.Bytes += e.Size
			}
			return nil
		})
		if err != nil {
			return nil, stats, fmt.Errorf("scanning %s: %w", s.Root, err)
		}
	}
	return entries, stats, nil
}

func entryFor(name, path string, fi fs.FileInfo) (Entry, bool, error) {
	e := Entry{Name: name, Path: path, Mode: fi.Mode(), ModTime: fi.ModTime()}
	switch {
	case fi.IsDir():
		e.Type = TypeDir
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return Entry{}, false, err
		}
		e.Type = TypeSymlink
		e.LinkTarget = target
	case fi.Mode().IsRegular():
		e.Type = TypeReg
		e.Size = fi.Size()
	default:
		// Sockets, devices and other special files are skipped.
		return Entry{}, false, nil
	}
	return e, true, nil
}

// WriteTar streams the entries as a tar archive to w.
func WriteTar(w io.Writer, entries []Entry) error {
	tw := tar.NewWriter(w)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.Name,
			Mode:    int64(e.Mode.Perm()),
			ModTime: e.ModTime,
			Format:  tar.FormatPAX,
		}
		switch e.Type {
		case TypeDir:
			hdr.Typeflag = tar.TypeDir
			hdr.Name = strings.TrimSuffix(e.Name, "/") + "/"
		case TypeSymlink:
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = e.LinkTarget
		default:
			hdr.Typeflag = tar.TypeReg
			hdr.Size = e.Size
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("tar header %s: %w", e.Name, err)
		}
		if e.Type != TypeReg {
			continue
		}

		f, err := os.Open(e.Path)
		if err != nil {
			return fmt.Errorf("open %s: %w", e.Path, err)
		}
		n, err := io.Copy(tw, f)
		closeErr := f.Close()
		if err != nil {
			return fmt.Errorf("archive %s: %w", e.Path, err)
		}
		if closeErr != nil {
			return fmt.Errorf("archive %s: %w", e.Path, closeErr)
		}
		if n != e.Size {
			return fmt.Errorf("archive %s: size changed during backup (expected %d, read %d)", e.Path, e.Size, n)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("finalizing tar stream: %w", err)
	}
	return nil
}
