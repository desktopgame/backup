package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Local stores snapshots in a local directory.
type Local struct {
	root string
}

// NewLocal creates a local backend rooted at dir.
func NewLocal(dir string) (*Local, error) {
	if dir == "" {
		return nil, fmt.Errorf("local storage: path is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Local{root: abs}, nil
}

// Root returns the absolute storage directory.
func (l *Local) Root() string { return l.root }

func (l *Local) resolve(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(name)), nil
}

func (l *Local) Put(ctx context.Context, name string, r io.Reader) error {
	p, err := l.resolve(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(p)
		return fmt.Errorf("upload interrupted: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(p)
		return err
	}
	return nil
}

func (l *Local) List(ctx context.Context) ([]Object, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	objects := make([]Object, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		objects = append(objects, Object{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return objects, nil
}

func (l *Local) Rename(ctx context.Context, from, to string) error {
	src, err := l.resolve(from)
	if err != nil {
		return err
	}
	dst, err := l.resolve(to)
	if err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (l *Local) Delete(ctx context.Context, name string) error {
	p, err := l.resolve(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func (l *Local) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	p, err := l.resolve(name)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (l *Local) Close() error { return nil }
