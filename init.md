# backup

`backup` is a small, single-binary backup CLI written in Go.

The primary use case is to collect important files and directories scattered across a machine, archive them, compress them, encrypt them locally, and upload the encrypted snapshot to another storage location.

The initial target is an Android device exposing a folder over FTP, but the storage layer must remain abstract enough to support other destinations without changing the backup pipeline.

The tool should prioritize:

* simple operation
* no external command dependencies
* encrypted backups
* easy disaster recovery
* predictable configuration
* low maintenance
* Windows and Linux support

It is intentionally not a general-purpose enterprise backup system.

---

## Core design

The backup pipeline is:

```text
configured paths
    ↓
file enumeration + ignore rules
    ↓
tar stream
    ↓
zstd stream
    ↓
age encryption stream
    ↓
storage backend
```

The entire pipeline should stream whenever possible.

Do not create intermediate `.tar`, `.tar.zst`, or other large temporary files unless a backend absolutely requires it.

Use Go implementations directly rather than shelling out to external programs.

In particular:

* tar: Go standard library `archive/tar`
* filesystem traversal: `filepath.WalkDir`
* zstd: Go library such as `github.com/klauspost/compress/zstd`
* encryption: `filippo.io/age`

Do not depend on installed `tar`, `zstd`, `openssl`, `age`, `scp`, or similar commands.

The final application should be distributable as a single executable.

---

## Configuration

The default configuration lives in the user's home directory.

```text
~/.backup
~/.backupignore
```

On Windows this corresponds to:

```text
C:\Users\<user>\.backup
C:\Users\<user>\.backupignore
```

There is only one global configuration by default.

Do not search parent directories for project-local `.backup` files.

This avoids ambiguity about which configuration is active.

### `.backup`

Use TOML syntax.

Example:

```toml
recipient = "age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

[storage]
type = "ftp"
host = "192.168.1.100"
port = 2121
username = "backup"
password = "${BACKUP_FTP_PASSWORD}"
path = "/backup"

[rotation]
daily = 7
weekly = 4
monthly = 6

[[path]]
name = "vps"
source = "C:\\Users\\example\\Work\\VPS"

[[path]]
name = "pubmir"
source = "C:\\Users\\example\\Work\\Repository\\pubmir"

[[path]]
name = "ssh-config"
source = "C:\\Users\\example\\.ssh\\config"
```

Environment variable expansion should be supported for secret values.

Do not require secrets to be stored directly in `.backup`.

The age recipient is public information and may safely live in the configuration file.

The age private identity should not be required for `backup run`.

---

## Backup paths

A backup may contain multiple unrelated paths.

Each configured path has:

```toml
[[path]]
name = "vps"
source = "/home/example/Work/VPS"
```

`source` may point to either:

* a directory
* a single file

`name` becomes the top-level name inside the archive.

For example:

```text
vps/
pubmir/
ssh-config
```

This prevents absolute host paths from leaking into the archive structure and makes restoration independent of the original machine layout.

Duplicate names are invalid.

Missing paths should fail `backup check`.

During `backup run`, a path that unexpectedly disappeared should produce an error rather than silently creating an incomplete backup.

---

## Ignore rules

Global ignore rules live in:

```text
~/.backupignore
```

Use gitignore-like syntax if practical.

At minimum support:

```text
node_modules/
.venv/
venv/
__pycache__/
dist/
build/
*.pyc
```

Ignore matching should apply recursively to every configured backup path.

Do not automatically apply `.gitignore`.

A file being ignored by Git does not imply that it should be excluded from disaster-recovery backups.

For example, the following may intentionally be backed up:

```text
.env
local configuration
SQLite databases
credentials
certificates
private service state
```

Since the resulting archive is encrypted before upload, sensitive files are allowed unless explicitly excluded by `.backupignore`.

---

## Encryption

Use `age`.

The normal backup path uses an age recipient:

```text
age1...
```

Only the recipient/public key needs to exist on the machine creating the backup.

The corresponding private identity may be stored elsewhere.

This is intentional.

A compromised backup source should not automatically contain the key required to decrypt historical backups.

