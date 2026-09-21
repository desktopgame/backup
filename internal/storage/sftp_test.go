package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sshServer struct {
	addr     string
	root     string
	username string
	password string
}

// startSSHServer runs an in-process SSH server exposing an SFTP subsystem
// rooted at a temporary directory. Password auth is enabled when password is
// non-empty; public key auth is enabled when trusted is non-nil.
func startSSHServer(t *testing.T, username, password string, trusted ssh.PublicKey) *sshServer {
	t.Helper()
	root := t.TempDir()
	s := &sshServer{root: root, username: username, password: password}

	cfg := &ssh.ServerConfig{}
	if password != "" {
		cfg.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == username && string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		}
	}
	if trusted != nil {
		cfg.PublicKeyCallback = func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == username && string(key.Marshal()) == string(trusted.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("public key rejected")
		}
	}
	cfg.AddHostKey(newSigner(t))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s.addr = ln.Addr().String()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(conn, cfg, root)
		}
	}()
	return s
}

func serveSSHConn(conn net.Conn, cfg *ssh.ServerConfig, root string) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported channel")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			continue
		}
		go func() {
			for req := range requests {
				if req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp" {
					_ = req.Reply(true, nil)
					server, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
					if err != nil {
						_ = ch.Close()
						return
					}
					_ = server.Serve()
					_ = ch.Close()
					return
				}
				_ = req.Reply(false, nil)
			}
		}()
	}
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// newKeyFile writes a fresh ed25519 key in OpenSSH format and returns the
// signer and the file path.
func newKeyFile(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return signer, p
}

func (s *sshServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func TestSFTPBackendPasswordAuth(t *testing.T) {
	srv := startSSHServer(t, "test", "secret", nil)
	host, port := srv.hostPort(t)

	st, err := NewSFTP(context.Background(), SFTPOptions{
		Host:                      host,
		Port:                      port,
		Username:                  srv.username,
		Password:                  srv.password,
		Path:                      ".",
		InsecureSkipHostKeyVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	runSFTPBackendChecks(t, st, srv.root)
}

func TestSFTPBackendPrivateKeyAuth(t *testing.T) {
	signer, keyPath := newKeyFile(t)
	srv := startSSHServer(t, "test", "", signer.PublicKey())
	host, port := srv.hostPort(t)

	st, err := NewSFTP(context.Background(), SFTPOptions{
		Host:                      host,
		Port:                      port,
		Username:                  srv.username,
		PrivateKey:                keyPath,
		Path:                      ".",
		InsecureSkipHostKeyVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	runSFTPBackendChecks(t, st, srv.root)
}

func runSFTPBackendChecks(t *testing.T, st *SFTP, root string) {
	t.Helper()
	ctx := context.Background()

	if err := st.Put(ctx, "a.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("stored content = %q", data)
	}

	objects, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objects) != 1 || objects[0].Name != "a.txt" || objects[0].Size != 5 {
		t.Fatalf("List = %+v", objects)
	}

	if err := st.Rename(ctx, "a.txt", "b.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	rc, err := st.Open(ctx, "b.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("Open content = %q", got)
	}

	if err := st.Delete(ctx, "b.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	objects, err = st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 0 {
		t.Errorf("expected no objects after delete, got %+v", objects)
	}
}
