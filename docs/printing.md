# Printing

BAM! prints labels straight to label printers over the network, a USB port, a print server or the
Windows spooler. You configure any number of printers, each with a driver for its family, the
media it has loaded, and a connection saying how BAM reaches it. Before anything prints, a fit
check makes sure the template suits the roll.

Labels can be printed at once, a batch at a time, or gathered over time in the spool (the
**Print queue** in the web app) and sorted onto templates and printers in bulk. Printer pools
spread one job across several printers loaded alike. Lasers engrave a label onto a workpiece, one
piece at a time, started by a person at the machine (see [Lasers](#lasers)).

For templates, content bindings and output formats, see [Labels](labels.md).

## Printers

Each printer is stored with:

- a name;
- a driver, chosen per printer;
- a model;
- a connection (see [Delivery](#delivery));
- the media currently loaded;
- driver `options`, as a JSON object.

Seven printer drivers ship today: `brother_ql`, `brother_td`, `brother_ptouch`, `dymo_labelwriter`,
`tspl`, `zpl` and `epl2`; and three for e-paper displays, which are printers too: `display`,
`openepaperlink` and `trmnl` (see [Displays](#displays)). Option values are listed by the drivers endpoint, and unknown options are
refused.

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
- The template draws in an [ink](labels.md#inks) the printer lacks, which prints black: "The
  printer prints only black and white, so red prints black."

Printing answers with the warnings too. So do the spool (`fit_warnings`, by template and
destination) and printing from it.

### Checking a fit from the editor

`POST /api/v1/printers/{id}/fit` takes:

- the template's size and DPI;
- optionally its elements' boxes in millimeters (`content`);
- optionally its `feed`;
- optionally the [inks](labels.md#inks) it draws in (`inks`), for a warning of any the printer
  lacks.

It answers `fits`, with `warnings` or why not. When it fits, it also answers with:

| Field | Meaning |
|---|---|
| `feed` | Which way the label goes through the printer: `across` or `along` |
| `palette` | The inks the printer prints in: `bw`, or more (see [Inks](labels.md#inks)) |
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

- TSPL printers, Zebras (ZPL and EPL2) and DYMO LabelWriters take it, up to their head's width.
- Brother TD printers take it, as wide as each model's rolls may be.
- Brother QL and P-touch printers take only their built-in rolls and cassettes. They identify the
  roll or cassette loaded, and need Brother's figures to place a label on it.

Changing stock checks every printer with it loaded. The change is refused, naming the printer, if
one would no longer take it. Stock loaded in a printer cannot be deleted (`409`).

## Drivers

### `brother_ql`

For Brother's QL range, which speaks Brother's raster command language and takes DK rolls:

| Line | Models | Head |
|---|---|---|
| 62 mm | QL-500, QL-550, QL-560, QL-570, QL-580N, QL-650TD and QL-700 | 720 pins |
| 62 mm | QL-710W and QL-720NW | 720 pins |
| 62 mm | QL-800, QL-810W and QL-820NWB | 720 pins |
| 4-inch | QL-1050 and QL-1060N | 1,296 pins |
| 4-inch | QL-1100, QL-1110NWB and QL-1115NWB | 1,296 pins |

Every model prints at 300 DPI. The figures come from Brother's raster command references:

- *Brother QL Series Command Reference (QL-500/550/560/570/580N/650TD/700/1050/1060N)*, version 6.0;
- *Raster Command Reference QL-600/QL-710W/QL-720NW*, version 1.02;
- *Raster Command Reference QL-800/810W/820NWB*, version 1.01;
- *Raster Command Reference QL-1100/1110NWB/1115NWB*, version 1.00.

Brother links them from its [command reference
list](https://support.brother.com/g/s/es/dev/en/command/reference/index.html).

**Options:** `cut` (default `true`) cuts each label off as it finishes; turned off, the tape feeds
on uncut. The QL-500 has no cutter: it feeds on, and turning `cut` on for it is refused.

**Media.** The media catalog records, for each DK roll:

- its width;
- whether it is die-cut, and if so its label length;
- its printable width in head dots;
- how many pins sit beside the tape on each head;
- which models take it.

The rolls:

- continuous tape 12, 29, 38, 50, 54 and 62 mm wide, and 102 and 103 mm on the 4-inch models;
- die-cut labels 17 × 54, 17 × 87, 23 × 23, 29 × 42, 29 × 90, 38 × 90, 39 × 48, 52 × 29,
  54 × 29, 60 × 86, 62 × 29, 62 × 60, 62 × 75 and 62 × 100 mm;
- die-cut labels 102 × 51, 102 × 152 and 103 × 164 mm on the 4-inch models;
- round labels 12, 24 and 58 mm across.

Each model takes the rolls its own reference lists:

- 29 × 42 and 60 × 86 mm labels are not in the QL-500 to QL-1060N reference.
- 54 × 29, 62 × 60 and 62 × 75 mm labels are only in the QL-800 series'.
- The QL-1050 and QL-1060N take the 102 mm rolls but not the 103 mm ones.
- The QL-1115NWB takes neither 103 mm roll.

A printer is refused with a roll its model does not take. The drivers endpoint lists, on each roll
that not every model takes, the models that do (`models`). The printer form offers each model only
those.

Rolls are named by their DK part number where Brother sells them under one; the references name
none.

**Red and black.** The QL-800, QL-810W and QL-820NWB print red as well as black on DK-22251, 62 mm
continuous tape. With it loaded, the printer's palette is `bwr` (see [Inks](labels.md#inks)):
red prints red, and any other ink but white prints black. Only those models take the roll. The
drivers endpoint gives each model's palette (`model_palette`) and the roll's (`palette`). A
printer's `loaded` roll gives the palette it prints in.

Each line of the label is sent twice, as the QL-800 series reference describes: once for black
(`w` 01) and once for red (`w` 02), with expanded mode's two-color bit set. Red prints only where
black does not. Labels in black alone are sent the same way, with nothing in red.

**Fit and margins.** Before any asset is laid out, the fit check requires the template to suit the
loaded media, at 300 DPI. Continuous labels can be as long as:

- 1,000 mm on the 62 mm models;
- 3,000 mm on the 4-inch ones.

The shortest is 25 mm, or 12.7 mm on the QL-570, QL-580N, QL-700, QL-710W and QL-720NW, and 25.4 mm
on the QL-1100 series. The head reaches the middle of the tape, and the middle of a die-cut label's
length, by Brother's printable pins and lines.

The printer cannot reach the tape edges, so the rendered label is cropped to the printable area
around its center:

- 306 of the 343 dots across 29 mm tape (about 1.5 mm lost on each side);
- 696 across 62 mm tape;
- 1,164 across 102 mm;
- on DK-11201, 991 of 1063 dots along the label (about 3 mm at each end).

Keep elements clear of those margins.

**Encoding.** Each label is rendered 1-bit at 300 DPI (in black, white and red on DK-22251) and
encoded as a raster stream. The stream
is: invalidate, initialize, then per label:

1. raster mode;
2. print information;
3. auto-cut;
4. margins and TIFF compression;
5. raster lines (90 bytes for each line of the 720-pin head, 162 for the 1,296-pin one, mirrored
   because the first pin sits at the right of the tape);
6. a print command.

Each model is sent only the commands its reference lists for it:

| Models | Raster mode | Cutting | TIFF compression |
|---|---|---|---|
| QL-500 | no | none | no |
| QL-550, QL-560 | no | auto-cut | no |
| QL-570, QL-700 | no | auto-cut, cut every label, cut at end | no |
| QL-650TD | yes | auto-cut, cut at end | no (over serial only) |
| QL-580N, QL-1050, QL-1060N, QL-710W, QL-720NW, QL-810W, QL-820NWB, QL-1100 series | yes | auto-cut, cut every label, cut at end | yes |
| QL-800 | yes | auto-cut, cut every label, cut at end | no |

Uncompressed raster lines span the whole head. With `cut` on, every label is cut off as it
finishes. The invalidate is 350 bytes on the QL-1100 series, as its reference asks, and 200 on
the rest.

**Where the references differ:**

- **The QL-500 to QL-1060N reference** contradicts itself on which models take "cut every
  label" and the cut at the end: its overview leaves out the QL-560, and its command details
  include it. The narrower list is followed. Both commands only restate the printer's own
  defaults.
- **The QL-800 series reference** asks for a 400-byte invalidate, and allows continuous labels
  from 12.7 mm. The QL-810W and QL-820NWB have always been sent 200 bytes and held to 25 mm, and
  the QL-800 is kept with them.
- **The QL-1100 reference's pin table** gives two figures that do not add up to its 1,296 pins.
  They are for 62 mm continuous tape and the 62 mm die-cut labels. The QL-1050's figures, which
  do, are used.

### `brother_td`

For Brother's TD-2 and TD-4 series of direct thermal printers, which speak the same raster command
language as the QL range but take rolls of any size:

| Series | Models | Resolution | Head |
|---|---|---|---|
| TD-2 | TD-2020, TD-2120N, TD-2125N and TD-2125NWB | 203 DPI | 448 pins (56 mm) |
| TD-2 | TD-2030A, TD-2130N, TD-2135N and TD-2135NWB | 300 DPI | 672 pins (57 mm) |
| TD-4 | TD-4210D, TD-4215D, TD-4410D, TD-4415D, TD-4420DN, TD-4425DN and TD-4455DNWB | 203 DPI | 832 pins (104 mm) |
| TD-4 | TD-4510D, TD-4520DN, TD-4525DN, TD-4550DNWB and TD-4555DNWB | 300 DPI | 1,280 pins (108 mm) |

The figures come from Brother's *Raster Command Reference* for the TD-2000 series (version 1.01)
and the TD-4000 series (version 1.03).

The TD-4T thermal transfer models (TD-4420TN, TD-4520TN, TD-4650TNWB and TD-4750TNWB) do not speak
this language. Their languages are Brother's FBPL and the emulations of ZPL, EPL and DPL: use
`zpl` for them. The linerless TD-4 models are left out.

**Why a driver of its own.** The command streams are nearly the QL's: the same commands, with
heads of other widths. What differs is the media:

- A QL identifies the DK roll loaded. Placing a label on its head takes Brother's table of pins
  for that roll.
- A TD prints on any roll from 19 mm wide (up to 63 mm on the TD-2, 118 mm on the TD-4). The
  printer is set up for the roll on its own, with its size and whether it has gaps or black
  marks.

So `brother_td` takes custom stock, which `brother_ql` cannot, and the two keep separate media
lists.

**Options:** `cut` (default `false`) cuts each label off as it finishes, with a TD-4's optional
cutter fitted. A cut label must be at least 20 mm long. The TD-2 series has no cutter, and
turning `cut` on for it is refused.

**Media.** The TD references size Brother's RD rolls, which are built in:

- **TD-2:** continuous tape 57 and 58 mm wide, and die-cut labels 30 × 30, 40 × 40, 40 × 50,
  40 × 60, 50 × 30, 51 × 26 and 60 × 60 mm.
- **TD-4:** continuous tape 58, 76, 90 and 102 mm wide, and die-cut labels 51 × 26, 76 × 26,
  102 × 50 and 102 × 152 mm.

Custom stock of any size the model takes works the same way.

**Set the printer up for the roll** with Brother's own tools: the Paper Size Setup in its printer
driver or Printer Setting Tool, and the media sensor for gaps or black marks. The raster language
can carry that setup only as an opaque block, which BAM! cannot produce, so it sends only the
labels. A custom stock's gap is not sent either.

**Fit and margins.** Templates must use the model's resolution. A label is placed centered on the
head, as every RD roll is in Brother's tables:

- across the roll, everything but 1.5 mm at each side prints, up to the width of the head;
- along a die-cut label, everything but 3 mm at each end prints;
- continuous labels are fed a 3 mm margin, and are 12 mm to 1,000 mm (TD-2) or 3,000 mm (TD-4)
  long.

These offsets reproduce Brother's printable pins and lines for every RD roll to within two dots.
The exception is 60 mm labels on a 300 DPI TD-2: the head is narrower than the label, and is
printed across whole, where Brother keeps 2 mm off each side.

**Encoding.** The stream is the QL's, with these differences:

- **Invalidate:** 200 bytes on the TD-2 series. On the TD-4 series, 350 bytes, or 661 on the
  TD-4215D, TD-4415D, TD-4425DN, TD-4455DNWB, TD-4525DN and TD-4555DNWB.
- **Commands:** the TD-2 series takes neither "cut every label" nor the cut at the end. Its mode
  setting has no cutter.
- **Raster lines** are 56, 84, 104 or 160 bytes, by the head, and TIFF-compressed.
- **Print information** gives the roll's type and size but asks the printer to check none of
  them, as Brother's own TD-4 example does. Otherwise a TD would refuse a job whose size did not
  match the paper format it was set up with.

### `brother_ptouch`

For Brother's P-touch tape printers, which print laminated TZe tape and HSe heat-shrink tube:

| Models | Resolution | Head | Connections |
|---|---|---|---|
| PT-P700 | 180 DPI | 128 pins (18 mm) | USB |
| PT-E550W, PT-P750W | 180 DPI | 128 pins | USB, Wi-Fi |
| PT-P710BT | 180 DPI | 128 pins | USB, Bluetooth (as a serial port: see [Bluetooth](#bluetooth)) |
| PT-P900 | 360 DPI | 560 pins (39.5 mm) | USB |
| PT-P900W, PT-P950NW | 360 DPI | 560 pins | USB, Wi-Fi (and Ethernet on the PT-P950NW) |
| PT-P910BT | 360 DPI | 560 pins | USB, Bluetooth (as a serial port: see [Bluetooth](#bluetooth)) |

Reach a networked one with `tcp`, and one on USB with `device`, `ipp` (a raw queue) or `windows`. A
Bluetooth one, paired, is a serial port: `device` on Linux and `windows` on Windows. That has not
yet been checked on these printers.

The figures come from Brother's raster command references, linked from its [command reference
list](https://support.brother.com/g/s/es/dev/en/command/reference/index.html):

- *Raster Command Reference PT-H500/P700/E500*, version 1.11;
- *Raster Command Reference PT-E550W/P750W/P710BT*, version 1.02;
- *Raster Command Reference PT-P900/P900W/P950NW/P910BT*, version 1.02.

Brother publishes no raster reference for the other models #232 names: the PT-E560BT, PT-D600,
PT-D610BT and PT-P300BT. They are left out rather than guessed at. The PT-P300BT is
Bluetooth-only besides.

**Why a driver of its own.** The language is a close relative of the QL's, and the driver shares
`brother_ql`'s PackBits and page preparation. But what a P-touch prints on, and how it finishes a
label, differ:

- **Tape cassettes, not rolls.** Every label is continuous: the template sets its length, and the
  cassette only its width.
- **Different command details.** The cassettes are named by other media types, and their widths
  are checked another way. Raster lines are sent with a two-byte length (`G`, or `g` on the
  PT-P700).
- **Different cutting.** A P-touch half cuts and chains labels, mirrors them for clear tape, and
  takes margins of its own. Those are options a QL has no use for, and the QL's `cut` means
  something else.
- **Its own status reply,** reporting the cassette loaded (see below).

**Options:**

- **`cut_after`:** `each_label` (default) cuts each label off; `last_label` cuts only after the
  job's last.
- **`half_cut`** (default `false`) cuts through the tape between labels but not its backing, so
  a job comes out as one strip. The PT-P700 and PT-P710BT do not half cut, and it is refused for
  them.
- **`chain`** (default `false`) leaves the last label in the printer, unfed and uncut, so the
  next job wastes no tape before its first label.
- **`mirror`** (default `false`) prints labels mirrored, to read through clear tape.
- **`margin_mm`** is the tape fed before and after each label. It runs from 2 mm (1 mm on the
  PT-P900 series) to 127 mm; unset is the least.

**Media.** The cassettes, with each one's printable pins on each head (section 2.3.5 of the
references):

| Cassette | 128-pin head | 560-pin head |
|---|---|---|
| TZe 3.5 mm | 24 | 48 |
| TZe 6 mm | 32 | 64 |
| TZe 9 mm | 50 | 106 |
| TZe 12 mm | 70 | 150 |
| TZe 18 mm | 112 | 234 |
| TZe 24 mm | 128 | 320 |
| TZe 36 mm | – | 454 |
| HSe 5.8, 8.8, 11.7, 17.7, 23.6 mm (2:1) | 28, 48, 66, 106, 128 | 56, 96, 132, 212, 256 |
| HSe 5.2, 9, 11.2, 21 mm (3:1) | 20, 44, 50, 120 | 40, 88, 100, 240 |
| HSe 31 mm (3:1) | – | 360 |

The PT-P910BT takes no tube. A printer is refused with a cassette its model does not take, and the
printer form offers each model only those.

**Fit and margins.** Templates use the model's resolution, 180 or 360 DPI. A template's height is
the tape's width (the template is turned, so its width runs along the tape, as tape labels do).
Its other side is the label's length:

- 4.4–1,000 mm at 180 DPI, and 4–1,000 mm at 360 DPI;
- tube to 500 mm.

The margins are fed around it. The printer feeds out at least about 25 mm of tape, whatever the
label's length. Only the middle of the tape prints, by Brother's printable pins: 70 dots of 12 mm
tape's 84 at 180 DPI, for example. The fit check warns of anything beyond them.

**The loaded cassette.** Each job names the cassette's width (3.5 mm tape is named 4, as the
printer reports it) and asks the printer to check it. A printer with another width loaded refuses
the job rather than printing it on the wrong tape. A tube is checked by its type (2:1 or 3:1)
instead, since a tube reports the width of a tape. The 3:1 tubes' widths are given nowhere.

The printer's status reply also reports the cassette loaded. Package `ptouch` reads it, checks it
against the cassette a printer is set up for, and finds the cassette for the fit check
(`ParseStatus`, `Status.Check`, `Status.LoadedMedia`). Nothing asks for it yet: every connection
BAM! has today only sends. When a connection that also reads arrives, the fit check can use the
reported cassette, and printing can be refused before anything is sent.

**Encoding.** Each label is rendered 1-bit and sent as raster lines, TIFF-compressed, mirrored
across the head, as the references' section 2.1 lays out. The stream is:

1. invalidate (100 bytes at 180 DPI, 200 on the PT-P900 series) and initialize;
2. then for each label:
   - raster mode;
   - print information (the cassette and the label's length);
   - various mode (auto cut at 40h, mirror at 80h);
   - "cut each label" where the model takes it;
   - advanced mode (half cut at 04h, and 08h unless chained);
   - the margin and compression;
   - the lines and a print command.

The PT-P900 series also numbers the last page in its print information.

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

**Models:** all at 203 DPI, with each maker's maximum print width.

- **Rollo** X1038 and X1040, and **Munbyn** ITPP941: 104 mm.
- **iDPRT** SP410 and SP420, **Polono** PL60, **Arkscan** 2054A, **HPRT** SL42, **Xprinter** XP-420B
  and XP-470B, **Phomemo** PM-241 (in TSPL, not ESC/POS), and **TSC**'s own TE200, TE210, DA210
  and TTP-244 Pro: 108 mm.
- **Polono** PL70e: 104 mm. No maker's figure was found, so this takes the narrower width of most
  4-inch printers.
- **Generic** TSPL printers at 203 DPI (104 mm) and 300 DPI (106 mm), for any other.

No quirks are known among them yet: each is sent the size and gap of its labels, as the generic
models are. Others are added as people report them.

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

### `zpl`

For printers that speak ZPL II, the most widely spoken label language: Zebra's desktop and
industrial printers, and the many that emulate it, such as Honeywell, TSC, Godex, Citizen,
Brother's TD series in ZPL mode, and inexpensive 4-inch printers. The driver follows Zebra's
*ZPL II Programming Guide*.

**Models:**

- **4-inch, 203 DPI:** Zebra GK420d and GK420t, GX420d, and ZD220.
- **4-inch, 300 DPI:** GX430t.
- **2-inch, 203 DPI:** ZD410.
- **By resolution:** ZD420 and ZD421, ZD620 and ZD621, and ZT230 at 203 or 300 DPI; ZT410 and
  ZT411 at 203, 300 or 600 DPI.
- **Generic, for emulators:** ZPL printers at 203, 300 and 600 DPI with a 4-inch head, and at 203
  DPI with a 2-inch or an 8-inch head.

**Options:** each left unset keeps the printer's own setting.

- `media_type`: `direct` for direct thermal labels, or `transfer` for thermal transfer with a
  ribbon (`^MT`).
- `darkness`: 0–30 (`~SD`).
- `speed`: 1–14 inches per second, as far as the model goes (`^PR`).
- `print_mode`: `tear_off`, `peel_off` or `cutter` (`^MM`).
- `calibrate`: default `false`; set it to measure the labels before each job (`~JC`), as after
  loading a new roll.
- `compress`: default `true`; turn it off for an emulator that mishandles Zebra's compression.

**Media.** Zebra media is given by its size, so the catalog lists the same common sizes as
[`tspl`](#tspl), with 4 × 1 in too. Any other size is [custom stock](#custom-label-stock). The fit
check is at the model's DPI. Continuous labels are 10–1000 mm long. A label wider than the head is
printed around its middle.

**Encoding.** `~JC` and `~SD` first, when set. Then, for each distinct label, one format:

1. `^XA`;
2. `^MNY` for die-cut labels, found by their gaps, or `^MNN` for continuous media;
3. `^MT`, `^MM` and `^PR`, when set;
4. `^PW` and `^LL`, the label's width and length in dots, and `^LH0,0`;
5. the label as graphics, `^FO` then `^GFA`, centered across the label. Each graphic is at most
   99,999 bytes, so a large label is sent in bands, one under the other. They use ZPL's ASCII
   compression unless `compress` is off;
6. `^PQ` with the copies, and `^XZ`.

None of the printer's own fonts or barcode commands are used, so output matches the SVG and PDF
exports.

**Connections.** `tcp` on port 9100 for networked Zebras, and `device`, `ipp` or `windows` for one
on USB (see [Delivery](#delivery)).

### `epl2`

For the older Zebras that speak EPL2: the LP and TLP 2824 and 2844, which many spaces still have
from shipping desks. The driver follows Zebra's *EPL2 Programming Guide*.

**Models:** Zebra LP 2824 and TLP 2824 (2-inch head), LP 2844 and TLP 2844 (4-inch head), and a
generic EPL2 printer at 203 DPI with a 4-inch head.

**Options:** `density`, 0–15; unset keeps the printer's own setting.

**Media.** As for [`zpl`](#zpl).

**Encoding.** For each distinct label:

1. `N`, to clear the image buffer;
2. `q` and `Q`, the label's width, and its length and gap in dots, with a gap of 0 for continuous
   media;
3. `ZT`, to print from the top, and `D` with the density, when set;
4. one `GW` holding the whole label, centered across it (a 0 bit prints a dot);
5. `P` with the copies.

## Delivery

Each printer has a `connection`, `{"type": ..., "address": ...}`, saying how BAM reaches it:

| Type | Address | Use |
|---|---|---|
| `tcp` | `host` or `host:port` (default 9100) | Network printers, and USB printers shared by a raw print server such as `p910nd`. |
| `device` | A device path on the BAM server, such as `/dev/usb/lp0` (Linux) or `/dev/ulpt0` (FreeBSD) | A USB printer plugged into the BAM server. The path must match a pattern in the server's `[printers] devices` setting, which is empty by default: otherwise anyone who can manage printers could make BAM write to any file it can open. BAM never creates the file, and needs permission to write the device (on Linux, usually membership of the `lp` group). |
| `ipp` | `ipp://host[:port]/path`, or `ipps`, `http`, `https` | A queue on CUPS or another IPP server, such as a USB printer shared from another machine. The job is sent as `application/octet-stream`, so the queue must be raw (CUPS: `lpadmin -m raw`; CUPS 2.x still supports raw queues though it marks them deprecated). CUPS can also forward a raw queue to a Windows shared printer through its `smb://` backend. |
| `windows` | A Windows printer name or `\\host\share` | When BAM runs on Windows: a local or shared printer, printed through the spooler with the `RAW` datatype. Windows has no device paths for USB printers, so this covers both. |
| `ble` | The printer's Bluetooth address, such as `7C:2F:80:A1:B2:C3`, then optionally `?service=…&write=…&notify=…&ack=…` | A Bluetooth LE printer in range of the BAM server, on Linux. The address must match a pattern in the server's `[printers] bluetooth` setting, which is empty by default. See [Bluetooth](#bluetooth). |
| `openepaperlink`, `pull`, `webhook` | See [Displays](#connections) | E-paper displays only. |

A connection may also name a relay, as `"relay": "front-desk"`: BAM! run beside the printer on its
own network, for a BAM! hosted elsewhere. The address is then on the relay's network, and the
relay's own allowlist, not `[printers] devices` or `bluetooth`, decides what it may reach. The main node still
encodes the job; the relay delivers the bytes. A relay that is offline fails the job, and its labels
wait in the queue (see [Relays](relays.md#printers-through-a-relay)).

Jobs are sent one at a time per destination, so concurrent requests never interleave their
streams. A job counts as printed once the destination has accepted the whole stream. Reading back
printer status is a later improvement.

### Bluetooth

Label makers that connect by Bluetooth use one of two kinds of it, and BAM! reaches each its own
way. Neither has yet been checked on real hardware.

**Bluetooth Classic** is a serial port (RFCOMM), as Brother's P-touch printers and many older ones
use. It needs no connection type of its own: once paired, the system presents the printer as a
serial port, which `device` or `windows` reaches.

- **On Linux,** pair and trust the printer (`bluetoothctl`, then `pair` and `trust` with its
  address), and bind a serial device to it: `rfcomm bind 0 7C:2F:80:A1:B2:C3 1`, where 1 is the
  printer's serial port channel (`sdptool browse` with its address lists it; most use 1). The
  printer is then `/dev/rfcomm0`, a `device` connection, with `/dev/rfcomm*` in `[printers]
  devices`. The device belongs to group `dialout` on most distributions, so the service needs
  `SupplementaryGroups=dialout`. A binding does not outlast a restart, so run `rfcomm bind` from a
  unit started before BAM!'s. Some distributions ship `rfcomm` and `sdptool` apart from BlueZ, as
  `bluez-deprecated` on Fedora.
- **On Windows,** pairing gives the printer a COM port (**Bluetooth settings › More Bluetooth
  options › COM Ports**, the outgoing one), or the maker's driver adds it as a printer. A printer
  Windows lists is a `windows` connection. For a bare COM port, add a printer on it first (**Add a
  printer › Add a local printer › Use an existing port**, with the **Generic / Text Only**
  driver), and use its name: BAM! prints `RAW`, so the driver never changes the job.

**Bluetooth LE** (GATT) is the `ble` connection, for printers such as Niimbot's and many of
Phomemo's. Its address is the printer's Bluetooth address, then the GATT service and
characteristics the job goes to:

```json
{"type": "ble", "address": "7C:2F:80:A1:B2:C3?service=fff0&write=fff2&notify=fff1"}
```

- **The profile.** A driver for Bluetooth LE printers gives its own service, write
  characteristic, notify characteristic, and whether the printer acknowledges writes, listed as
  `ble` by the drivers endpoint. The connection's `service`, `write`, `notify` and `ack` (`true` or
  `false`) override them. UUIDs are written in full, or as 16 or 32 bits of the Bluetooth base
  UUID (`fff0`). None of the drivers shipped today has a profile of its own, so a `ble` printer
  gives at least its service and write characteristic, from the printer's documentation.
- **Writing.** The job is written to the write characteristic in chunks the link allows: the ATT
  MTU agreed with the printer, less 3 bytes, so 20 bytes on a link that agreed none, and never more
  than 512. A characteristic that takes writes without response is written that way, and others
  with responses. When the printer acknowledges writes, each waits for it to notify once on the
  notify characteristic before the next is written.
- **Timeouts.** A printer that cannot be found and connected to in 20 seconds fails the job, as
  does one that takes 60 seconds to take a write, or to acknowledge it.
- **The server** reaches Bluetooth LE through BlueZ, over the system D-Bus, so on Linux only, with
  `bluetoothd` running and the adapter powered. The user BAM! runs as must be allowed to use
  BlueZ: on Debian and Ubuntu, members of group `bluetooth` are, so the service needs
  `SupplementaryGroups=bluetooth`. A printer BlueZ does not know yet is searched for; most need no
  pairing. BAM! disconnects from the printer after each job, if it connected it.
- **Elsewhere,** on Windows, macOS and FreeBSD, a `ble` connection is refused: "Bluetooth LE isn't
  supported on this system". A [relay](relays.md) on Linux beside the printer, such as a Raspberry
  Pi, reaches it instead.
- **Allowing it.** `[printers] bluetooth` lists the addresses BAM! may reach, or patterns of them
  such as `7C:2F:80:*` (`path.Match` syntax, in either case), or `*` for any. It is empty by
  default, refusing every `ble` connection, as `[printers] devices` refuses every device. On a
  relay, the relay's own `[printers] bluetooth` decides.

### Timeouts and errors

- A printer that cannot be reached in 5 seconds (a Bluetooth LE one, 20 seconds) fails the job.
- A printer that stops taking a job for 60 seconds (over TCP, a device or IPP), or that takes 60
  seconds to answer once it has the job, fails it.
- A printer takes a job only as fast as it prints it, so there is no limit on a long job that
  keeps moving.
- A connection the server may not use (a device or Bluetooth printer outside the allowed
  patterns, `windows` on another OS, or `ble` off Linux) is refused with `400` when the printer is
  saved, and with `502` at print time.
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

## Displays

An e-paper display is a printer whose driver drives displays. It has a model, which is its panel,
and the panel is its media: a "label" of the screen's size, at the panel's resolution, in its inks.
The fit check, the template editor, printing, the spool and the Printed list treat it as they treat
a printer loaded with die-cut labels of that size, so a label template shows on a display with
nothing new to learn. Nothing here has been tried on real hardware yet.

- **Printing an asset's label to a display** renders the template and sends the image to the
  display (push), or keeps it for the display to fetch (pull). A display shows one label at a time:
  of several printed at once, the last.
- **Each display remembers what it shows:** the template, the asset, and who showed it, as whom its
  refreshes are laid out. A test print is shown but not remembered.
- **Sizes.** A panel's size in millimeters is its pixels at its resolution, so a template of the
  screen's size, as the editor makes it from the display's screen, renders to its exact pixels. A
  label smaller than the screen is centered on white; a larger one is cut to it. A template whose
  height is the screen's width is turned 90° counterclockwise, as on a roll, and `feed` chooses as
  it does there. A screen's edges do not shift, so no content is warned of as near the edge.
- **Inks.** Each model declares its panel's inks ([Inks](labels.md#inks)), and labels are drawn in
  them; a template's ink the panel lacks shows black, with a warning.
- **Options**, for every display driver: `rotate_180`, for a display mounted upside down; for those
  that pull, `refresh_seconds` (60 to 86400), how long it waits before asking again; and where the
  driver makes more than one, `format`.

### Display drivers

| Driver | Models | Connections | Image |
|---|---|---|---|
| `display` | Panels by size: 1.54-inch 152 × 152, 2.13-inch 250 × 122, 2.9-inch 296 × 128, 4.2-inch 400 × 300 and 7.5-inch 800 × 480, each black and white or with red; 4.2-inch with red and yellow; 5.65-inch 600 × 448 in seven colors (ACeP); and 7.3-inch 800 × 480 Spectra 6 | `pull`, `webhook` | PNG (default) or BMP |
| `openepaperlink` | OpenEPaperLink tags: 1.54-inch 152 × 152, 2.6-inch 296 × 152, 2.9-inch 296 × 128, 4.2-inch 400 × 300 and 7.4-inch 640 × 384, with red | `openepaperlink` | JPEG |
| `trmnl` | TRMNL (`og`, 800 × 480) and TRMNL X (`x`, 1872 × 1404), in black and white | `pull`, by its MAC address | PNG (default) or BMP |

Each model's resolution is its panel's, near enough: its diagonal in pixels over its diagonal in
inches. PNGs are paletted, a bit a dot for black and white; BMPs are uncompressed with a palette, a
bit a dot for black and white and four for more inks, bottom up, as microcontroller libraries and
TRMNL read them. JPEGs are baseline at the highest quality, in gray for black and white.

### Connections

| Type | Address | How it reaches the display |
|---|---|---|
| `openepaperlink` | The access point, `host`, `host:port` or `http(s)://host[:port]`, then `?tag=` and the tag's MAC address, 6 or 8 bytes, such as `192.168.1.50?tag=0000021E3D6F1234` | Push: uploaded to the access point (below). It may go through a [relay](relays.md), whose allowlist lists the access point's URL. |
| `pull` | None; for a `trmnl` display, its MAC address | Pull: kept for the display to fetch with its own token (below). |
| `webhook` | A webhook [notification channel](notifications.md)'s ID | Push: posted through the channel (below). The channel must be a webhook, and on. |

A display driver takes only its own connections, and a printer's driver none of these. `pull` and
`webhook` are the server's own, never through a relay.

### OpenEPaperLink

[OpenEPaperLink](https://github.com/OpenEPaperLink/OpenEPaperLink) is open firmware for second-hand
electronic shelf labels, with an ESP32 access point. Following its wiki, BAM! posts each image to
the access point's `/imgupload` as `multipart/form-data`: `mac`, the tag's 8-byte MAC address;
`dither`, `0`, as the image is drawn in the tag's inks already; and `file`, the image, a baseline
JPEG of the tag's exact size. The access point answers `200` when it has it, and sends it to the tag
when the tag next checks in, typically within a minute. When it answers `409`, busy, the upload is
tried again after its `Retry-After`, at most 10 seconds, five times in all; any other answer fails
the job with what it said.

Go's JPEG encoder subsamples color, where the wiki recommends 4:4:4, so the edges of red may blur by
a dot until this is checked on a tag.

### Pulling

A display that pulls fetches `{base_url}/api/v1/displays/{id}/image` with one of its own tokens.

- **Tokens.** An admin (`settings.manage`) makes them under **Admin › Displays**, or with `POST
  /api/v1/displays/{id}/tokens` and `{"name"}`; the secret, starting `bamd_`, is shown once, with
  the URL. Only its SHA-256 is kept, as an API token's is. A token reaches its own display's image
  and nothing else: it is no API token, and another display's image refuses it with `403`.
  Revoking one (`DELETE /api/v1/displays/{id}/tokens/{token}`) stops it at once.
- **Presenting one:** as `?token=`, which suits displays that fetch a plain URL, such as ESPHome's
  `online_image`; as `Authorization: Bearer`; or as `Access-Token`. A URL with its token in it is a
  secret: whoever has it sees what the display shows.
- **The image** is in the display's resolution and inks, in its format. One shown nothing yet is
  white.
- **Caching.** The answer carries an `ETag`, which changes only when the image does: asked with
  `If-None-Match`, an unchanged image is `304` and not sent again.
- **Refresh.** The answer's `Refresh` header is the seconds to wait before asking again: the
  display's `refresh_seconds`, or the instance's default (below).
- Those who may see printers can fetch it too, signed in, to see what the display shows.

### TRMNL

The `trmnl` driver answers TRMNL's "bring your own server" API, as TRMNL documents it, in front of
the pull URL. Point the device at BAM!'s address as its server, and add it as a printer with the
`trmnl` driver, a `pull` connection, and its MAC address as the address.

- `GET /api/setup`, with the device's MAC address as `ID`: a device set up in BAM! with no token yet
  is given one, as `api_key`, with `friendly_id` and `image_url`. One already set up is refused
  (`409`) until its tokens are revoked, so that knowing a MAC address is not enough to take a
  device's key; one BAM! does not know is `404`. The body's `status` repeats the code, as TRMNL's
  firmware reads it.
- `GET /api/display`, with the key as `Access-Token` (or `ACCESS_TOKEN`): `status` `0`, the
  `image_url` to show, a `filename` that changes only when the image does, `refresh_rate` in
  seconds, `special_function` `none`, and no firmware update. An `ID` that is not the device the
  key is for is refused.
- `POST /api/log`: taken, `204`, and not kept.

TRMNL's devices take PNG, or a 1-bit BMP of exactly 800 × 480; an OG's image must be at most 90,000
bytes, which a label in black and white is far below. The fields BAM! answers are those TRMNL's
documentation lists; firmware that asks for more, such as a playlist, is not served it.

### Webhook

A `webhook` connection posts the image through a webhook channel: the image itself as the body, with
its media type (`image/png` or `image/bmp`), the channel's own headers, `BAM-Event: display.image`,
`BAM-Display` the display's ID, and `BAM-Delivery` and `BAM-Timestamp`, signed in `BAM-Signature`
when the channel has a secret, as its events are ([Notifications](notifications.md)). A body
template is for events, and is not used. Any answer but 2xx fails the job.

### Keeping displays current

When the asset a display shows changes, or something it is in does, a hook can refresh the display
([Hooks](hooks.md#actions)): its `refresh_displays` action, with an event trigger on the changes
that matter, such as `asset.edited`, `asset.quantity_changed` or `asset.checked_out`, offered on the
categories of the assets shown and of their containers.

- **A refresh** lays the display's template out again for its asset, as whoever last showed it, and
  sends it through the spool as a job, so retries, history and each printer's turn hold. A refresh
  whose person can no longer print fails, as their jobs would.
- **Bursts.** E-paper is slow, and some tags limit refreshes, so a refresh waits for the rest of a
  burst: asked for again before it is due, it is not asked for twice. It is due the instance's
  refresh window after the first ask, 30 seconds unless changed. Each is claimed in the database,
  so servers sharing it refresh once.
- **By hand:** `POST /api/v1/displays/{id}/refresh` (`labels.print`) refreshes a display now.

**The instance's settings** (`GET` and `PUT /api/v1/settings/displays`, set with `settings.manage`):

| Setting | Default | Range |
|---|---|---|
| `refresh_window_seconds` | 30 | 0 to 3600 |
| `pull_refresh_seconds` | 900 | 60 to 86400 |

## Lasers

A laser engraves labels that last: on tool handles, bin fronts, plywood tags, anodized aluminum or
acrylic. It is a **one-shot printer**: it makes one piece a job, on whatever someone has put on its
bed. It has no roll; what it works on is a material and a workpiece, chosen for each job.

### Safety first

Lasers start fires and blind people. BAM! is built around a person being at the machine:

- **Starting needs a person at the machine.** On a machine that starts jobs with its own button,
  BAM! sends the job ready, and the person presses the button. On one that does not, BAM!'s *Start*
  must be pressed by someone holding `lasers.run` (see [Access](access.md)).
- **The machine's interlocks are checked first.** BAM! asks the machine for them as it starts, and
  refuses while any reports a problem: the lid open, the safety key out, a flame alarm, or the
  exhaust off, on machines that report them. Resuming a paused job checks them again.
- **Nothing else starts a laser.** An assistant over MCP, a flow or an incoming hook never starts,
  frames or resumes one, whatever it holds; an API token does only with `lasers.run`. Hooks have no
  action that reaches a laser. An assistant may prepare a job, and watch it, but not start it.
- **The print queue never sends to a laser.** The spool, its pools and its batches never offer one,
  printing to one is refused, and a laser is never in a pool. A job is prepared and started from an
  asset's page or the template editor, one at a time.
- **Every job is in the history:** who prepared it and who started it, the asset and template, the
  material with its power and speed, where it went on the bed, and how it ended. A job done is in
  its asset's history as its label engraved, with the material.
- **Stopping is never harder than starting.** Anyone who may print pauses or cancels a job.

BAM! does not replace the machine's own safety: its emergency stop, its interlocks and its
manufacturer's instructions come first, and someone stays with the machine while it runs.

### Laser drivers

A laser's driver reports each model's **work area** (how far the head reaches across and down the
bed), its **materials**, whether it starts jobs with **its own button**, whether it can **frame** a
job, and which **interlocks** it reports. `GET /api/v1/printers/drivers` gives them, as `one_shot`.

The drivers for real machines come next: xTool, then GRBL diode lasers and Ruida CO2 controllers.
For tests there is `test-laser`, a simulated laser offered only where `[printers] test_laser` is
on (`BAM_PRINTERS_TEST_LASER`, see [Configuration](configuration.md)). It takes a `tcp` connection
it never dials, engraves nothing, and finishes a job a few seconds after starting it. Its models
are `button` (115 × 115 mm, started with its button) and `panel` (200 × 150 mm, started by BAM!),
and both frame and report every interlock.

A laser has no media, and is reached by this server, never through a relay.

### Materials

Each material has a name, and its settings:

- **Engraving:** power (a percentage of the laser's), speed (millimeters a second), passes, and
  lines per inch, which is the resolution the label is drawn at;
- **Cutting:** power, speed and passes, for a material the laser cuts; without them, a template's
  cut outline is left uncut, with a warning;
- **From the back:** for a material engraved through its face, such as clear acrylic. Its jobs are
  mirrored, left for right, and the person is told to lay the piece face down.

A laser starts with its driver's presets. An admin (`settings.manage`) tunes a preset for that
laser alone, or adds materials of its own, on the laser's page; putting a tuned preset back returns
it to the driver's settings. A laser has at most 100 materials.

### Engraving and cutting

- **Engraving** is the label rendered by the same renderer as every printer, 1-bit, at the
  material's lines per inch, so what engraves is what the editor shows. A template's resolution does
  not matter to a laser.
- **Cutting** follows the template's **cut outlines**: shapes marked *Cut* in the editor
  (`"cut": true`, see [Labels](labels.md)), such as a tag's outline and the hole it hangs by. They
  are taken from the template as vectors, curves followed within 0.02 mm, and cut after engraving.
  A laser does not engrave them; every other printer draws them as they are designed.
- **The fit check** for a laser asks only that the label fit its work area; inks other than black
  engrave as black does.

### Preparing a job

A job names the template, the asset (or none, for the template's placeholders), the material, and
the **workpiece**: its width and height, and where its top left corner lies on the bed, in
millimeters from the work area's top left corner. The label is centered on the workpiece, unless
the job says where its top left corner goes on it. The label must fit on the workpiece, and the
workpiece in the work area.

Preparing renders the label and holds it ready; nothing reaches the machine. A laser holds one
prepared job at a time: preparing another discards the one before. The prepared job lives on the
server that prepared it, so after a restart, or on another server sharing the database, it is
prepared again.

**The preview** draws the work area, the workpiece on it, the label in its place and the cut
outlines, so the person can check where it goes before anything moves.

**Framing**, where the machine supports it, has it trace the job's outline with its low-power
pointer, so the person can check the placement on the real piece. Framing is for a person at the
machine, as starting is, and does not check the interlocks: the lid is often open to see.

### Running a job

**Start** checks the interlocks, then sends the job: started at once, or ready for the machine's
button. While it runs, the laser's page shows its progress, *Pause* and *Cancel*, and the machine's
state and interlocks as the driver reports them; *Start* resumes a paused job. BAM! asks the machine
how the job is going every second until it ends, and takes it as failed if the machine has not
answered for ten minutes. A job's states are `prepared`, `ready` (waiting for the button),
`running`, `paused`, `done`, `cancelled`, `failed`, and `discarded` (prepared, never started, and
replaced).

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
| `GET /api/v1/printers/drivers` | `labels.print`, `settings.manage` or `templates.manage` | Every registered driver, with its models (and each model's DPI), media (each with the models that take it, when not all do) and options; for drivers that take custom stock, how wide each model's labels may be and how long continuous ones (`custom_stock`); for drivers of Bluetooth LE printers, their service and characteristics (`ble`) |
| `POST /api/v1/printers/{id}/fit` | template designers | Fit check without printing |
| `POST /api/v1/printers/{id}/test-print` | `labels.print` and `templates.manage` | Print one label of a possibly unsaved template |
| `POST /api/v1/printers/{id}/changes` | `settings.manage` | What a change would strand, without saving |
| `POST /api/v1/printers/{id}/print` | `labels.print` | Print labels directly |
| `/api/v1/label-stock` | `settings.manage`; listed to those who may see printers | Custom label stock |
| `/api/v1/printer-pools` | `settings.manage`; listed to those who may see printers | Printer pools |
| `GET /api/v1/templates` | `assets.read` | Includes `suits`, to those who may see printers |

### Laser API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/printers/{id}/materials` | `labels.print`, `settings.manage` or `templates.manage` | Its materials: each preset (`preset`, `tuned`) and its own |
| `PUT`, `DELETE /api/v1/printers/{id}/materials/{material}` | `settings.manage` | Tune a preset or save its own; delete its own, or put a preset back |
| `POST /api/v1/printers/{id}/jobs` | `labels.print` | Prepare a job: `template_id`, `asset_id`, `material`, `workpiece` (`width_mm`, `height_mm`, `x_mm`, `y_mm`) and `label_at` (`x`, `y`); answers the job, its `cuts` and `outline` on the bed, `warnings` and a `preview` |
| `GET /api/v1/printers/{id}/jobs`, `GET /api/v1/printers/{id}/jobs/{job}` | `labels.print` | Its history, newest first, and one job |
| `GET /api/v1/printers/{id}/jobs/{job}/preview` | `labels.print` | A prepared job's engraving, as a PNG |
| `POST /api/v1/printers/{id}/jobs/{job}/frame`, `…/start` | `lasers.run`, never over MCP, from a flow or from an incoming hook | Trace its outline; start it, send it ready, or resume it, with the interlocks checked first (`409` with the machine's `status` when they refuse) |
| `POST /api/v1/printers/{id}/jobs/{job}/pause`, `…/cancel` | `labels.print` or `lasers.run` | Pause it, or cancel it, prepared or under way |
| `GET /api/v1/printers/{id}/status` | `labels.print` | The machine's `status` (`state`, `progress`, `interlocks`), or why it is `unreachable`; its `model`; and its latest `job` |

### Display API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/displays`, `GET /api/v1/displays/{id}` | `labels.print`, `settings.manage` or `templates.manage` | Displays, what each `shown`: its template, asset, when and by whom, a refresh due, when it last fetched its image; and for one that pulls, its `pull_url` and `refresh_seconds` |
| `POST /api/v1/displays/{id}/refresh` | `labels.print` | Refresh it now, through the spool; `409` when it has been shown no asset's label |
| `GET`, `POST /api/v1/displays/{id}/tokens`, `DELETE /api/v1/displays/{id}/tokens/{token}` | `settings.manage` | Its tokens; making one answers its `secret` and `url`, once |
| `GET /api/v1/displays/{id}/image` | Its own token, or those who may see printers | Its image, with `ETag` and `Refresh` |
| `GET`, `PUT /api/v1/settings/displays` | Those who may see printers; `settings.manage` to set | The refresh window and default pull interval |
| `GET /api/setup`, `GET /api/display`, `POST /api/log` | A TRMNL, by its MAC address and token | [TRMNL](#trmnl)'s API |

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
| `082` | Displays: what each shows, its pull image and refresh due (`displays`), its tokens (`display_tokens`), and hooks' actions |

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

A display's driver is a `DisplayDriver` too: it names the connections its displays take, and its
plans encode the last label as the image file the display shows (`display.go`). The server itself
delivers to `pull` and `webhook` connections, keeping the image or posting it, and the `Transport`
the rest.
