# BAM! documentation

These pages describe how BAM! works, for people running it and for people working on it. People using BAM! will find help inside it, under **Help** on every page.

## Running BAM!

- [Installing](installing.md): as a service on Linux, FreeBSD or Windows, behind a reverse proxy, releases and updating.
- [Configuration](configuration.md): the configuration file, environment variables, where uploaded files are kept, and the `bam` commands.
- [Backups and archives](backups.md): backing up, restoring, replacing an instance in use, and moving between databases.
- [Packages](packages.md): sharing part of an instance with another, and icon sets.
- [Access](access.md): permissions, roles, scopes, sign-in, sessions and API tokens.

## How it works

- [Data model](data-model.md): fields, categories, assets, codes, attachments and photos, links and places, stored text and databases.
- [Vendors](vendors.md): suppliers and how to reach them, who sees them, and the vendors categories are bought from.
- [Contracts](contracts.md): warranties, service and maintenance agreements, leases and licenses with vendors, what they cover, and their reminders.
- [Purchase orders](purchase-orders.md): orders to vendors from their catalogs, started from what is running low, sent as PDFs, and received into stock.
- [Comments](comments.md): notes in Markdown on assets, categories, loans and filings, how they are rendered safely, and who sees and writes them.
- [Labels](labels.md): label templates, content bindings, codes and output formats.
- [Template editor](template-editor.md): the label editor, its tools and shortcuts.
- [Printing](printing.md): printers, label stock, printer drivers and connections, and the print queue.
- [Icons](icons.md): isometric icons written in isolex, and how BAM! compiles and draws them.
- [Stock and lending](stock-and-lending.md): running low, quantities and weights, asset history, and loans.
- [Review states](review.md): draft, confirmed and verified assets, dates to review by, and assistants' changes landing as drafts.
- [Notifications](notifications.md): channels, drivers and deliveries.
- [Hooks and live connections](hooks.md): buttons on assets that tell devices to act.
- [Relays](relays.md): the relay bus, over BoneMesh and WebSocket, linking a hosted BAM! to printers and devices on a local network.
- [Scanning](scanning.md): scan pages, scan templates and quick actions.
- [Flows](flows.md): guided pages for everyday jobs, and their definitions.
- [Reports](reports.md): saved views of assets, as tables and charts.
- [CSV and Excel import and export](csv.md).
- [MCP server](mcp.md): connecting AI assistants, their tools and sign-in.

## Working on BAM!

- [Architecture](architecture.md): how BAM! is built, its web interface, the look and accessibility, and the help system.
- [Testing](testing.md): the test suites, reaper, the web interface tests, and running against PostgreSQL, MariaDB and S3.
- [Roadmap](roadmap.md).