Encryption happens before data leaves the local machine.

Storage backends must only receive encrypted data.

Expected output format:

```text
<snapshot-name>.tar.zst.age
```

The encrypted stream should use authenticated encryption provided by age.

Do not implement custom cryptography.

---

## Compression

Use zstd.

The default compression level should favor speed rather than maximum compression.

Source code and configuration files compress well enough that extreme compression is unnecessary.

Compression should be configurable later if needed, but MVP may use a sensible fixed default.

---

## Snapshot naming

A snapshot filename should include:

* machine/profile identifier
* timestamp
* extension

Example:

```text
desktop-20260921-170000.tar.zst.age
```

Timestamps should be sortable lexicographically.

Use local time in filenames unless there is a strong implementation reason to use UTC.

Snapshot metadata should not contain sensitive source filenames.

---

## Storage abstraction

The backup pipeline must not know storage-specific details.

Define a small storage interface around the operations actually needed.

Conceptually:

```go
type Storage interface {
    Put(ctx context.Context, name string, r io.Reader) error
    List(ctx context.Context) ([]Object, error)
    Rename(ctx context.Context, from, to string) error
    Delete(ctx context.Context, name string) error
    Open(ctx context.Context, name string) (io.ReadCloser, error)
}
```

Keep this interface small.

Do not attempt to build an rclone replacement.

### Initial backends

MVP:

* `local`
* `ftp`

FTP is included because the initial Android destination is a normal Android file-manager app exposing a folder over FTP.

Future backends may include:

* SFTP
* WebDAV
* S3-compatible storage

These are not required for the initial implementation.

---

## Upload safety

Do not publish a snapshot under its final filename until upload completes.

Upload first as:

```text
desktop-20260921-170000.tar.zst.age.part
```

After successful transfer, rename it atomically where supported:

```text
desktop-20260921-170000.tar.zst.age
```

Rotation must only run after the new snapshot has been successfully finalized.

A failed backup must never delete an older valid backup.

Stale `.part` files may be detected and cleaned up separately.

---

## Rotation

Support automatic retention.

Initial policy:

```toml
[rotation]
daily = 7
weekly = 4
monthly = 6
```

Interpret this as keeping representative snapshots across progressively older periods.

Typical result:

```text
recent backups:
  one per day

older backups:
  one per week

still older backups:
  one per month
```

Do not simply retain the newest N files globally.

Rotation should be deterministic and testable.

When multiple snapshots qualify for the same bucket, keep the newest snapshot in that bucket.

Never delete:

* the newly uploaded snapshot
* `.part` files as part of normal retention logic
* files that do not match this tool's snapshot naming scheme

`backup prune` should expose the same retention logic manually.

A dry-run mode is desirable:

```bash
backup prune --dry-run
```

---

## Commands

### `backup run`

Create and upload a new backup.

```bash
backup run
```

Responsibilities:

1. load configuration
2. validate configured paths
3. connect to storage
4. enumerate files
5. apply ignore rules
6. create tar stream
7. compress with zstd
8. encrypt with age
9. upload as `.part`
10. finalize the snapshot
11. apply rotation
12. report result

Return a non-zero exit code on failure.

---

### `backup check`

Validate the complete setup without creating a real backup.

```bash
backup check
```

Check:

* `.backup` syntax
* `.backupignore` syntax
* configured paths exist
* duplicate archive names
* valid age recipient
* storage configuration
* storage connectivity
* destination directory accessibility
* ability to create/delete a small temporary object if practical

This command should make configuration problems discoverable before the first real backup.

---

### `backup list`

List available snapshots.

```bash
backup list
```

Example:

```text
2026-09-21 17:00  28.4 MiB  desktop-20260921-170000.tar.zst.age
2026-09-20 18:02  27.9 MiB  desktop-20260920-180200.tar.zst.age
```

Only recognized backup snapshots should be shown by default.

---

### `backup prune`

Apply configured retention rules without creating a new backup.

```bash
backup prune
```

Support:

```bash
backup prune --dry-run
```

