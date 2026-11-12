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
4. Once it answers, it records `instance.update_failed` with `bam update failed`, naming the
   version tried, why it failed and whether the database was restored. Notification channels
   subscribed to it send it (see [Notifications](notifications.md)). It is recorded only now,
   since the restore takes with it anything recorded before. An old binary from before this
   command cannot record it, and the updater says so.

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

## Knowing when BAM! changes

Whoever looks after an instance can hear of each change of version, by whatever route it came:
`bam-update`, a package manager, a container image or a binary swapped by hand. Notification
channels subscribe to these events (see [Notifications](notifications.md)).

| Event | When |
|---|---|
| `instance.upgraded` | BAM! starts on a release other than the one it last ran |
| `instance.update_failed` | `bam-update` rolled a release back, as above |
| `instance.update_available` | The update check, when turned on, finds a newer release |

### Upgrades

As `bam serve` starts, after migrating, it compares its version with the one recorded in the
database:

- **Another release:** it records the new version and `instance.upgraded` in one transaction. The
  event names the versions it went from and to, the database's schema versions before and after
  (the migrations applied), and the release notes.
- **Several servers at once:** the version is claimed with a conditional update, so servers on one
  database starting at once record the upgrade once.
- **An older release:** the event says it is a downgrade.
- **The same release:** nothing is recorded.
- **The first start:** a new instance records its version, with no event. So does an instance's
  first start on the first release that records versions, as it cannot know which ran before.
- **A build that is not a release** (`dev`, or a `git describe` version such as
  `v1.2.0-4-gabc1234`) records nothing, so development does not fill channels.

### Knowing when a release is out

The update check asks, once a day, whether a newer release is out, and records
`instance.update_available` for it. It is for instances that do not update automatically, or to know
before they do. It is off unless turned on, since it reaches out to GitHub on its own:

```toml
[updates]
check = true
```

or `BAM_UPDATES_CHECK=true` (see [Configuration](configuration.md)).

- **When:** from the worker in `bam serve`, at most once a day. Servers sharing a database take turns:
  each check is claimed in the database first.
- **What counts:** only a full release newer than the running version, as `bam-update` decides.
  Drafts, pre-releases, and versions older than or equal to the running one are passed over. A build
  that is not a release never checks.
- **Once per release:** the newest release announced is recorded, so each is announced once. A
  release passed over by a newer one before a check found it is not announced on its own.
- **The event:** the running and available versions, and links to the release notes and to these
  docs, as they are for that release.
- **Errors:** a failed check, such as no network or GitHub's rate limit, is logged and tried again
  the next day. It never becomes an event.
- **Where:** GitHub's releases API for this repository, unless `[updates] url` (or
  `BAM_UPDATES_URL`) names another, for a fork or a mirror. It may answer with one release, as
  `…/releases/latest` does, or a list, as `…/releases` does. For an address that is not GitHub's API,
  the release notes link is the release's `html_url`, and there is no link to the docs.

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

## On a phone

