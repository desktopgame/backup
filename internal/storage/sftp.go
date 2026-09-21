package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const sshTimeout = 30 * time.Second

// SFTP stores snapshots on a remote host over SSH.
type SFTP struct {
	client *sftp.Client
	ssh    *ssh.Client
	base   string
}

// SFTPOptions describes how to reach an SFTP backend.
type SFTPOptions struct {
	Host                      string
	Port                      int
	Username                  string
	Password                  string
	PrivateKey                string
	Passphrase                string
	Path                      string
	KnownHosts                string
	InsecureSkipHostKeyVerify bool
}

// NewSFTP connects and authenticates to the SSH server.
func NewSFTP(ctx context.Context, opts SFTPOptions) (*SFTP, error) {
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		return nil, fmt.Errorf("sftp storage: host is required")
	}
	if strings.TrimSpace(opts.Username) == "" {
		return nil, fmt.Errorf("sftp storage: username is required")
	}
	port := opts.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	auth, err := sftpAuthMethods(opts)
	if err != nil {
		return nil, err
	}
	hostKey, err := sftpHostKeyCallback(opts)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            opts.Username,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         sshTimeout,
	}

	dialer := net.Dialer{Timeout: sshTimeout}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("SFTP connection failed: %w", err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("SFTP handshake failed: %w", err)
	}
	sshClient := ssh.NewClient(sshConn, chans, reqs)

	client, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, fmt.Errorf("SFTP session failed: %w", err)
	}

	base := strings.TrimSpace(opts.Path)
	if base == "" {
		base = "."
	}
	return &SFTP{client: client, ssh: sshClient, base: base}, nil
}

func sftpAuthMethods(opts SFTPOptions) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if keyPath := strings.TrimSpace(opts.PrivateKey); keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("sftp storage: cannot read private key %s: %w", keyPath, err)
		}
		var signer ssh.Signer
		if opts.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(opts.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(data)
		}
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return nil, fmt.Errorf("sftp storage: private key %s is encrypted; set passphrase", keyPath)
			}
			return nil, fmt.Errorf("sftp storage: invalid private key %s: %w", keyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if opts.Password != "" {
		methods = append(methods, ssh.Password(opts.Password))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("sftp storage: no authentication configured (set private_key or password)")
	}
	return methods, nil
}

func sftpHostKeyCallback(opts SFTPOptions) (ssh.HostKeyCallback, error) {
	if opts.InsecureSkipHostKeyVerify {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	khPath := strings.TrimSpace(opts.KnownHosts)
	if khPath == "" {
		if h, err := os.UserHomeDir(); err == nil {
			p := filepath.Join(h, ".ssh", "known_hosts")
			if _, err := os.Stat(p); err == nil {
				khPath = p
			}
		}
	}
	if khPath == "" {
		return nil, fmt.Errorf("sftp storage: known_hosts not found; set known_hosts or insecure_skip_host_key_verify")
	}
	cb, err := knownhosts.New(khPath)
	if err != nil {
		return nil, fmt.Errorf("sftp storage: cannot read known_hosts %s: %w", khPath, err)
	}
	return cb, nil
}

func asErr(err error, target **ssh.PassphraseMissingError) bool {
	for err != nil {
		if e, ok := err.(*ssh.PassphraseMissingError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (s *SFTP) remote(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return path.Join(s.base, name), nil
}

func (s *SFTP) Put(ctx context.Context, name string, r io.Reader) error {
	p, err := s.remote(name)
	if err != nil {
		return err
	}
	if err := s.client.MkdirAll(s.base); err != nil {
		return fmt.Errorf("sftp storage: cannot create %s: %w", s.base, err)
	}
	f, err := s.client.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = s.client.Remove(p)
		return fmt.Errorf("upload interrupted: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = s.client.Remove(p)
		return err
	}
	return nil
}

func (s *SFTP) List(ctx context.Context) ([]Object, error) {
	if _, err := s.client.Stat(s.base); err != nil {
		return nil, nil
	}
	entries, err := s.client.ReadDir(s.base)
	if err != nil {
		return nil, err
	}
	objects := make([]Object, 0, len(entries))
	for _, fi := range entries {
		if fi.IsDir() {
			continue
		}
		objects = append(objects, Object{Name: fi.Name(), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	return objects, nil
}

func (s *SFTP) Rename(ctx context.Context, from, to string) error {
	src, err := s.remote(from)
	if err != nil {
		return err
	}
	dst, err := s.remote(to)
	if err != nil {
		return err
	}
	return s.client.Rename(src, dst)
}

func (s *SFTP) Delete(ctx context.Context, name string) error {
	p, err := s.remote(name)
	if err != nil {
		return err
	}
	if err := s.client.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func (s *SFTP) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	p, err := s.remote(name)
	if err != nil {
		return nil, err
	}
	return s.client.Open(p)
}

func (s *SFTP) Close() error {
	var err error
	if s.client != nil {
		err = s.client.Close()
	}
	if s.ssh != nil {
		if cerr := s.ssh.Close(); err == nil {
			err = cerr
		}
	}
	return err
}
