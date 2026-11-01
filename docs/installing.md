# Installing

BAM is one static binary with the web interface built in. This page covers installing it as a
service on Linux and FreeBSD, running it on Windows, putting it behind a reverse proxy, where
releases come from, and how an installation updates itself, backing up first so a failed update
can be undone.

## Getting the binary

Each release on GitHub has binaries for Linux (amd64 and arm64) and FreeBSD (amd64), and
`bam-windows-amd64.exe` for Windows (see [Releases](#releases)).

To build the same from a checkout instead, run `tools/release/build.sh`. It writes the binaries to
`bin/`, and needs only podman (or docker, with `CONTAINER=docker`). Name other targets as
arguments, such as `tools/release/build.sh freebsd/arm64`.

## How the service is set up

The service files and example configurations are in `packaging/`. The Linux and FreeBSD packaging
set BAM up the same way:

- a `bam` account that owns only the data directory
- a configuration readable by root and that account alone
- BAM listening on loopback, behind a reverse proxy that terminates HTTPS
- a restart five seconds after any crash

Database migrations run each time the service starts.

## Linux (systemd: Debian, Arch and the rest)

As root, from a checkout with the binary built:

```bash
install -m 0755 bin/bam-linux-amd64 /usr/local/bin/bam
install -D -m 0644 packaging/linux/bam.sysusers /etc/sysusers.d/bam.conf
systemd-sysusers                                   # creates the bam account
install -d -m 0750 -o root -g bam /etc/bam
install -m 0640 -o root -g bam packaging/linux/config.example.toml /etc/bam/config.toml
editor /etc/bam/config.toml                        # base_url, and the database if not SQLite
install -m 0644 packaging/linux/bam.service /etc/systemd/system/bam.service
systemctl daemon-reload
systemctl enable --now bam
runuser -u bam -- bam admin create --config /etc/bam/config.toml \
  --email admin@example.org --password 'change-me-at-first-sign-in'
```

| What | Where |
|---|---|
| Data | `/var/lib/bam`, which systemd creates for the service |
| Logs | The journal (`journalctl -u bam`) |
| Configuration | `/etc/bam/config.toml` |
| Environment overrides ([Configuration](configuration.md)) | `/etc/bam/environment`, one `NAME=value` per line |

The unit is sandboxed: BAM can reach the network and write to `/var/lib/bam`, and nothing else.

To use printers on USB device files (see [Printing](printing.md)), uncomment
`SupplementaryGroups=lp` in the unit and list the devices under `[printers]`.

To upgrade, replace `/usr/local/bin/bam` and run `systemctl restart bam`.

## FreeBSD

As root, from a checkout with the binary built:

```sh
install -m 0555 bin/bam-freebsd-amd64 /usr/local/bin/bam
pw groupadd bam
pw useradd bam -g bam -d /var/db/bam -s /usr/sbin/nologin -c "BAM! (Boring Asset Manager)"
install -d -m 0750 -o root -g bam /usr/local/etc/bam
install -m 0640 -o root -g bam packaging/freebsd/config.example.toml /usr/local/etc/bam/config.toml
ee /usr/local/etc/bam/config.toml                  # base_url, and the database if not SQLite
install -m 0555 packaging/freebsd/bam /usr/local/etc/rc.d/bam
sysrc bam_enable=YES
service bam start
su -m bam -c "bam admin create --config /usr/local/etc/bam/config.toml \
  --email admin@example.org --password 'change-me-at-first-sign-in'"
```

| What | Where |
|---|---|
| Data | `/var/db/bam`, which the rc script creates |
| Logs | BAM runs under `daemon(8)`, which logs to syslog with the tag `bam` (`/var/log/messages`) |
| Configuration | `/usr/local/etc/bam/config.toml` |

The rc script's settings are all optional, and listed at its top. Set them with `sysrc`:

| Setting | Purpose |
|---|---|
| `bam_config` | The configuration file. |
| `bam_user` | The account BAM runs as. |
| `bam_dbdir` | The data directory. |
| `bam_env` | Environment overrides ([Configuration](configuration.md)). |

To use printers on device files, give the `bam` account write access to them (a `devfs.rules`
entry) and list them under `[printers]`.

To upgrade, replace `/usr/local/bin/bam` and run `service bam restart`.

## Windows

Releases include `bam-windows-amd64.exe`, or build it with
`tools/release/build.sh windows/amd64`. There is no service packaging or updater for Windows.

From a folder of its own:

1. Run `bam init` to write a `config.toml` there.
2. Set `base_url` and the paths in it.
3. Run `bam migrate`.
4. Run `bam admin create --email … --password …`.
5. Run `bam serve`, under whatever keeps it running: Task Scheduler at startup, or a service
   wrapper such as NSSM.

Printers can use the `windows` connection, through the spooler (see [Printing](printing.md)).

## The reverse proxy

The example configurations listen on `127.0.0.1:8080` and expect an HTTPS address in `base_url`.
An HTTPS `base_url` also marks session cookies `Secure`. Any reverse proxy will do.

With Caddy, which obtains a certificate itself:

```
bam.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

With nginx, pass `X-Forwarded-Proto` and allow bodies up to 8 MB, for CSV imports (5 MB) and image
uploads (2 MB):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 8m;
}
```

To serve plain HTTP on the network without a proxy instead, set `host = "0.0.0.0"` and an
`http://` `base_url`.

## Releases

Each version tag pushed to GitHub, such as `v1.2.0`, is built by `.github/workflows/release.yml`:

1. The Go suites run.
2. `tools/release/build.sh` builds the binaries, with the tag as their version (`bam version`).
3. They are published as the release's files, with `SHA256SUMS`.

The release notes are `tools/release/notes/<tag>.md` when there is one, followed by GitHub's list
of what was merged. A tag with a suffix, such as `v1.2.0-rc.1`, is published as a pre-release.

`.github/workflows/build.yml` runs the same build on every push and pull request, so a change that
breaks it shows up before a tag does.

### Which version is running

The web app's footer, on every page and the sign-in page too, names the running version and links
to its release. A build that is not a release says "development build", with the commit it was
built from.

`GET /api/v1/info` needs no sign-in. It reports:

- `version`
- `commit`, recorded by Go when building in a git checkout
- `modified`, whether the tree had changes
- `mcp`, whether the server has MCP turned on (see [MCP](mcp.md))

### Installing from a release

Instead of building, an installation can start from a release. Follow the steps above, using the
downloaded file in place of `bin/bam-<os>-<arch>`:

```sh
curl -fLO https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/latest/download/bam-linux-amd64
curl -fLO https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
# On FreeBSD, with bam-freebsd-amd64 downloaded (fetch works as well as curl):
# sha256 -c "$(awk '$2 == "bam-freebsd-amd64" { print $1 }' SHA256SUMS)" bam-freebsd-amd64
```

## Updating automatically

Automatic updates are optional. `packaging/bam-update` installs the latest release when it is
newer than the installed one, on Linux and FreeBSD.

### What an update does

1. It verifies the download against the release's `SHA256SUMS`.
2. It checks that the new binary reports the release's version.
3. It stops the service.
4. It backs up the instance (see [Backups before updating](#backups-before-updating)).
5. It swaps the binary, keeping the old one as `bam.previous`.
6. It starts the service, and waits up to 30 seconds for it to answer its health check.

If the service does not answer, the update is rolled back:

1. The service is stopped.
2. The database is restored from the backup, so the changes the new version made (such as its
   migrations) are undone.
3. The old binary is started again.

### What it leaves alone

The updater leaves an installation alone when it is:

- up to date
- newer than the latest release
- not a release at all. A build from source reports `git describe` or `dev`;
  `bam-update --force` replaces it.

Pre-releases are never installed.

### Running it

- `bam-update --check` only reports.
- `bam-update --force` replaces a build that is not a release.
- Each run logs to syslog with the tag `bam-update`.
- cron mails what it prints when something goes wrong.

### Installing the updater

On Linux (cron must be installed; on Arch, `cronie`):

```bash
install -m 0755 packaging/bam-update /usr/local/sbin/bam-update
install -m 0644 packaging/linux/bam-update.cron /etc/cron.d/bam-update
```

On FreeBSD:

```sh
install -m 0555 packaging/bam-update /usr/local/sbin/bam-update
install -d /usr/local/etc/cron.d
install -m 0644 packaging/freebsd/bam-update.cron /usr/local/etc/cron.d/bam-update
```

Both run it nightly at 04:17. Change the minute in the cron file, so that every installation does
not ask GitHub at once.

### Settings

The settings (the repository, the binary, the health check URL) are listed at the top of the
script. Put any of them in `update.conf` as `NAME=value` lines:

- `/etc/bam/update.conf` on Linux
- `/usr/local/etc/bam/update.conf` on FreeBSD

If BAM listens on another port, set `BAM_UPDATE_HEALTH_URL` there.

| Setting | Effect |
|---|---|
| `BAM_UPDATE_HEALTH_URL` | The health check URL. |
| `BAM_UPDATE_KEEP_BACKUPS` | How many backups to keep (5 by default). |
| `BAM_UPDATE_BACKUP=off` | Update without a backup. |
| `BAM_UPDATE_BACKUP_COMMAND` | A command to back up with, in place of `bam backup`. |
| `BAM_UPDATE_RESTORE_COMMAND` | A command to restore with, in place of `bam restore`. |

## Backups before updating

The installed bam backs the instance up with `bam backup` into `/var/backups/bam`, readable by root
only. The backup is an archive (see [Backups](backups.md)). It holds:

- the instance's rows, authentication included
- its uploaded files

It works whatever the database (SQLite, PostgreSQL or MariaDB) and wherever the files are kept. The
newest 5 backups are kept (`BAM_UPDATE_KEEP_BACKUPS`).

### Restoring after a failed update

If the update fails, the previous bam puts the instance back from the backup with
`bam restore --wipe`. This empties the database first, so what the new version changed goes: its
migrations, and anything it wrote. It gives what it restored back to the service's user.

To do the same by hand, with BAM stopped:

```sh
bam restore --config /etc/bam/config.toml --from /var/backups/bam/bam-v1.2.3-20261029T041700Z.bam.tar.gz --wipe
```

### Installations from before archives

An installed bam from before archives (v0.14.0 and earlier) copies a SQLite database instead. For
PostgreSQL and MariaDB, the updater refuses to update until you give it commands for both
directions in `update.conf`.

### Your own backup commands

Commands set in `update.conf` are always used in place of `bam backup`. Each is run by `sh`, with
the backup's path in `$BAM_BACKUP_FILE`. For example:

```sh
BAM_UPDATE_BACKUP_COMMAND='pg_dump -Fc -f "$BAM_BACKUP_FILE" bam'
BAM_UPDATE_RESTORE_COMMAND='pg_restore --clean --if-exists -d bam "$BAM_BACKUP_FILE"'
```

### Updating without a backup

`BAM_UPDATE_BACKUP=off` updates without a backup. This leaves a failed update's changes to the
database in place.

If the backup fails, nothing is changed and the old version is started again.

### SQLite copies outside the updater

Outside the updater, `bam backup --config <file> --to <file>.db` copies a SQLite database at any
time, even while BAM runs. `bam restore --config <file> --from <file>.db` puts one back while BAM is
stopped.

Such a copy holds no uploaded files. Label images and asset photos live under
`storage.local_path`, or in a bucket (see [Configuration](configuration.md)). An archive (see
[Backups](backups.md)) holds both, from any database.

## Testing the packaging

### The systemd service

`tools/service/check-systemd.sh` follows the Linux steps above in a Debian container booted with
systemd. It checks that the service:

- starts at boot
- serves
- signs the administrator in
- comes back after it is killed, and after a reboot
- stops cleanly

The FreeBSD script has no such check, as FreeBSD cannot run in a container. ShellCheck passes it.

### The updater

`tools/service/check-update.sh` checks the updater in a Debian container booted with systemd,
against fake releases served from files. The updater must:

- update to a newer release
- leave an up-to-date installation running
- ignore an older release
- refuse a download that does not match its checksum
- roll back a release that does not come up, together with the changes that release made to the
  database and the uploaded files, on SQLite and on PostgreSQL, leaving what it restored owned by
  the service's user
- refuse to update a database it cannot back up
- run backup and restore commands when given them
- keep only the newest backups
- leave a build that is not a release alone until forced

The check's first release is built from v0.14.0, so the first update backs up as an installation
from before archives does.
