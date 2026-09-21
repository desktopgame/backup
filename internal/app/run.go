package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"backup/internal/archive"
	"backup/internal/pipeline"
	"backup/internal/rotation"
	"backup/internal/snapshot"
	"backup/internal/storage"
)

func (a *App) cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var cf commonFlags
	addCommon(fs, &cf)
	fs.Usage = func() { fmt.Fprintln(a.Stderr, "usage: backup run [-c config] [-v]") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("run takes no positional arguments")
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

	sources := make([]archive.Source, len(cfg.Paths))
	for i, p := range cfg.Paths {
		sources[i] = archive.Source{Name: p.Name, Root: p.Source}
	}

	fmt.Fprintf(a.Stdout, "Scanning %d paths...\n", len(sources))
	entries, stats, err := archive.Enumerate(sources, ign)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "%s files, %s\n", comma(stats.Files), humanBytes(stats.Bytes))

	ctx := storageContext()
	st, err := storage.Open(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	defer st.Close()

	now := a.now()
	name := snapshot.Name(cfg.Machine, now)
	part := name + ".part"

	fmt.Fprintf(a.Stdout, "Compressing + encrypting...\n")
	fmt.Fprintf(a.Stdout, "Uploading %s...\n", name)

	prog := newProgress(a.Stdout, a.isInteractive())
	uploaded, err := putStream(ctx, st, part, func(w io.Writer) error {
		return pipeline.Encrypt(w, recipients, func(pw io.Writer) error {
			return archive.WriteTar(pw, entries)
		})
	}, prog)
	prog.stop()
	if err != nil {
		return err
	}

	if err := st.Rename(ctx, part, name); err != nil {
		return fmt.Errorf("finalizing snapshot %s: %w", name, err)
	}
	fmt.Fprintf(a.Stdout, "Uploaded %s\n", humanBytes(uploaded))

	_, remove, err := planRotation(ctx, st, policyOf(cfg.NormalizedRotation()), now)
	if err != nil {
		fmt.Fprintf(a.Stderr, "backup: rotation failed: %v\n", err)
	} else {
		removed := 0
		for _, s := range remove {
			if err := st.Delete(ctx, s.Name); err != nil {
				fmt.Fprintf(a.Stderr, "backup: rotation: cannot delete %s: %v\n", s.Name, err)
				continue
			}
			removed++
		}
		if removed > 0 {
			fmt.Fprintf(a.Stdout, "Rotation: removed %d old snapshots\n", removed)
		}
	}

	fmt.Fprintln(a.Stdout, "Backup complete.")
	return nil
}

// putStream streams generated content into storage, propagating producer
// errors to the backend and vice versa.
func putStream(ctx context.Context, st storage.Storage, name string, produce func(io.Writer) error, prog *progress) (int64, error) {
	pr, pw := io.Pipe()
	counter := &countingReader{r: pr}
	if prog != nil {
		prog.attach(counter)
	}

	errCh := make(chan error, 1)
	go func() {
		err := produce(pw)
		_ = pw.CloseWithError(err)
		errCh <- err
	}()

	putErr := st.Put(ctx, name, counter)
	if putErr != nil {
		_ = pr.CloseWithError(putErr)
		<-errCh
		return counter.n, putErr
	}
	if err := <-errCh; err != nil {
		return counter.n, err
	}
	return counter.n, nil
}

func planRotation(ctx context.Context, st storage.Storage, policy rotation.Policy, ref time.Time) (keep, remove []snapshot.Snapshot, err error) {
	objects, err := st.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	snaps := parseSnapshots(objects)
	keep, remove = rotation.PlanSnapshots(snaps, policy, ref)
	return keep, remove, nil
}

func parseSnapshots(objects []storage.Object) []snapshot.Snapshot {
	var out []snapshot.Snapshot
	for _, o := range objects {
		s, ok := snapshot.Parse(o.Name)
		if !ok {
			continue
		}
		s.Size = o.Size
		out = append(out, s)
	}
	return out
}

func (a *App) isInteractive() bool {
	f, ok := a.Stdout.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
