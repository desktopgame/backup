package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, ".backup")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadExpandsEnvAndResolvesSources(t *testing.T) {
	t.Setenv("BACKUP_TEST_PASSWORD", "s3cret")
	dir := t.TempDir()

	p := writeConfig(t, dir, `
recipient = "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"

[storage]
type = "ftp"
host = "example.test"
password = "${BACKUP_TEST_PASSWORD}"

[[path]]
name = "data"
source = "relative/src"
`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Password != "s3cret" {
		t.Errorf("password = %q, want expanded value", cfg.Storage.Password)
	}
	want := filepath.Join(dir, "relative", "src")
	if cfg.Paths[0].Source != want {
		t.Errorf("source = %q, want %q", cfg.Paths[0].Source, want)
	}
	if len(cfg.AllRecipients()) != 1 {
		t.Errorf("recipients = %v", cfg.AllRecipients())
	}
}

func TestDuplicateNamesRejected(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `
[storage]
type = "local"
path = "/tmp/store"

[[path]]
name = "dup"
source = "/tmp/a"

[[path]]
name = "dup"
source = "/tmp/b"
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestNormalizedRotationDefaults(t *testing.T) {
	c := &Config{}
	r := c.NormalizedRotation()
	if r.Daily != DefaultDaily || r.Weekly != DefaultWeekly || r.Monthly != DefaultMonthly {
		t.Errorf("defaults = %+v", r)
	}
	c.Rotation = Rotation{Daily: 1}
	r = c.NormalizedRotation()
	if r.Daily != 1 || r.Weekly != 0 || r.Monthly != 0 {
		t.Errorf("explicit policy = %+v", r)
	}
}

func TestSanitizeMachine(t *testing.T) {
	cases := map[string]string{
		"desktop":        "desktop",
		"my host":        "my-host",
		"C:\\HOST\\name": "C-HOST-name",
		"":               "backup",
	}
	for in, want := range cases {
		if got := SanitizeMachine(in); got != want {
			t.Errorf("SanitizeMachine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInvalidStorageType(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `
[storage]
type = "carrier-pigeon"

[[path]]
name = "a"
source = "/tmp/a"
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected invalid storage type error")
	}
}
