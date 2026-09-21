package app

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/desktopgame/backup/internal/restore"
	"github.com/desktopgame/backup/internal/storage"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func (a *App) cmdRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var cf commonFlags
	var output string
	var identities stringList
	addCommon(fs, &cf)
	fs.StringVar(&output, "output", ".", "directory to restore into")
	fs.StringVar(&output, "o", ".", "directory to restore into")
	fs.Var(&identities, "identity", "age identity file (repeatable)")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "usage: backup restore <snapshot> [--output dir] [--identity path]")
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("restore requires exactly one snapshot name")
	}
	name := fs.Arg(0)

	cfg, err := cf.loadConfig()
	if err != nil {
		return err
	}

	ids := []string(identities)
	if len(ids) == 0 {
		ids = envIdentities()
	}
	if len(ids) == 0 {
		return fmt.Errorf("no age identity supplied (use --identity or BACKUP_AGE_IDENTITY)")
	}
	parsed, err := restore.LoadIdentities(ids)
	if err != nil {
		return err
	}

	ctx := storageContext()
	st, err := storage.Open(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := restore.Restore(ctx, st, name, output, parsed); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Restored %s to %s\n", name, output)
	return nil
}

func envIdentities() []string {
	v := strings.TrimSpace(os.Getenv("BACKUP_AGE_IDENTITY"))
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, string(os.PathListSeparator)) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