The dry run should clearly show which snapshots would be removed.

---

### `backup restore`

Restore a snapshot.

Possible forms:

```bash
backup restore desktop-20260921-170000.tar.zst.age
```

or

```bash
backup restore desktop-20260921-170000.tar.zst.age --output ./restore
```

Restore pipeline:

```text
storage
    ↓
age decrypt
    ↓
zstd decompress
    ↓
tar extract
```

The restore operation should work using the same single binary.

Do not require external `age`, `zstd`, or `tar` programs.

The age private identity may be supplied via:

```text
--identity <path>
```

and/or an environment variable.

Do not store the private identity in `.backup` by default.

Extraction must defend against archive path traversal such as:

```text
../../somewhere
```

---

## Platform support

Primary targets:

```text
Windows amd64
Windows arm64 if practical
Linux amd64
Linux arm64
```

Avoid platform-specific shell behavior.

Use Go filesystem and networking APIs directly.

Paths in configuration must support native Windows and Linux syntax.

---

## Logging

Default output should be concise.

Example:

```text
Scanning 3 paths...
12,482 files, 184.3 MiB
Compressing + encrypting...
Uploading desktop-20260921-170000.tar.zst.age...
Uploaded 31.8 MiB
Rotation: removed 2 old snapshots
Backup complete.
```

Support a verbose mode:

```bash
backup run -v
```

Do not print credentials, encryption identities, or secret environment values.

---

## Progress

For long-running backups, show useful progress when running interactively.

Useful information includes:

* files discovered
* bytes read
* bytes uploaded
* transfer rate
* elapsed time

Do not require a progress UI for redirected/non-interactive output.

---

## Failure behavior

Errors should be explicit.

Examples:

```text
backup: configured path does not exist: C:\...\foo
backup: FTP connection failed: connection refused
backup: upload interrupted after 18.2 MiB
backup: invalid age recipient
```

Partial uploads must retain the `.part` suffix.

Rotation must not run after a failed upload.

---

## Security model

Assume the storage destination may be lost, stolen, copied, or otherwise untrusted.

The storage backend receives only age-encrypted backup data.

The backup destination does not need the decryption key.

FTP may therefore be acceptable on a trusted local/VPN network for the initial Android use case because the payload itself is already encrypted.

However:

* FTP credentials are not protected by FTP itself
* FTP must not be exposed directly to the public internet
* SFTP may be added later for stronger transport security

The backup tool's security should not depend on the remote storage remaining confidential.

---

## Non-goals

MVP does not need:

* deduplicated backups
* incremental block-level backups
* Git-aware backup behavior
* database-specific dump integrations
* daemon mode
* built-in scheduler
* Web UI
* cloud-provider-specific integrations
* automatic `.gitignore` processing
* Android-specific code
* a custom server application
* custom encryption algorithms

Scheduling can initially be handled externally by:

```text
cron
systemd timers
Windows Task Scheduler
```

---

## Initial Android usage

The initial destination is expected to be an Android phone with a normal file-manager application's FTP sharing feature enabled.

Example:

```text
PC / VPS
    ↓
backup run
    ↓
tar → zstd → age
    ↓
FTP
    ↓
Android / Backup/
```

Android does not need:

* Termux
* Git
* age
* zstd
* a custom backup application
* the age private key

It only stores opaque encrypted snapshot files.

This Android-specific usage should be possible entirely through the generic FTP backend.

---

## MVP completion criteria

The first useful version is complete when all of the following work:

1. `backup check`
2. multiple configured source paths
3. `.backupignore`
4. tar creation without external commands
5. zstd compression without external commands
6. age encryption without external commands
7. local storage backend
8. FTP storage backend
9. safe `.part` upload/finalization
10. daily/weekly/monthly rotation
11. `backup list`
12. `backup prune --dry-run`
13. `backup restore`
14. Windows and Linux builds
15. tests covering archive paths, ignore matching, rotation and restore safety

Keep the implementation small.

The main value of `backup` is that after initial configuration, normal operation should be:

```bash
backup run
```

and disaster recovery should be possible with the same executable.
