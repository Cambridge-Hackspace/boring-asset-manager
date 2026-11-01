# BAM! (Boring Asset Manager)

![BAM!](img/bam.png)

> Excruciatingly simple physical asset label generation and tracker.

BAM! keeps track of the physical things a shop, lab or hackspace owns: what they are, where they are, who has them, and how much is left. It designs and prints their labels, on thermal label printers such as the Brother QL-810W, DYMO LabelWriter and Rollo, or on sheets.

It is one binary for Linux, FreeBSD and Windows, with its web interface built in. It runs on SQLite, PostgreSQL or MariaDB, and everything the interface does is in its API too.

## What it does

- **Assets and categories.** Categories form a tree, and each records the fields its assets need, such as a serial number or an owner. A subcategory inherits its parent's fields. An asset can be in several categories, and holds the fields of all of them.
- **Labels.** A visual editor, laid out like Krita, designs label templates with text, QR codes, Micro QR, Code 128, images, shapes and icons. What it shows is what prints, dot for dot. Labels print straight to the printer, with no driver, or download as SVG, PNG or PDF.
- **Printing in bulk.** A print queue collects labels to print together, across printers and pools of printers.
- **Scanning.** Scan an asset's code with a phone's camera or a barcode scanner, to open it or act on it at once.
- **Stock and lending.** Consumables say when they are running low, by count or by quantity and weight. Things can be lent, returned and chased up when overdue.
- **Links and places.** A bin contains screws, a drill is used with its bits. Assets live inside other assets, so BAM! knows where everything is.
- **Flows.** Guided pages, made for a phone or a tablet by the door, for everyday jobs such as "leave something in the shop" and "collect it", so anyone can do them without learning BAM!.
- **Reports, CSV and notifications.** Reports with charts, import and export as CSV, and notices to Discord or an MQTT broker when stock runs low or something is overdue.
- **Hooks.** Buttons on an asset, such as **Identify**, that tell a device outside BAM! to do something, over a live connection.
- **Icons.** Isometric icons for categories, written in [isolex](https://forge.axonibyte.com/axonibyte/isolex), drawn on screen and on labels.
- **Assistants.** An MCP endpoint lets AI assistants look things up and make changes, as the person who signed them in.
- **People and permissions.** Roles grant permissions, optionally only in some scopes, such as only for the tools. Scripts sign in with API tokens.
- **Backups and packages.** Back up a whole instance from the command line or the web, on any database, and restore it anywhere. Share part of one, such as a category of tools with their labels, as a package another instance can import.

## Getting started

Download the latest release for your system, and check it against its checksums:

```sh
curl -fLO https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/latest/download/bam-linux-amd64
curl -fLO https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
chmod +x bam-linux-amd64 && mv bam-linux-amd64 bam
```

Then write a configuration, set up the database, make the first administrator, and start it:

```sh
./bam init --config ./config.toml
./bam migrate --config ./config.toml
./bam admin create --config ./config.toml --email admin@example.org --password 'change-me' --name 'Ada Admin'
./bam serve --config ./config.toml
```

Open <http://localhost:8080> and sign in. The configuration starts with SQLite, in `./data`.

To run BAM! as a service, behind HTTPS, and keep it up to date, see [Installing](docs/installing.md).

## Documentation

People using BAM! will find help inside it, under **Help** on every page.

For running it:

- [Installing](docs/installing.md): as a service on Linux, FreeBSD or Windows, behind a reverse proxy, and updating.
- [Configuration](docs/configuration.md): the configuration file, where uploaded files are kept, and the `bam` commands.
- [Backups](docs/backups.md) and [packages](docs/packages.md): backing up, restoring, moving between databases, and sharing.
- [Access](docs/access.md): permissions, roles, scopes, sign-in and API tokens.

How each part works, with its API:

- [Data model](docs/data-model.md): fields, categories, assets, codes and links.
- [Labels](docs/labels.md), the [template editor](docs/template-editor.md) and [printing](docs/printing.md).
- [Icons](docs/icons.md).
- [Stock and lending](docs/stock-and-lending.md), [notifications](docs/notifications.md) and [hooks](docs/hooks.md).
- [Scanning](docs/scanning.md), [flows](docs/flows.md), [reports](docs/reports.md) and [CSV](docs/csv.md).
- [Assistants (MCP)](docs/mcp.md).

For working on it:

- [Architecture](docs/architecture.md): how BAM! is built, and its web interface.
- [Testing](docs/testing.md): the test suites, and running them on every database.
- [Roadmap](docs/roadmap.md).

## Building from source

`tools/release/build.sh` builds release binaries into `bin/`, needing only podman (or docker, with `CONTAINER=docker`). A plain `go build ./cmd/bam` works too, without the web interface or icons; see [Architecture](docs/architecture.md) for building those.

## License

See [LICENSE](LICENSE). The icon compiler, isolex, is by Axonibyte Innovations, LLC, under the Apache License 2.0 (`internal/isolex/NOTICE`).
