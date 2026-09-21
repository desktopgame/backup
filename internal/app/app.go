// Package app implements the backup command line interface.
package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/desktopgame/backup/internal/config"
	"github.com/desktopgame/backup/internal/ignore"
	"github.com/desktopgame/backup/internal/rotation"
)

// Version is the CLI version string.
const Version = "0.1.2"

// App holds the command line environment.
type App struct {
	Stdout io.Writer
	Stderr io.Writer

	now func() time.Time
}

// New creates an App bound to the process standard streams.
func New() *App {
	return &App{Stdout: os.Stdout, Stderr: os.Stderr, now: time.Now}
}

// Main dispatches a command and returns the process exit code.
func (a *App) Main(args []string) int {
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.now == nil {
		a.now = time.Now
	}

	if len(args) == 0 {
		a.usage()
		return 2
	}

	var err error
	switch args[0] {
	case "run":
		err = a.cmdRun(args[1:])
	case "check":
		err = a.cmdCheck(args[1:])
	case "list":
		err = a.cmdList(args[1:])
	case "prune":
		err = a.cmdPrune(args[1:])
	case "restore":
		err = a.cmdRestore(args[1:])
	case "keygen":
		err = a.cmdKeygen(args[1:])
	case "version", "--version", "-version":
		fmt.Fprintf(a.Stdout, "backup %s\n", Version)
		return 0
	case "help", "-h", "--help":
		a.usage()
		return 0
	default:
		fmt.Fprintf(a.Stderr, "backup: unknown command %q\n", args[0])
		a.usage()
		return 2
	}

	if err != nil {
		fmt.Fprintf(a.Stderr, "backup: %v\n", err)
		return 1
	}
	return 0
}

func (a *App) usage() {
	fmt.Fprint(a.Stderr, `usage: backup <command> [flags]

commands:
  run       create and upload a new backup
  check     validate configuration and storage connectivity
  list      list available snapshots
  prune     apply retention rules
  restore   restore a snapshot
  keygen    generate an age identity and recipient
  version   print the version

flags are command specific; run "backup <command> -h" for details.
`)
}

type commonFlags struct {
	configPath string
	ignorePath string
	verbose    bool
}

func addCommon(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.configPath, "c", "", "path to the configuration file (default ~/.backup)")
	fs.StringVar(&c.configPath, "config", "", "path to the configuration file (default ~/.backup)")
	fs.StringVar(&c.ignorePath, "ignore", "", "path to the ignore file (default ~/.backupignore)")
	fs.BoolVar(&c.verbose, "v", false, "verbose output")
	fs.BoolVar(&c.verbose, "verbose", false, "verbose output")
}

func (c commonFlags) configFile() (string, error) {
	if c.configPath != "" {
		return c.configPath, nil
	}
	return config.DefaultConfigPath()
}

func (c commonFlags) loadConfig() (*config.Config, error) {
	path, err := c.configFile()
	if err != nil {
		return nil, err
	}
	return config.Load(path)
}

func (c commonFlags) loadIgnore() (*ignore.Matcher, error) {
	path := c.ignorePath
	if path == "" {
		p, err := config.DefaultIgnorePath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	return ignore.LoadFile(path)
}

func parseRecipients(list []string) ([]age.Recipient, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("no age recipient configured")
	}
	out := make([]age.Recipient, 0, len(list))
	for _, s := range list {
		r, err := age.ParseX25519Recipient(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("invalid age recipient: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// countingReader tracks how many bytes have been read from the underlying
// reader, which corresponds to the number of bytes uploaded.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func comma(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

func storageContext() context.Context {
	return context.Background()
}

// reorderArgs moves positional arguments after flags so that flags may appear
// either before or after them (matching the documented CLI examples).
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) > 1 && arg[0] == '-' {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if name == "h" || name == "help" {
				continue
			}
			if strings.ContainsRune(name, '=') {
				continue
			}
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, arg)
	}
	return append(flags, positional...)
}

func policyOf(r config.Rotation) rotation.Policy {
	return rotation.Policy{Daily: r.Daily, Weekly: r.Weekly, Monthly: r.Monthly}
}
