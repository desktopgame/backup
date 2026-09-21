package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	DefaultFileName = ".backup"
	DefaultIgnore   = ".backupignore"
	StorageLocal    = "local"
	StorageFTP      = "ftp"
	StorageSFTP     = "sftp"
	DefaultFTPPort  = 21
	DefaultSFTPPort = 22
	DefaultDaily    = 7
	DefaultWeekly   = 4
	DefaultMonthly  = 6
)

type Path struct {
	Name   string `toml:"name"`
	Source string `toml:"source"`
}

type Storage struct {
	Type     string `toml:"type"`
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
	Path     string `toml:"path"`

	// SFTP specific.
	PrivateKey                string `toml:"private_key"`
	Passphrase                string `toml:"passphrase"`
	KnownHosts                string `toml:"known_hosts"`
	InsecureSkipHostKeyVerify bool   `toml:"insecure_skip_host_key_verify"`
}

type Rotation struct {
	Daily   int `toml:"daily"`
	Weekly  int `toml:"weekly"`
	Monthly int `toml:"monthly"`
}

type Config struct {
	Machine    string   `toml:"machine"`
	Recipient  string   `toml:"recipient"`
	Recipients []string `toml:"recipients"`
	Storage    Storage  `toml:"storage"`
	Rotation   Rotation `toml:"rotation"`
	Paths      []Path   `toml:"path"`

	dir string
}

func HomeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return h, nil
}

func DefaultConfigPath() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, DefaultFileName), nil
}

func DefaultIgnorePath() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, DefaultIgnore), nil
}

// Load reads and validates a configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config %s: %w", path, err)
	}

	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cfg.dir = filepath.Dir(abs)

	cfg.expand()
	cfg.resolveSources()
	cfg.resolveStorage()

	if cfg.Machine == "" {
		if h, err := os.Hostname(); err == nil {
			cfg.Machine = h
		}
	}
	cfg.Machine = SanitizeMachine(cfg.Machine)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Dir returns the directory containing the configuration file.
func (c *Config) Dir() string { return c.dir }

// AllRecipients returns every configured age recipient.
func (c *Config) AllRecipients() []string {
	var out []string
	if strings.TrimSpace(c.Recipient) != "" {
		out = append(out, strings.TrimSpace(c.Recipient))
	}
	for _, r := range c.Recipients {
		if strings.TrimSpace(r) != "" {
			out = append(out, strings.TrimSpace(r))
		}
	}
	return out
}

// NormalizedRotation returns the effective retention policy, applying the
// defaults when no explicit policy is configured.
func (c *Config) NormalizedRotation() Rotation {
	if c.Rotation.Daily == 0 && c.Rotation.Weekly == 0 && c.Rotation.Monthly == 0 {
		return Rotation{Daily: DefaultDaily, Weekly: DefaultWeekly, Monthly: DefaultMonthly}
	}
	return c.Rotation
}

func (c *Config) expand() {
	c.Recipient = expandEnv(c.Recipient)
	for i := range c.Recipients {
		c.Recipients[i] = expandEnv(c.Recipients[i])
	}
	c.Storage.Host = expandEnv(c.Storage.Host)
	c.Storage.Username = expandEnv(c.Storage.Username)
	c.Storage.Password = expandEnv(c.Storage.Password)
	c.Storage.Path = expandEnv(c.Storage.Path)
	c.Storage.PrivateKey = expandEnv(c.Storage.PrivateKey)
	c.Storage.Passphrase = expandEnv(c.Storage.Passphrase)
	c.Storage.KnownHosts = expandEnv(c.Storage.KnownHosts)
	for i := range c.Paths {
		c.Paths[i].Name = expandEnv(c.Paths[i].Name)
		c.Paths[i].Source = expandEnv(c.Paths[i].Source)
	}
}

func (c *Config) resolveSources() {
	for i := range c.Paths {
		c.Paths[i].Source = c.resolvePath(c.Paths[i].Source)
	}
}

// resolvePath expands a leading "~" and resolves relative paths against the
// configuration file directory.
func (c *Config) resolvePath(p string) string {
	if p == "" {
		return ""
	}
	p = ExpandHome(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.dir, p)
	}
	return filepath.Clean(p)
}

func (c *Config) resolveStorage() {
	switch strings.ToLower(strings.TrimSpace(c.Storage.Type)) {
	case StorageLocal:
		c.Storage.Path = c.resolvePath(c.Storage.Path)
	case StorageSFTP:
		c.Storage.PrivateKey = c.resolvePath(c.Storage.PrivateKey)
		c.Storage.KnownHosts = c.resolvePath(c.Storage.KnownHosts)
	}
}

func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
}

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if h, err := HomeDir(); err == nil {
			if path == "~" {
				return h
			}
			return filepath.Join(h, path[2:])
		}
	}
	return path
}

// SanitizeMachine turns an arbitrary host name into a filename-safe token.
func SanitizeMachine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "backup"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
			b.WriteRune(r)
			lastDash = false
		case r == '-':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "backup"
	}
	return out
}

// Validate checks the structural integrity of the configuration.
func (c *Config) Validate() error {
	if err := c.Storage.validate(); err != nil {
		return err
	}
	if len(c.Paths) == 0 {
		return fmt.Errorf("no [[path]] entries configured")
	}
	seen := make(map[string]bool, len(c.Paths))
	for i, p := range c.Paths {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("path #%d: name is required", i+1)
		}
		if err := validateArchiveName(p.Name); err != nil {
			return fmt.Errorf("path %q: %w", p.Name, err)
		}
		if strings.TrimSpace(p.Source) == "" {
			return fmt.Errorf("path %q: source is required", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate path name: %q", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

func (s Storage) validate() error {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case StorageLocal:
		if strings.TrimSpace(s.Path) == "" {
			return fmt.Errorf("storage: path is required for local storage")
		}
	case StorageFTP:
		if strings.TrimSpace(s.Host) == "" {
			return fmt.Errorf("storage: host is required for ftp storage")
		}
		if s.Port < 0 || s.Port > 65535 {
			return fmt.Errorf("storage: invalid port %d", s.Port)
		}
	case StorageSFTP:
		if strings.TrimSpace(s.Host) == "" {
			return fmt.Errorf("storage: host is required for sftp storage")
		}
		if strings.TrimSpace(s.Username) == "" {
			return fmt.Errorf("storage: username is required for sftp storage")
		}
		if strings.TrimSpace(s.Password) == "" && strings.TrimSpace(s.PrivateKey) == "" {
			return fmt.Errorf("storage: sftp requires private_key or password")
		}
		if s.Port < 0 || s.Port > 65535 {
			return fmt.Errorf("storage: invalid port %d", s.Port)
		}
	default:
		return fmt.Errorf("storage: unsupported type %q (want %q, %q or %q)", s.Type, StorageLocal, StorageFTP, StorageSFTP)
	}
	return nil
}

func validateArchiveName(name string) error {
	if name == "." || name == ".." {
		return fmt.Errorf("invalid name %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name must not contain path separators")
	}
	if runtime.GOOS == "windows" {
		if strings.ContainsAny(name, `:*?"<>|`) {
			return fmt.Errorf("name contains characters invalid on Windows")
		}
	}
	return nil
}
