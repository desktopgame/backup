package app

import (
	"flag"
	"fmt"
	"sort"

	"github.com/desktopgame/backup/internal/storage"
)

func (a *App) cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var cf commonFlags
	addCommon(fs, &cf)
	fs.Usage = func() { fmt.Fprintln(a.Stderr, "usage: backup list [-c config]") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("list takes no positional arguments")
	}

	cfg, err := cf.loadConfig()
	if err != nil {
		return err
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

	snaps := parseSnapshots(objects)
	sort.SliceStable(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })

	if len(snaps) == 0 {
		fmt.Fprintln(a.Stdout, "No snapshots found.")
		return nil
	}
	for _, s := range snaps {
		fmt.Fprintf(a.Stdout, "%s  %9s  %s\n", s.Time.Format("2006-01-02 15:04"), humanBytes(s.Size), s.Name)
	}
	return nil
}
