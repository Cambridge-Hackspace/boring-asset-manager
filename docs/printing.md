# Printing

BAM! prints labels straight to label printers over the network, a USB port, a print server or the
Windows spooler. You configure any number of printers, each with a driver for its family, the
media it has loaded, and a connection saying how BAM reaches it. Before anything prints, a fit
check makes sure the template suits the roll.

Labels can be printed at once, a batch at a time, or gathered over time in the spool (the
**Print queue** in the web app) and sorted onto templates and printers in bulk. Printer pools
spread one job across several printers loaded alike.

For templates, content bindings and output formats, see [Labels](labels.md).

## Printers

Each printer is stored with:

- a name;
- a driver, chosen per printer;
- a model;
- a connection (see [Delivery](#delivery));
- the media currently loaded;
- driver `options`, as a JSON object.

Three drivers ship today: `brother_ql`, `dymo_labelwriter` and `tspl`. Option values are listed
by the drivers endpoint, and unknown options are refused.

### Who can do what

| Action | Permission |
|---|---|
| Configure printers, label stock and pools | `settings.manage` |
| List printers | `labels.print` or `templates.manage` |
| Run a printer's fit check without printing | template designers (see [Printer API](#printer-api)) |
| Print, use the spool | `labels.print` |
| Test print | `labels.print` and `templates.manage` |

## The fit check

The fit check is the same for every driver (`printers.FitLabel`). A driver reports its printer's
geometry as configured, and keeps only encoding. The geometry is:

- the DPI templates must use;
- the roll's label size;
- the lengths it prints from a continuous roll;
- how much of each label its head reaches.

A template fits when all of these hold:

- its DPI is the printer's;
- one side is the roll's width (±0.5 mm);
- the other side is the label length on die-cut rolls, or within the printer's range on
  continuous ones (to half a dot).

A template whose height is the roll's width is rotated 90° counterclockwise. So is one that fits
either way when its `feed` is `along`; with `across`, it is not. A feed that does not fit is
refused, saying so.

Anything else is refused, with `422` when printing. The refusal gives every reason at once (its
resolution and its size).

### Warnings

A fit can also carry **warnings**, which never stop printing:

- Part of the template (its elements' boxes, as they lie turned) falls where the head does not
  print, such as the far edge of a 59 mm label on a first-generation LabelWriter or the outer
  1.5 mm of Brother tape.
- Content is within 1 mm of a die-cut label's edge, and may be cut off as labels shift.
- Custom stock is loaded on a DYMO 550, 5XL or Wireless, which print only on labels they identify.

Printing answers with the warnings too. So do the spool (`fit_warnings`, by template and
destination) and printing from it.

### Checking a fit from the editor

`POST /api/v1/printers/{id}/fit` takes:

- the template's size and DPI;
- optionally its elements' boxes in millimeters (`content`);
- optionally its `feed`.

It answers `fits`, with `warnings` or why not. When it fits, it also answers with:

| Field | Meaning |
|---|---|
| `feed` | Which way the label goes through the printer: `across` or `along` |
| `printable` | The part of the label the printer prints, in the template's millimeters |
| `edge_mm` | How near a die-cut label's edge content may be cut off |
| `outside` | Which of the boxes reach outside what the printer prints |
| `near_edge` | Which of the boxes reach near the edge |

The editor shades and outlines `outside` and `near_edge` on its canvas.

### Test prints

`POST /api/v1/printers/{id}/test-print` (`labels.print` and `templates.manage`) prints one label
of a template that need not be saved, taking it as `/templates/preview` does. It prints for an
asset or with placeholders, laid out for the printer if the template is adaptive. A test print is
not recorded in history.

## Changing a printer

Changing what a printer has loaded can leave labels, categories and pools without a printer that
suits them. BAM says what a change would affect before you save it, and the web app asks before
saving a change that affects anything.

`POST /api/v1/printers/{id}/changes` (`settings.manage`) takes a printer as `PUT` would save it
and, without saving, answers with `changes`:

| Field | What the change would leave without a printer |
|---|---|
| `stranded` | Labels waiting in the spool for this printer, or for a pool it is in, that would no longer fit there; by template and destination, with how many |
| `categories` | Categories whose label template no printer would suit |
| `pools` | Pools whose printers would no longer be loaded alike |

`PUT` answers with the same.

Waiting labels whose template came from their categories, and that the change leaves unfit, move
to the first other template of their categories' that fits where they go. `stranded` reports this
as `move_to`. Labels whose template was chosen by hand stay, marked as not fitting.

Templates are never changed by a printer change. A template that no longer fits is not printed on
that printer until a roll it suits is loaded again. `GET /api/v1/templates` says which printers
suit each template as loaded (`suits`), to those who may see printers.

## Custom label stock

Custom label stock is a roll known by its size alone:

- a name;
- a width;
- for die-cut labels, the label length and the gap between labels (none for a continuous roll).

It is managed with the printers through `/api/v1/label-stock` (`settings.manage`). It is listed to
those who may see printers, with the printers that have each stock loaded.

A printer's `media` names either one of its driver's built-in rolls or custom stock by ID.
Printers answer with the stock (`stock`) and a description of whatever is loaded (`loaded`), so
clients need not look either up.

Drivers take custom stock only where a size is all the printer needs:

- TSPL printers and DYMO LabelWriters take it, up to their head's width.
- Brother QL printers take only their built-in rolls. They identify the DK roll loaded and need
  Brother's figures to place a label on it.

Changing stock checks every printer with it loaded. The change is refused, naming the printer, if
one would no longer take it. Stock loaded in a printer cannot be deleted (`409`).

## Drivers

### `brother_ql`

For the QL-810W and QL-820NWB.

**Options:** `cut` (default `true`) cuts each label off as it finishes; turned off, the tape feeds
on uncut.

**Media.** The media catalog records, for each supported roll:

- its width;
- whether it is die-cut, and if so its label length;
- its printable width in head dots;
- how many pins sit beside the tape.

It covers every roll the QL-800 series takes, with the figures from Brother's *Raster Command
Reference QL-800/810W/820NWB* (version 1.01, sections 2.3.2 and 2.3.5):

- continuous tape 12, 29, 38, 50, 54 and 62 mm wide;
- die-cut labels 17 × 54, 17 × 87, 23 × 23, 29 × 42, 29 × 90, 38 × 90, 39 × 48, 52 × 29,
  54 × 29, 60 × 86, 62 × 29, 62 × 60, 62 × 75 and 62 × 100 mm;
- round labels 12, 24 and 58 mm across.

Rolls are named by their DK part number where Brother sells them under one. Two-color black/red
media (DK-22251) is out of scope.

**Fit and margins.** Before any asset is laid out, the fit check requires the template to suit the
loaded media, at 300 DPI. Continuous labels are 25–1000 mm long. The head reaches the middle of the
tape, and the middle of a die-cut label's length, by Brother's printable pins and lines.

The printer cannot reach the tape edges, so the rendered label is cropped to the printable area
around its center:

- 306 of the 343 dots across 29 mm tape (about 1.5 mm lost on each side);
- 696 across 62 mm tape;
- on DK-11201, 991 of 1063 dots along the label (about 3 mm at each end).

Keep elements clear of those margins.

**Encoding.** Each label is rendered 1-bit at 300 DPI and encoded as a raster stream, as described
in Brother's QL-800 series raster command reference. The stream is: invalidate, initialize, then
per label:

1. raster mode;
2. print information;
3. auto-cut;
4. margins and TIFF compression;
5. PackBits-compressed raster lines (90 bytes for each line of the 720-pin head, mirrored because
   the first pin sits at the right of the tape);
6. a print command.

With `cut` on, every label is cut off as it finishes.

### `dymo_labelwriter`

For DYMO LabelWriter printers, which speak one of two raster languages:

| Generation | Models |
|---|---|
| First | 400, 400 Turbo, Twin Turbo, 450, 450 Turbo, 450 Twin Turbo, 450 Duo (label side), 4XL |
| Second | 550, 550 Turbo, 5XL, LabelWriter Wireless |

The first language is implemented from DYMO's *LabelWriter 450 Series Technical Reference Manual*,
and its tests are worked out from that manual. DYMO has published no reference for the second. Its
command format was learned by reading DYMO's open-source (GPL) Linux drivers. No code or test data
is taken from those drivers, so BAM stays MIT-licensed.

**Options:**

- `density`: `light`, `medium`, `normal` or `dark`; default `normal`.
- `roll`, on twin-roll models only: `auto`, `left` or `right`; default `auto`.

**Media.** The media catalog holds the common die-cut rolls:

| Roll | Size |
|---|---|
| 30252/99010 address | 28 × 89 mm |
| 30321/99012 large address | 36 × 89 mm |
| 30256 shipping | 59 × 102 mm |
| 30323/99014 shipping | 54 × 101 mm |
| 30334/11354 | 57 × 32 mm |
| 30336/11352 | 25 × 54 mm |
| 30332/11353 | 25 × 25 mm |
| 30347 | 25 × 38 mm |
| 30330/11355 | 19 × 51 mm |
| 30345 | 19 × 64 mm |

It also holds 30270 continuous 57 mm, and 1744907 4 × 6 in shipping labels, which need the 4XL or
5XL. Label sizes in dots follow from the nominal sizes at 300 DPI. The fit check is as above, at
300 DPI. Continuous labels are 10–2700 mm long.

**Placement.** Labels sit against the roll guide at the head's first dot, and the render is
centered on the label. On first-generation models it is then cut to the most the head takes in one
line: 56 mm (672 dots) or 104 mm (1248 dots). So the far edge of a 59 mm shipping label does not
print, which the fit check warns of when the template has anything there. Second-generation models
receive the whole label.

**First-generation encoding.** The raster stream begins with one ESC more than the longest raster
line (85, or 157 on the 4XL) to resynchronize. Then come text mode, density, the roll on twin
models, and the label length (a little over the label on die-cut rolls, `0xFFFF` for continuous).

Each line is then trimmed of blank bytes at both ends and sent run-length compressed when that is
shorter. Dot tab and bytes per line are sent only when they change, and runs of blank lines are
sent as skips. Labels are separated by short form feeds, and a form feed after the last brings it
to the tear bar. Blank lines at the end of a die-cut label are left to the form feed.

**Second-generation encoding.** The raster stream begins with a job header (random job ID,
density, text quality, default media type). Then, per label, it sends the label length (0 for
die-cut, `0xFFFF` for continuous), a label index, a print data header and uncompressed lines.
After a final form feed, an end-of-job command closes the job. The 550 series only prints on
genuine DYMO labels it can identify, whatever the driver sends.

**Connections.** The LabelWriter 400 and 450 series and the 4XL have only a USB port, so they use a
`device`, `ipp`, `windows` or print-server `tcp` connection (see [Delivery](#delivery)). The 550
Turbo, 5XL and Wireless have network ports.

### `tspl`

For printers that speak TSPL, the label language of TSC printers that many inexpensive 4-inch
direct thermal printers also use. The driver follows TSC's *TSPL/TSPL2 Programming Manual*.

**Models:** Rollo X1038 and X1040, Munbyn ITPP941, and generic 203 DPI and 300 DPI TSPL printers.

**Options:**

- `density`: 0–15; unset keeps the printer's own setting.
- `rotate_180`: default `false`; set it to print labels the other way up.

**Media.** These printers take any label size up to their head width, so the media catalog lists
common sizes rather than part numbers:

- die-cut labels with a 1/8 in gap: 4 × 6, 4 × 4, 4 × 3, 4 × 2, 3 × 2, 3 × 1, 2.25 × 1.25, 2 × 1,
  1.5 × 1 and 1 × 1 in;
- continuous: 4 in and 2 in.

Any other size is added as [custom stock](#custom-label-stock). The fit check is as above, at the
model's DPI (203 for the Rollo). Continuous labels are 10–1000 mm long. A label wider than the head
is printed around its middle.

**Encoding.** The program sent is `SIZE`, `GAP`, `DIRECTION`, `REFERENCE` and optionally
`DENSITY`. Then, for each distinct label, it sends:

1. `CLS`;
2. one `BITMAP` holding the whole label (a 0 bit prints a dot), centered across the label;
3. `PRINT 1,n`, with `n` the copies.

None of the printer's own fonts or barcode commands are used, so output matches the SVG and PDF
exports.

**Connections.** The X1038 has only a USB port (see [Delivery](#delivery)); the X1040 is reached
over the network.

## Delivery

Each printer has a `connection`, `{"type": ..., "address": ...}`, saying how BAM reaches it:

| Type | Address | Use |
|---|---|---|
| `tcp` | `host` or `host:port` (default 9100) | Network printers, and USB printers shared by a raw print server such as `p910nd`. |
| `device` | A device path on the BAM server, such as `/dev/usb/lp0` (Linux) or `/dev/ulpt0` (FreeBSD) | A USB printer plugged into the BAM server. The path must match a pattern in the server's `[printers] devices` setting, which is empty by default: otherwise anyone who can manage printers could make BAM write to any file it can open. BAM never creates the file, and needs permission to write the device (on Linux, usually membership of the `lp` group). |
| `ipp` | `ipp://host[:port]/path`, or `ipps`, `http`, `https` | A queue on CUPS or another IPP server, such as a USB printer shared from another machine. The job is sent as `application/octet-stream`, so the queue must be raw (CUPS: `lpadmin -m raw`; CUPS 2.x still supports raw queues though it marks them deprecated). CUPS can also forward a raw queue to a Windows shared printer through its `smb://` backend. |
| `windows` | A Windows printer name or `\\host\share` | When BAM runs on Windows: a local or shared printer, printed through the spooler with the `RAW` datatype. Windows has no device paths for USB printers, so this covers both. |

Jobs are sent one at a time per destination, so concurrent requests never interleave their
streams. A job counts as printed once the destination has accepted the whole stream. Reading back
printer status is a later improvement.

### Timeouts and errors

- A printer that cannot be reached in 5 seconds fails the job.
- A printer that stops taking a job for 60 seconds (over TCP, a device or IPP), or that takes 60
  seconds to answer once it has the job, fails it.
- A printer takes a job only as fast as it prints it, so there is no limit on a long job that
  keeps moving.
- A connection the server may not use (a device outside the allowed patterns, or `windows` on
  another OS) is refused with `400` when the printer is saved, and with `502` at print time.
- An unreachable printer also returns `502`.

## Printing labels

### Printing directly

`POST /api/v1/printers/{id}/print` (`labels.print`) sends labels to a printer. It takes:

- a template ID;
- asset IDs;
- a copy count (1–20, at most 500 labels in all);
- optionally a `feed` and a `mirror` to use this time in place of the template's.

It checks the template against the printer's media ([the fit check](#the-fit-check)) and streams
the job to the printer over TCP port 9100. It answers once the printer has the whole job, which for
hundreds of labels can take many minutes.

So this request, label exports, spool downloads and MCP requests may run for up to 30 minutes, in
place of the server's usual 15 seconds. A job once begun is sent whole even if its caller hangs up.

### Exporting labels

`POST /api/v1/labels/export` takes a template ID, up to 500 distinct asset IDs and a format (`pdf`
or `zip`, with `"mono": true` for 1-bit PNGs). It streams back a multi-page PDF or a ZIP of PNGs
named `<asset_id>.png`.

### Validation

Every asset is bound and validated before any output is produced. A batch either renders completely
or fails with `422` and a list of `{asset_id, path, message}` for the assets that could not be
rendered. Archived assets are never labeled.

### What can print

`POST /api/v1/labels/options` (`assets.read`) says what can print. It takes up to 500 asset IDs and
answers with the templates that can label them:

- Their categories' templates come first, in order of preference (marked `preferred`), then the
  rest.
- Each template carries the `problems` of up to 50 of the assets it cannot label (an asset with no
  code of a type it prints, in a sentence).
- `template_id` is the one to start with: the first of their categories' templates that labels them
  all and prints somewhere.

For those with `labels.print` it adds the `printers`, and `fits`, by template and printer:

- whether its labels print there (`fits`);
- why not (`reason`, every reason at once, naming the template and printer);
- `warnings`.

The web app's Labels panel offers only the printers a template fits, listing the others with why
not. It puts a template's problems in place of its preview.

### History

Labels sent to a printer record `asset.label_printed` in each asset's history, with the template
and printer as they were named then, and the copies. Exports are not recorded, since they may never
be printed. Channels cannot subscribe to `asset.label_printed`.

## The spool

The spool, the **Print queue** in the web app, is a print queue that labels gather in over time.
They are then sorted onto templates and printers in bulk and printed together.

There is one spool, shared by everyone with `labels.print`, which every spool endpoint needs. Each
entry also needs its asset readable in the requester's scopes (see [Access](access.md)). Entries
for assets a requester cannot read are neither listed nor changed for them.

### Entries

Each entry is one asset's label:

- a template;
- a destination: `printer:<id>`, `pool:<id>`, `download`, or none yet;
- copies (1–20);
- who added it and when.

An asset may wait more than once with different templates. Adding one already waiting with the same
template adds nothing. Deleting a printer or a template leaves its entries without one.

At most 5,000 labels wait at once. An add that would pass that adds nothing (`422`).

### Category label templates

A category names the label templates its assets join the spool with, in order of preference, up to
10. Its subcategories take them unless they name their own. Set them with
`PUT /api/v1/categories/{id}/label-template` (`categories.manage`):

- `{"label_template_ids": [...]}` for a list;
- `{"label_template_id"}` for one;
- an empty list or null for none.

A category answers with `label_template_ids`, and the first as `label_template_id`. The one template
a category named before migration `030` becomes its first.

The list is chosen as scan templates are (see [Scanning](scanning.md)): the closest category, ties
settled by depth, size and age, with no default. Categories naming the same list in the same order
agree. An asset's `label_template` gives the choice, with the list as `template_ids`.

Ties are marked as for scan templates, and listed by:

- `GET /api/v1/assets?conflict=label_template`;
- `GET /api/v1/categories/label-conflicts`, which takes `label_template_ids=a,b` to ask as if a
  category named them.

A template a category names cannot be deleted (`409`).

### Adding labels

`POST /api/v1/spool` adds labels, taking exactly one of:

- `asset_ids`;
- `category_id`, with `"subcategories": true` for those below it too;
- `filter`, an asset filter as `GET /api/v1/assets` takes it (never archived).

Each label takes:

- `template_id` when given; else the first of its categories' templates that suits its
  `destination` (see [the fit check](#the-fit-check)), or the first when it has no destination yet;
- `destination` when given; else the one printer whose fit check its template passes, if only one
  does;
- `copies`, 1 by default.

The answer lists the entries `added` (`201` when there are any), the assets `already_waiting`, and
those `skipped` as archived or not found.

The web app adds labels from:

- the asset list (the selection, or everything the filters show);
- a category;
- an asset's page, and its scan page;
- the new-asset form and CSV imports. These two start ticked and remember each person's choice in
  their browser.

### Sorting the queue

`GET /api/v1/spool` lists the waiting entries in the order added, for sorting, with the templates
and printers. It also returns:

| Field | Meaning |
|---|---|
| `fits` | Which printers each template suits |
| `fit_warnings` | What to know before printing each template where it can go; never keeps an entry from printing |
| `template_from_category` | Whether each entry's template came from its categories; choosing one by hand clears it |
| `problems` | Per entry: no template, no destination, a template its printer does not suit, an archived asset, or why its label could not be produced last time |

`?mine=true` keeps those the requester added.

`PATCH /api/v1/spool` sets `template_id`, `destination` or `copies` on `entry_ids`, all or none,
and clears their last problem. Setting a template on entries with no destination sends them to the
one printer it suits, if only one does.

`POST /api/v1/spool/remove` takes entries out.

### Printing from the spool

`POST /api/v1/spool/print` makes print jobs of `entry_ids`. With no `entry_ids`, it takes every
entry ready to print as it stands that is going to a printer. It answers at once with the jobs
(`201`) and the entries left out, with why.

- Each printer and template is a job of at most 500 labels, in the order the entries were added.
- An entry's last failure does not stop it printing again; its blockers do.
- Entries in a job show it (`job`, with its state and printer) until it is done. Meanwhile they
  cannot be changed, removed or printed again (`409`).

### Downloading from the spool

`POST /api/v1/spool/download` lays out `entry_ids`, all of one template, each as many times as its
copies, as one PDF or a ZIP of PNGs (`"mono": true` for 1-bit). Labels that cannot be produced stay
waiting with the reason, and `X-Spool-Problems` says how many. The rest count as printed, recorded
as a finished job to send again.

### The spooler

The spooler in `bam serve` sends jobs in the background, oldest first.

- Each printer takes one batch of a job at a time, and printers work in parallel.
- Direct printing (`POST /api/v1/printers/{id}/print`) takes the same per-printer turn, so two jobs
  never interleave on a printer served by one server.
- The database keeps two servers sharing it from sending the same batch, or two batches to one
  printer.
- Labels are laid out as whoever asked for the job may see them when it is sent. Nothing is printed
  for an account deactivated or a token revoked meanwhile.
- A label that cannot be produced stays waiting with the reason while the rest print.
- A printer that cannot be reached fails the job, and its labels wait with the error.

A batch beats a heartbeat every few seconds from when it is claimed, through laying out its labels,
waiting for the printer's turn and sending. One without a beat for 30 seconds, as a stopped server
leaves it, fails, and its labels wait marked as perhaps printed. A job whose printer is deleted
before it starts fails too.

### Jobs

`GET /api/v1/spool/jobs` lists jobs newest first, from the last 30 days unless `?older=true`. Each
job has:

- the template and printer as named then;
- the state: `waiting`, `sending`, `done`, `failed` or `canceled`;
- any error;
- the labels in all, and printed so far;
- for a pool's job, its `members`, each with the labels it printed, whether it is sending and
  whether it dropped out.

`POST /api/v1/spool/jobs/{id}/cancel` cancels a job not yet started (`409` after), and its labels
wait again.

`POST /api/v1/spool/jobs/{id}/resend` puts the labels a finished job printed back in the spool with
their template and copies. They go to `destination` when given, and where the job went otherwise.
They print at once when that is a printer or a pool.

### Printed labels

Printed entries stay, as the Printed list: `GET /api/v1/spool?printed=true`, most recent first,
from the last 30 days unless `?older=true`, with the template and printer as named when printed.

`POST /api/v1/spool/requeue` puts them back to print again with the template, destination and
copies they had.

Printing records `asset.label_printed` in each asset's history. A download names no printer and
sets `download`.

## Printer pools

A printer pool is a set of printers loaded alike that share print jobs. Pools are managed with the
printers through `/api/v1/printer-pools` (`settings.manage`), and listed to those who may see
printers. A pool has:

- a name;
- a batch size (1–100, 10 by default);
- whether shares are fixed throughout;
- its printers, each with a share (1–100, 1 by default).

An entry's destination may be `pool:<id>`. A template's labels take a pool by themselves when it is
the one pool they suit and it holds every printer they suit.

A pool's job is cut into batches of about the batch size, with a label's copies kept together. Its
members are the printers whose fit check the template passes. The others sit the job out, which the
print answer's `warnings` says.

### How batches are shared

- **Direct connections** (`tcp`, `device`): delivery tracks printing, because the printer stops
  taking data when its buffer fills. These members take the next batch when they have sent their
  last, so a faster printer prints more.
- **Behind a spooler** (`ipp`, `windows`): the spooler accepts a whole job at once. These members
  are given their share of the batches up front, by share, interleaved.
- **Fixed shares:** every member is given its share up front, as behind a spooler.

Each member takes one batch at a time, like any printer.

A member that cannot be reached, or whose media no longer suits, hands its batch, and any it was
given, back to the rest and drops out of that job. The others take them, including those behind a
spooler once no other is left. The job fails only when every member has dropped out.

Deleting a pool leaves its waiting labels with nowhere to go, and fails its batches no member holds.
Deleting a printer takes it out of its pools.

## API

### Printer API

| Method and route | Permission | Purpose |
|---|---|---|
| `/api/v1/printers` | `settings.manage`; listing: `labels.print` or `templates.manage` | Configure printers |
| `GET /api/v1/printers/drivers` | `labels.print`, `settings.manage` or `templates.manage` | Every registered driver, with its models (and each model's DPI), media and options; for drivers that take custom stock, how wide each model's labels may be and how long continuous ones (`custom_stock`) |
| `POST /api/v1/printers/{id}/fit` | template designers | Fit check without printing |
| `POST /api/v1/printers/{id}/test-print` | `labels.print` and `templates.manage` | Print one label of a possibly unsaved template |
| `POST /api/v1/printers/{id}/changes` | `settings.manage` | What a change would strand, without saving |
| `POST /api/v1/printers/{id}/print` | `labels.print` | Print labels directly |
| `/api/v1/label-stock` | `settings.manage`; listed to those who may see printers | Custom label stock |
| `/api/v1/printer-pools` | `settings.manage`; listed to those who may see printers | Printer pools |
| `GET /api/v1/templates` | `assets.read` | Includes `suits`, to those who may see printers |

### Labels API

| Method and route | Permission | Purpose |
|---|---|---|
| `POST /api/v1/labels/export` | `assets.read` | Multi-page PDF or ZIP of PNGs |
| `POST /api/v1/labels/options` | `assets.read` (printers and fits need `labels.print`) | Which templates and printers can label assets |
| `PUT /api/v1/categories/{id}/label-template` | `categories.manage` | A category's label templates |
| `GET /api/v1/categories/label-conflicts` | `assets.read` | Categories whose label template choice ties |
| `GET /api/v1/assets?conflict=label_template` | `assets.read` | Assets whose label template choice ties |

### Spool API

Every spool route needs `labels.print`.

| Method and route | Purpose |
|---|---|
| `POST /api/v1/spool` | Add labels |
| `GET /api/v1/spool` | List waiting entries (`?mine=true`); `?printed=true` for the Printed list (`?older=true`) |
| `PATCH /api/v1/spool` | Set template, destination or copies on entries |
| `POST /api/v1/spool/remove` | Take entries out |
| `POST /api/v1/spool/print` | Make print jobs |
| `POST /api/v1/spool/download` | Download entries as PDF or ZIP |
| `POST /api/v1/spool/requeue` | Put printed entries back |
| `GET /api/v1/spool/jobs` | List jobs (`?older=true`) |
| `POST /api/v1/spool/jobs/{id}/cancel` | Cancel a job not yet started |
| `POST /api/v1/spool/jobs/{id}/resend` | Print a finished job's labels again |

## Storage

| Migration | Adds |
|---|---|
| `023` | The spool |
| `024` | Print jobs |
| `025` | Printer pools |
| `028`, `029` | Custom label stock; `029` widens `printers.media` to hold its ID |
| `030` | A category's list of label templates (before it, one template) |

Printers are stored in the `printers` table with a name, driver, model, connection, the media
currently loaded, and driver `options` as a JSON object.

## Internals

### Drivers

Printer families are supported by **drivers** (`internal/printers`), registered by name and chosen
per printer:

```go
type Driver interface {
    Info() DriverInfo                                // models, media and options, for clients
    Validate(s Settings) error                       // model, media and options of a printer
    Plan(s Settings, label LabelSpec) (Plan, error)  // fit check; ErrDoesNotFit → 422
}

type Plan interface {
    Encode(labels []*image.Gray) ([]byte, error)     // 1-bit labels in, one print job out
}
```

The print endpoint only renders labels, asks the printer's driver to plan and encode them, and
hands the job to a `Transport` for the printer's connection (see [Delivery](#delivery)). Adding a
printer family means registering a driver; the API does not change.
