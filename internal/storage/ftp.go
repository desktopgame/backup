package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

const ftpTimeout = 30 * time.Second

// FTP stores snapshots on an FTP server.
type FTP struct {
	conn *ftp.ServerConn
}

// FTPOptions describes how to reach an FTP backend.
type FTPOptions struct {
	Host     string
	Port     int
	Username string
	Password string
	Path     string
}

// NewFTP connects and logs in to the server.
func NewFTP(ctx context.Context, opts FTPOptions) (*FTP, error) {
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		return nil, fmt.Errorf("ftp storage: host is required")
	}
	port := opts.Port
	if port == 0 {
		port = 21
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	conn, err := ftp.Dial(addr,
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(ftpTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("FTP connection failed: %w", err)
	}
	if err := conn.Login(opts.Username, opts.Password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("FTP login failed: %w", err)
	}
	base := strings.TrimSpace(opts.Path)
	if base != "" && base != "/" {
		if err := conn.ChangeDir(base); err != nil {
			_ = conn.Quit()
			return nil, fmt.Errorf("FTP directory unavailable: %s: %w", base, err)
		}
	}
	return &FTP{conn: conn}, nil
}

func (f *FTP) Put(ctx context.Context, name string, r io.Reader) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if err := f.conn.Stor(name, r); err != nil {
		return fmt.Errorf("upload interrupted: %w", err)
	}
	return nil
}

func (f *FTP) List(ctx context.Context) ([]Object, error) {
	entries, err := f.conn.List(".")
	if err != nil {
		return nil, err
	}
	objects := make([]Object, 0, len(entries))
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile {
			continue
		}
		if e.Name == "." || e.Name == ".." {
			continue
		}
		objects = append(objects, Object{Name: e.Name, Size: int64(e.Size), ModTime: e.Time})
	}
	return objects, nil
}

func (f *FTP) Rename(ctx context.Context, from, to string) error {
	if err := ValidName(from); err != nil {
		return err
	}
	if err := ValidName(to); err != nil {
		return err
	}
	return f.conn.Rename(from, to)
}

func (f *FTP) Delete(ctx context.Context, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	return f.conn.Delete(name)
}

func (f *FTP) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	resp, err := f.conn.Retr(name)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (f *FTP) Close() error {
	if f.conn == nil {
		return nil
	}
	return f.conn.Quit()
}
