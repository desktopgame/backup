// Package storage defines the backup destination abstraction and its backends.
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Object is a single item stored in a backend.
type Object struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Storage is the minimal interface the backup pipeline needs.
type Storage interface {
	Put(ctx context.Context, name string, r io.Reader) error
	List(ctx context.Context) ([]Object, error)
	Rename(ctx context.Context, from, to string) error
	Delete(ctx context.Context, name string) error
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	Close() error
}

// ValidName rejects names that could escape the backend root.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("empty object name")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid object name %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("object name must not contain path separators: %q", name)
	}
	return nil
}
