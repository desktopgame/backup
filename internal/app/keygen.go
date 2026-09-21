package app

import (
	"flag"
	"fmt"
	"os"

	"filippo.io/age"
)

func (a *App) cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var output string
	fs.StringVar(&output, "o", "", "write the identity to this file (default stdout)")
	fs.StringVar(&output, "output", "", "write the identity to this file (default stdout)")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "usage: backup keygen [-o identity.txt]")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("keygen takes no positional arguments")
	}

	id, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generating identity: %w", err)
	}
	content := fmt.Sprintf("# public key: %s\n%s\n", id.Recipient().String(), id.String())

	if output == "" {
		fmt.Fprint(a.Stdout, content)
		return nil
	}

	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create identity file: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Wrote identity to %s\nPublic key: %s\n", output, id.Recipient())
	return nil
}
