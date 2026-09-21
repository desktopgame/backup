package app

import (
	"flag"
	"fmt"

	"backup/internal/storage"
)

func (a *App) cmdPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var cf commonFlags
	var dryRun bool
	addCommon(fs, &cf)
	fs.BoolVar(&dryRun, "dry-run", false, "show what would be removed without deleting")
	fs.Usage = func() { fmt.Fprintln(a.Stderr, "usage: backup prune [-c config] [--dry-run]") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("prune takes no positional arguments")
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

	_, remove, err := planRotation(ctx, st, policyOf(cfg.NormalizedRotation()), a.now())
	if err != nil {
		return err
	}

	if dryRun {
		if len(remove) == 0 {
			fmt.Fprintln(a.Stdout, "Nothing to prune.")
			return nil
		}
		fmt.Fprintf(a.Stdout, "Would remove %d snapshot(s):\n", len(remove))
		for _, s := range remove {
			fmt.Fprintf(a.Stdout, "  %s\n", s.Name)
		}
		return nil
	}

	if len(remove) == 0 {
		fmt.Fprintln(a.Stdout, "Nothing to prune.")
		return nil
	}
	for _, s := range remove {
		if err := st.Delete(ctx, s.Name); err != nil {
			return fmt.Errorf("deleting %s: %w", s.Name, err)
		}
		fmt.Fprintf(a.Stdout, "Removed %s\n", s.Name)
	}
	fmt.Fprintf(a.Stdout, "Pruned %d snapshot(s).\n", len(remove))
	return nil
}
