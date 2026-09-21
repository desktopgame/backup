package app

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"backup/internal/storage"
)

func (a *App) cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var cf commonFlags
	addCommon(fs, &cf)
	fs.Usage = func() { fmt.Fprintln(a.Stderr, "usage: backup check [-c config]") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("check takes no positional arguments")
	}

	cfg, err := cf.loadConfig()
	if err != nil {
		return err
	}
	recipients, err := parseRecipients(cfg.AllRecipients())
	if err != nil {
		return err
	}
	ign, err := cf.loadIgnore()
	if err != nil {
		return err
	}

	for _, p := range cfg.Paths {
		if _, err := os.Lstat(p.Source); err != nil {
			return fmt.Errorf("configured path does not exist: %s", p.Source)
		}
	}

	ctx := storageContext()
	st, err := storage.Open(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	defer st.Close()

	objects, err := st.List(ctx)
	if err != nil {
		return fmt.Errorf("storage listing failed: %w", err)
	}

	tmp := fmt.Sprintf(".backup-check-%d.tmp", a.now().UnixNano())
	if err := st.Put(ctx, tmp, strings.NewReader("backup-check")); err != nil {
		return fmt.Errorf("storage write test failed: %w", err)
	}
	if err := st.Delete(ctx, tmp); err != nil {
		return fmt.Errorf("storage delete test failed: %w", err)
	}

	fmt.Fprintf(a.Stdout, "Configuration OK: %d paths, %d recipient(s), %d ignore rule(s), %d object(s) in storage.\n",
		len(cfg.Paths), len(recipients), ign.Len(), len(objects))
	return nil
}