BAM! installs on a phone, a tablet or a computer from the instance itself, as an app of its own:
an icon on the home screen, a window without the browser's bar, and pages that open at once. There
is no separate app to build, sign or publish (#212); the binary serves everything installing needs.

- **On Android,** Chrome offers **Install app** in its menu, and the home page shows an
  **Install** button when the browser allows it.
- **On an iPhone or iPad,** no browser offers to install; **Share › Add to Home Screen** in Safari
  does it. The home page says so when opened there.

Browsers install only from HTTPS (or `localhost`), so the instance must be served over HTTPS, as
[the reverse proxy](#the-reverse-proxy) does.

### What the server serves for it

- **`/manifest.webmanifest`,** the web app manifest: the app's name, its icons, its colors,
  `display: standalone`, the home page as where it starts, and its shortcuts. It is drawn from the
  settings below on every request.
- **`/app-icons/`,** the icons the manifest lists: 192 and 512 px, each whole and maskable (the
  logo kept to the middle, for launchers that cut icons to a circle or squircle), and the 180 px
  `apple-touch-icon` iOS reads instead. Each is the logo centered on the background color. They
  are drawn when first asked for and kept in memory, and their addresses change with the logo or
  color, so phones fetch them again.
- **`/sw.js`,** the service worker, from the root so that its scope is the whole app, sent with
  `Cache-Control: no-cache` and `Service-Worker-Allowed: /`.

Anyone may fetch the manifest and icons, signed in or not: a phone does before anyone signs in.
The Content Security Policy is unchanged; the worker, the manifest and the icons are all the
app's own (`default-src 'self'`).

### Settings

Under **Admin › Installed app** (`settings.manage`), or `PUT /api/v1/settings/app`:

| Setting | Default | |
| :--- | :--- | :--- |
| `name` | the instance's name, as on PDFs | Empty follows the instance's name. |
| `short_name` | the name | Under the icon, where about 12 characters fit. |
| `theme_color` | `#faf7f0`, BAM!'s paper | The bar above the app in light mode. |
| `theme_color_dark` | `#0b0b0a`, BAM!'s ink | The bar above the app in dark mode. |
| `background_color` | `#faf7f0` | Behind the icon's logo, and on the screen shown as the app opens. |
| `icon_image_id` | none: BAM!'s logo | An image in the [image library](labels.md) to draw the icon from. |

`GET /api/v1/settings/app` reads them, with `instance_name` beside them, and needs no sign-in, as
the manifest does not. `POST /api/v1/settings/app/icon`, a multipart form with the image in
`file`, adds a PNG or JPEG logo to the image library and makes it the icon. The image stays in the
library while it is the icon: deleting it is refused until another is chosen. MCP has
`get_app_settings` and `set_app_settings`.

The manifest has one theme color, the light one. The page names both, each for its color scheme,
and once the person chooses Light or Dark in the footer, that one.

Phones that installed the app read a changed name or icon when they next check the manifest,
which Chrome does about once a day; iOS keeps what it was installed with.

### Shortcuts

Pressing and holding the icon offers **Scan** (`/scan`), **Shopping list** (`/shopping`, see
[Stock](stock-and-lending.md#the-shopping-list)) and **Loans** (`/loans`). Scan and the shopping
list work offline too.

### Offline

The service worker keeps the app's shell: `index.html` and the files the build made, but the
WebAssembly: the editor's, which it keeps once the editor first opens, and the code reader's,
which it keeps once a browser without its own first scans ([Scanning](scanning.md#reading-codes-with-the-camera)). Every page of the app opens from
the shell at once, with or without a signal. Paths the server answers itself, the API among them,
are left to the network.

So an installed BAM! opens offline. With [a copy](#the-offline-copy) kept, it opens from that;
without one, it says it is offline, with **Try again**, rather than showing the browser's error,
and tries again by itself when the connection is back. A page already open says so too when the
connection drops.

### The offline copy

The installed app keeps a copy of what its person can see (#214), so that in a basement storeroom
or on a supply run they can still:

- **scan** a label, or type its code, and see the asset's scan page from the copy: its name, codes,
  categories, where it is, how many there are, its condition, its loan and its review state. The
  page says it is from the copy, made when, and that it may be out of date;
- **find** things in it, on the **Offline copy** page (`/offline`), by every word of a search over
  their names, codes, categories and places;
- **shop**, from [the shopping list](stock-and-lending.md#the-shopping-list), checking things off
  as bought, and use stock, from the scan page. These are kept on the device and sent when the app
  is next open and online ([below](#changes-made-offline)).

Opened offline, the app shows these pages, and Help, with a banner saying it runs from the copy
and since when. Other pages say they need a connection, and point to these.

#### What it holds

Only what the person can read, as the API would show them, when it was made:

- each asset: its name, the codes on its labels, its categories, where it is (only the places they
  can read), its stock, its condition if not ok, its review state, whether they may use and
  restock it, and its loan. Who has it on loan is named only with `assets.track`, or to the
  borrower, as everywhere else. Person fields ([Data model](data-model.md#person-fields)) are not
  kept at all;
- the categories those assets are in, by their paths;
- the shopping list, as `GET /api/v1/shopping` gives it to them;
- with **Keep thumbnails of photos too** chosen, each asset's first photo's thumbnail, in a cache of
  its own (`bam-offline-thumbs`).

It is made by `GET /api/v1/offline` (`assets.read`, signed in), and kept in IndexedDB with whom it
is for. Each refresh replaces it whole, so what someone can no longer see leaves the device at the
next one. The app asks the browser to keep it (`navigator.storage.persist()`), which iOS may still
not do.

**Size:** 3,000 assets, each with a code, a category, a place three levels deep, half of them with
stock and all with a photo, make about 880 KB of JSON (about 290 bytes each), and about 50 KB
gzipped over the wire (`TestOfflineCopySize` in `internal/api`). Thumbnails add what the photos'
thumbnails weigh.

#### When it is kept, refreshed and cleared

- **Kept** on a device only by choice: the installed app keeps one unless its person turns
  **Keep a copy on this device** off on the **Offline copy** page; a browser tab, only if they turn
  it on, so a shared computer keeps nothing. These choices, and **Keep thumbnails of photos too**
  (off by default), are the person's, on that device.
- **Refreshed** when the app opens or is shown again and its copy is older than the instance's
  interval, while it stays open, and with **Refresh now**.
- **Cleared:**
  - on signing out, with any changes not yet sent (the app asks first when there are some);
  - when the server says nobody is signed in any more (a session that ended): the copy goes, and
    the person's unsent changes wait for them to sign in again;
  - when someone else signs in on the device: the copy, and the changes of whoever had it;
  - when a refresh is refused because the person may no longer read assets, or copies are turned
    off: the copy and their unsent changes;
  - when they turn **Keep a copy on this device** off.

#### Settings

Under **Admin › Installed app**, or `PUT /api/v1/settings/offline` (`settings.manage`; `GET` with
`assets.read`). MCP has `get_offline_settings` and `set_offline_settings`.

| Setting | Default | |
| :--- | :--- | :--- |
| `enabled` | `true` | Whether apps keep copies at all. Turned off, each copy is cleared at its next refresh. |
| `refresh_minutes` | `60` | How often an open app refreshes its copy, from 5 minutes to a week. |
| `thumbnails` | `true` | Whether people may keep thumbnails in their copy. |

#### Changes made offline

**Restocks** checked off the shopping list, and **stock used** from the scan page, are kept on the
device and sent, in the order they were made, when the app is next open and online. Background
Sync is Chrome's alone, so nothing is sent while the app is closed. Online, they are sent at once,
the same way.

- **A restock** is sent with `unless_restocked_since`, the time the copy, or the list, it was made
  from was read ([Stock](stock-and-lending.md#restocking-from-a-copy)). If someone restocked the
  thing since, it is refused, and the app shows the conflict: what was added and when, by whom to
  those who track it, and how many there are now. The person chooses **Record mine too**, for
  another purchase, or **Drop mine**. Nothing is added up for them.
- **Stock used** is relative, so uses by several people add up as they should. Using more than is
  left is refused, and shown.
- **A change refused outright,** as when the person can no longer change the asset, is shown with
  **Try again** and **Drop it**.

Until then, the scan page and the list show what is waiting beside each quantity, and a banner on
every page says when a change needs a decision.

**Lending and returning are not kept offline.** Whether something can be lent depends on whether
someone else has it now, which a copy cannot know. Two people lending the last ladder offline
cannot both be right, and putting it right means finding one of them, so these need a
connection.

### Updates

The worker is named after its release's files, so a new release is a new worker. Browsers check
for one when the app opens, and the app checks again whenever it is shown and every hour, since an
installed app may stay open for days. A new release's shell is fetched in the background, and
**Reload to update** appears at the bottom of the window; the release takes over only when it is
chosen, so nobody loses what they were doing. Releases of the server alone, with the same web
interface, need no reload.

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
- record the failed update, once the release it put back is running, on SQLite and on PostgreSQL
- leave a build that is not a release alone until forced

The check's first release is built from v0.14.0, so the first update backs up as an installation
from before archives does.
