# Labels

A label template describes a label: its size, its resolution, and the elements printed on it,
such as codes, text, images, icons and shapes. BAM! fills a template in for each asset and
renders it as SVG, PNG, PDF or a printer's own raster. Templates can be fixed to one label size,
or adaptive, laid out again for each label they print on.

Printers and bulk printing are covered in [Printing](printing.md). The template editor is
covered in [Template editor](template-editor.md).

## How a label is made

Every label passes through the same three stages, whichever format it ends up in:

```
template + asset ──► bind ──► layout (whole printer dots) ──► SVG │ PNG │ PDF │ printer raster
```

1. **Bind** resolves each element's `content_binding` against the asset, its field values and
   its generator keys.
2. **Layout** converts the template's physical units into dots at the template's DPI, snaps
   every barcode module to a whole number of dots, and sets text with the embedded fonts. Its
   output is a scene of rectangles, glyph outlines and images, measured in dots.
3. **Backends** draw that scene. Text is drawn as outlines rather than font references, so SVG,
   PDF and PNG agree at the template's DPI and never depend on the fonts installed where they
   are viewed. Every backend draws the elements in the order the template lists them, each over
   those before it.

## The template document

A template is stored in the `templates` table. `id`, `name`, `units`, `width`, `height` and
`dpi` are columns. `elements`, with `sizing` and `adapt` for an adaptive template, are kept in
`layout_json`, in drawing order. The API reads and writes it as one JSON document:

```json
{
  "id": "3f6c2a9e-8b1d-4e57-a0c4-91d2b7e5f803",
  "name": "Standard Die-Cut 29mm x 90mm",
  "units": "mm",
  "width": 90.0,
  "height": 29.0,
  "dpi": 300,
  "layering": "list",
  "elements": [
    {
      "type": "generator",
      "generator_type": "qr",
      "error_correction": "M",
      "x": 3.0,
      "y": 3.0,
      "width": 23.0,
      "height": 23.0,
      "content_binding": "{{base_url}}/a/{{asset.id}}"
    },
    {
      "type": "text",
      "x": 28.0,
      "y": 5.0,
      "width": 58.0,
      "height": 6.0,
      "font_size": 14,
      "font_weight": "bold",
      "content_binding": "{{asset.name}}"
    },
    {
      "type": "text",
      "x": 28.0,
      "y": 12.0,
      "width": 58.0,
      "height": 4.0,
      "font_size": 9,
      "font_family": "mono",
      "content_binding": "{{field.location}}"
    },
    {
      "type": "static_image",
      "x": 75.0,
      "y": 18.0,
      "width": 10.0,
      "height": 8.0,
      "image_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7"
    }
  ]
}
```

### IDs

A template's `id` is a UUID the server assigns when the template is created. An `id` sent with
`POST /api/v1/templates` is ignored, and the response carries the new one. A `PUT` may leave `id`
out, but cannot change it. Templates created by earlier versions keep the slugs their authors
chose.

### Layering

Elements are drawn in the order they are listed, each over those before it, so the first is at
the back. Templates are answered with `"layering": "list"`, which says so.

A document without `layering` was written when images were always drawn first, then text, then
codes, whatever the order. Such a document is drawn that way, its problems name its elements as
it lists them, and it is saved with its elements in that order. Migration `033` did the same to
every stored template, so each prints as it did.

With elements drawn in order, an image is drawn over what is beneath it, which shows through
where the image is transparent. In PDF, that transparency is kept as a soft mask.

### Canvas, units and DPI

- `units` is `mm` or `in`.
- `width` and `height` are positive.
- `dpi` is 150–600. Brother QL and DYMO LabelWriter printers print at 300, Rollo and most TSPL
  printers at 203.
- Positions and sizes use the template's units, with the origin at the top-left corner.
- Every element must lie inside the canvas.
- `font_size` is always in points.

### Orientation and feed

A template is portrait when taller than wide, landscape when wider.

Turning one a quarter turn swaps its width and height, and an adaptive template's ranges. It
turns each element about the label's center, its box keeping its size and its `rotation` a
quarter turn more, so the design reads the same, turned. Anchors follow the sides they were on,
and four turns give the template back. The editor's Portrait and Landscape switch and Turn left
and Turn right do this, as does `save_template` with `turn` over MCP.

`feed` says which way labels that fit a printer either way, such as squares or labels on a
continuous roll, go through it:

- `auto` (the default): as fits, not turned when that fits;
- `across`: the label's width across the roll;
- `along`: along the roll.

An adaptive template with `turnable` also takes labels turned a quarter turn from the sizes it
takes. They are laid out with its design turned to match, and it says so among its notices.

### Mirroring and rotating the printed label

Not every label is read from the front, or put on the way it was printed:

- `print_mirror` (`across`, `down` or `both`) prints the whole label mirror-image, codes and all.
  It is for a clear label stuck inside glass and read through it.
- `print_rotation`, in degrees from 0 up to 360, turns everything on the label clockwise about
  its center as it prints, after any mirroring. It is for labels a dispenser or applicator puts
  on turned, so they read level once on.

Both apply to every backend (SVG, PNG, PDF and the printer drivers) and to exports. The design
is kept, and edited, the right way round and level, and the editor draws it so. Its preview,
like the print screen's, has a switch between the label as printed and as applied, mirrored back
and turned.

What is turned off the label is cut off. Printing, the print screen and the spool warn of each
element that goes partly off, by its `name` or its place.

A print can mirror one job without changing the template, as with `feed`. `mirror` on
`POST /api/v1/printers/{id}/print`, or MCP's `print_labels`, is `across`, `down`, `both`, or
`none` to print a mirrored template the right way round. `save_template` sets `print_mirror`
(`none` to clear it) and `print_rotation`.

### Editor-only properties

Some properties are for the editor alone. None of them changes what prints.

| Property | On | Meaning |
|---|---|---|
| `guides` | Template | `{"x": [...], "y": [...]}`: lines across and down at places on the label, at most 50 each way, to line elements up against |
| `grid` | Template | The editor's grid (below) |
| `group` | Element | Names the group the editor moves, scales and turns it with |
| `locked` | Element | Keeps it from being selected on the canvas by accident |
| `name` | Element | What its layer is called: a plain string of at most 100 characters on one line |

An element's `name` is kept in the template's JSON as written, so it reads raw and survives a
template rebuilt from that JSON. The JSON preview gives it too.

A template's `grid` has lines `x` apart across and `y` apart down (more than 0, at most the
label's size), from `offset_x` and `offset_y` (from 0 up to a step). Each step is divided in
`subdivisions` (1 to 10), and the grid is drawn when `visible`. Without one, the editor keeps to
half a millimeter or a fiftieth of an inch, undrawn.

Guides and the grid turn with the template. A template laid out for a label (see
[Fixed and adaptive templates](#fixed-and-adaptive-templates)) leaves both behind.

### Mirroring an element

Any element's `mirror` flips it in its box, before it is turned: `across`, `down` or `both`. A
mirrored code is warned of when printing, because it does not scan from the side the label is
read from.

### Arranging elements over MCP

`save_template`'s `arrange_elements` does what the editor's buttons do:

- `align` lines elements up as they lie turned: `left`, `center`, `right`, `top`, `middle` or
  `bottom`, with each other or `to_label`.
- `distribute` spreads three or more out evenly: `across` or `down`.

### Rotating an element

Any element may set `rotation`, the degrees it is turned clockwise about the center of its box,
from 0 up to 360. `x`, `y`, `width` and `height` stay the box before it is turned, so turning
never changes its size. The element must lie inside the canvas once turned, and its box may
start before the canvas to do so.

How each kind is turned:

- Text is laid out in its box and then turned, as outlines.
- Images are resampled.
- A code turned a whole number of quarter turns is laid out on whole printer dots, as when
  upright.
- At other angles, a code's modules are drawn as turned polygons, filled with the same 50%
  coverage threshold as text in 1-bit output. The module size is chosen before turning, so
  `min_module_mm` holds.

Wherever a box is measured (adaptive layout, overlaps, a printer's printable area and edges), its
turned bounds are used. An adaptive element's anchors place its box, which is then turned. An
`alternative` takes its place turned as it is; alternatives have no `rotation` of their own.

## Content bindings

`content_binding` is literal text with `{{...}}` placeholders:

| Placeholder | Resolves to |
|---|---|
| `{{asset.id}}` | The asset's UUID |
| `{{asset.name}}` | The asset's name |
| `{{asset.code.<generator_type>}}` | The asset's assigned code for that generator, e.g. `{{asset.code.micro_qr}}` |
| `{{field.<key>}}` | The asset's value for that corpus field (empty when unset) |
| `{{base_url}}` | `server.base_url` from the configuration |
| `{{asset.categories}}` | The asset's categories, by name, separated by commas |
| `{{asset.location}}` | Where the asset is, through location links (see [Data model](data-model.md)): `Shelf 2 › Bin A3` |
| `{{asset.condition}}` | `Working`, `Needs maintenance` or `Out of service` |
| `{{stock.quantity}}`, `{{stock.unit}}` | Its stock counted by quantity, and the unit (empty when not counted) |
| `{{loan.borrower}}`, `{{loan.due}}` | Who has it on loan, by name ("Unnamed user" for someone without one; empty without `assets.track`, see [Access](access.md)), and the day it is due back as `YYYY-MM-DD` in UTC (empty when not lent) |
| `{{scan_url}}` | The asset's scan page, `{{base_url}}/scan/<asset ID>` (see [Scanning](scanning.md)): a QR code of it takes whoever scans the label there |
| `{{asset.icon}}` | The name of the icon the asset is drawn with (see [Icons](icons.md)), its own or its categories' (empty when none); as an `icon` element's `icon`, the icon itself |

Rules:

- Unknown placeholders and unknown field keys are rejected when the template is saved.
- A field deleted later resolves to an empty string.
- A `generator` element without a `content_binding` encodes `{{asset.code.<its generator_type>}}`,
  and rendering fails for an asset that has no code of that type.
- Resolved generator payloads go through the same validator as asset generator keys before they
  are encoded.

## Element types

### `generator`

A `generator` element draws a barcode or matrix code.

| Property | Values |
|---|---|
| `generator_type` | `qr`, `micro_qr` or `code128` |
| `error_correction` | Optional. QR: `L`, `M` (default), `Q`, `H`. Micro QR: `L` (default), `M`, `Q` |
| `margin` | Optional. The space kept clear around it, side by side: `top`, `right`, `bottom`, `left` |
| `margin_unit` | `modules` (the default), growing with the code, or `length`, in the template's units whatever the code holds |
| `align` | `left`, `center` or `right`; centered by default |
| `vertical_align` | `top`, `middle` or `bottom`; centered by default |
| `quiet_zone` | From before margins (below) |

A side left out of `margin` takes the standard quiet zone: QR 4 modules, Micro QR 2, Code 128 10
across and none above and below. Each side is from 0 to 20 modules or to half the box, and is
always whole printer dots. `quiet_zone` still means the same margin in modules on every side the
standard has, and cannot be set with `margin`.

The symbol is drawn at the largest whole number of dots per module that fits the box inside its
margins, placed as `align` and `vertical_align` say. Rendering fails if that is under 2 dots.
Code 128 bars fill the height inside the top and bottom margins.

A margin narrower than the standard quiet zone is allowed, as a code at the label's edge often
scans, but the preview warns of it.

### `text`

| Property | Values |
|---|---|
| `font_size` | Points |
| `font_weight` | `normal` or `bold` |
| `font_family` | `sans` or `mono` |
| `align` | `left`, `center` or `right` |
| `wrap` | Default `false` |
| `min_font_size` | Default 6 |
| `max_font_size` | With `fit: "fill"`; absent: 200 pt |
| `fit` | `"shrink"` (the default) or `"fill"` |
| `fill` | `black` (the default) or `white`, for text reversed out of a black shape |

Lines break at newlines, and between words when wrapping. The block is centered vertically in
its box.

How text fits its box:

- With `fit: "shrink"`, text that overflows its box shrinks in 0.5 pt steps down to
  `min_font_size`, then is cut short with an ellipsis.
- With `fit: "fill"`, it takes the largest size in 0.5 pt steps from `min_font_size` to
  `max_font_size` that fits its box, growing as well as shrinking. An adaptive template does not
  refuse it for its size.

The JSON preview gives the size each text was drawn at (`drawn_font_size`). The embedded Go fonts
cover Latin, Greek and Cyrillic; other scripts render as empty boxes.

### `static_image`

A `static_image` element's `image_id` names an uploaded PNG or JPEG, scaled to fit the box with
its aspect ratio kept.

Uploads are recorded in the `images` table and stored as `images/<id>` in the file store (see
[Configuration](configuration.md)), which in a directory is `storage.local_path/images/<id>`.
`GET /api/v1/images/{id}` returns the metadata and `/content` the image itself.

SVG images are not placed as images, since there is no dependable pure-Go SVG rasterizer. SVG
artwork is imported as shapes instead, which print sharp at any size (see
[Importing SVG artwork](#importing-svg-artwork)).

### `icon`

An `icon` element draws an icon (see [Icons](icons.md)), placed in its box as an image is.

`icon` is either `{{asset.icon}}`, each asset's own icon or its categories', or an icon by name,
which must be one the build draws when the template is saved. It is drawn as large as fits its
box, its proportions kept, at the dots it prints at. It is centered, or placed across its box as
`align` says (`left`, `center` or `right`); vertically centered; and flipped by `mirror` as an
image is.

| Property | Values |
|---|---|
| `shading` | `shaded` (the default) or `outline` |
| `state` | `true` draws the asset's icon in its state; with `{{asset.icon}}` only |
| `rendition` | `label` (the default) or `screen`; with `{{asset.icon}}` only |

With `shaded`, each palette token prints as the gray of its light color, and the `category`
token as the asset's category's color (or the palette's `ink-muted`), dithered in 1-bit output.
With `outline`, only its strokes print, in black, which reads better on a small label. A stroke
an icon keeps the width of prints as wide as it is at the icon's own size, enlarged with the rest
of it, so that a hairline is still seen at a printer's resolution.

`state: true` draws the asset's icon in its state as the label is printed. A label outlives the
state it was printed in, so it is drawn plain unless asked. Label data carries it as
`state_icon`.

`rendition` is which of the asset's icons it draws. `label` is its label icon, which is its
screen icon until it has a label icon. Existing templates print the screen icon until a label
icon exists, then the label one.

Edge cases:

- An asset with no icon leaves the box blank.
- A name nothing draws any more prints the unknown icon.
- Previewed without an asset, the unknown icon stands for each asset's.
- Like an image, an icon with no room on an adaptive template's label is left off.

The server draws it from the current build and the palette (`render.IconSource`). The editor's
live renderer is given the same as `setIcons`, and draws it dot for dot as the server does
(`live_parity.json`).

### `shape`

A `shape` element's `shape` is one of:

- `rectangle`, optionally with `corner_radius`, up to half its shorter side;
- `ellipse`;
- `line`, from its box's top left to its bottom right, or bottom left to top right with `flip`.
  Its box may have no height or no width, for a level or upright line;
- `path`: `d`, SVG path data whose points are fractions of its box, at most 2,000 segments, and
  20,000 across a template's paths. It is filled by `fill_rule`, `nonzero` or `evenodd`;
- `polygon`: `points`, 3 to 256 of them, or 2 with `"closed": false`. Each is a pair of
  fractions of its box across and down, so it scales with the box. It is closed unless `closed`
  is false.

| Property | Values |
|---|---|
| `stroke` | `black` (the default), `white` or `none` |
| `stroke_width` | In the template's units; 0.25 mm by default |
| `fill` | `black`, `white` or `none` (the default); a line or open polygon has no fill |

The fill is drawn, then the stroke. A rectangle's or ellipse's stroke lies inside its box; a
line's or polygon's is centered on it. A stroke thinner than one dot is drawn one dot wide, and
the JSON preview warns of it. White paints over whatever is beneath it.

A rectangle with square corners and a level or upright line are drawn on whole dots, as code
modules are. The rest are outlines, filled with the 50% coverage threshold in 1-bit output.

On an adaptive template, a shape left without room on the label is left off by default. Its
corners round in proportion to its box, and its stroke keeps its width.

#### Importing SVG artwork

Artwork is imported with `POST /api/v1/templates/import-svg` (`templates.manage`). It takes
`{"svg"}`, an SVG file's text of at most 1 MB, and optionally a `box` on the template. It answers
with:

- its shapes, fitted into the box keeping their proportions and centered, or in the artwork's
  own units and size;
- `skipped`, what was left out.

Nothing is stored until the template is saved.

What is converted:

- Each `path`, `rect`, `circle`, `ellipse`, `line`, `polyline` and `polygon`, through `g` and
  `transform` and the `viewBox`, becomes a `path` shape.
- Its paint comes from `fill`, `stroke`, `stroke-width`, `fill-rule` and the opacities, as
  attributes or inline styles, inherited down groups.
- Colors become black or white by how light they look, and paint mostly see-through becomes
  none.
- Gradients and patterns are drawn in their fallback color, or black.

What is left out and listed: text (which needs the artwork's fonts), images, scripts, links to
other files, `use` elements, style sheets, filters, masks and clipping.

The file is read as XML with no document type or entities, nested at most 64 deep, with at most
10,000 elements.

## Fixed and adaptive templates

A template is either:

- **fixed** (no `sizing`, or `"fixed"`): printed only at its own size and DPI, dot for dot as
  previewed; or
- **adaptive** (`"sizing": "adaptive"`): designed at its own size, it is laid out again for the
  size of each label it prints on and rendered at the printer's DPI.

### The sizes an adaptive template takes

`adapt` gives the sizes it takes, `min_width`–`max_width` by `min_height`–`max_height` in its
units, which must include its own size. It also says how long its labels are on a continuous
roll, whose width alone is given (`continuous_length`): `proportional` to the width (the
default), or `fixed` as designed.

On a printer, its width or its height runs across the roll, whichever leaves it nearest its own
shape and fits.

### Anchors

Each element says how it follows the label across and down (`anchor_x`, `anchor_y`):

- `scale`: in proportion (the default);
- `start` or `end`: at its distance from that edge;
- `both`: at its distance from each edge, stretching;
- `center`: at its distance from the middle.

Text scales its font with its box (by the lesser of its two scales) and still shrinks to fit.
Codes take the largest whole number of dots per module their box allows, as always, and images
keep their proportions. Laid out (`render.Adapt`), it is an ordinary fixed template, so
rendering, exports and the printers are unchanged.

### Size limits and alternatives

An element of an adaptive template may set `min_size` and `max_size`, bounding each side of its
box in the template's units. Laid out larger, it is centered where its box would be at its
maximum.

A code may set `min_module_mm`, the smallest module it is drawn with for the scanners in use, on
fixed templates too. Two dots at the printer's DPI is always the least.

An element does not fit a label when it has no room, is under its minimum size, is text below its
minimum, or is a code too big for its box with the longest codes assigned. It then does as
`when_too_small` says:

- `refuse` the label (the default for codes and text);
- `hide`, leaving it off (the default for images);
- `alternative`, giving way to its `alternative`: a code or text element laid out in its box,
  such as a Micro QR code of the asset's `micro_qr` code, or the code as text. An alternative
  that does not fit either refuses.

What was left off or replaced is said in the print answer's and the spool's warnings.

### When a label is refused

A label is refused, saying which element, when:

- an element would run off it or have no room;
- text would fall below its `min_font_size`;
- two elements that do not overlap as designed would;
- a code would not fit its box at the printer's DPI with the longest code assigned with its
  generator, or the longest value of a field it uses, among active assets.

Downloads and previews of an adaptive template use its own size.

### In the editor

The template editor switches a template between fixed and adaptive, and sets its range and each
element's anchors, limits and alternative. It shows the template laid out on each printer as
loaded, and at any size typed in, with the sizes it takes in its own shape.

### Adaptive layout API

| Route | Does |
|---|---|
| `POST /api/v1/templates/adapt` | Needs `templates.manage`. Lays a template out at a `width`, `height` and `dpi`, or as a printer would (`printer_id`). Answers with it laid out and its `notices`, or the `problem`. |
| `POST /api/v1/templates/range` | Works out the sizes it takes in its own shape at a `dpi` (its own by default), from `smallest` to `largest`, and what stops it going further (`smaller`, `larger`), when its declared range does not. |

Both use the longest codes and field values assigned among active assets.

One asset's code can still be too long for a size the template otherwise takes. That label alone
is not produced. When its template came from its categories, it moves to the first of theirs
after it that suits the printer and holds it, to print next time (see [Printing](printing.md)).

## Generators

Generators encode an asset's codes as barcodes and matrix codes. They implement this interface:

```go
// Symbol is an encoded barcode as a grid of modules. Linear symbologies
// (Code 128) have a single row, which layout stretches to the element height.
type Symbol struct {
    Cols, Rows int
    QuietZone  int    // standard quiet zone, in modules
    Dark       []bool // row-major, len(Dark) == Cols*Rows
}

type Generator interface {
    Type() string
    // Validate applies the asset key rules; every payload it accepts encodes
    // at the lowest error correction level.
    Validate(payload string) error
    // ErrorCorrectionLevels lists accepted levels, default first (none for Code 128).
    ErrorCorrectionLevels() []string
    // Encode validates, then encodes; "" selects the default level.
    Encode(payload string, errorCorrection string) (Symbol, error)
}
```

Generators return modules, not images. Sizing and drawing belong to layout, which is what keeps
modules on whole dots.

### QR Code

QR Code (`qr`) is Model 2, versions 1–40, error correction L/M/Q/H. A payload is encoded as a
single segment in the most compact of numeric, alphanumeric or byte mode. UTF-8 is written as
plain bytes with no ECI header, which is what phone scanners expect. The smallest version that
fits is used, and the mask is chosen by the standard's penalty rules.

### Micro QR Code

Micro QR Code (`micro_qr`) uses versions M2–M4 in numeric or alphanumeric mode. It uses the
smallest version that fits at the requested error correction, with the mask chosen by the Micro
QR evaluation rule.

M1 is never used: it detects errors but cannot correct them, so a scuffed label would stop
scanning. Asset keys are validated against M4-L capacity, so a template asking for `M` or `Q` can
reject a long code at render time.

### Code 128

Code 128 (`code128`) uses code sets B and C with the modulo-103 check symbol. Following ISO/IEC
15417 Annex E, a symbol starts in C when it begins with four or more digits, and switches to C
for a run of four digits at the end or six elsewhere.

### Implementation

All three encoders are written in-house as standalone packages (`internal/generators/qr`,
`microqr`, `code128`) that know nothing of the registry. The QR flavors share private helpers for
Reed–Solomon, bit packing and matrix layout. Go QR libraries exist, but none implements Micro QR,
and owning all three keeps output deterministic for golden tests.

## Output formats

| Format | Use and behavior |
|---|---|
| SVG | The interchange format for the frontend editor preview. Adjacent dark modules merge into rectangles; text is glyph outlines. |
| PNG | Rendered at the template's DPI, as 8-bit grayscale for previews or 1-bit black and white for printing. In 1-bit output, text is thresholded at 50% coverage and images are Floyd–Steinberg dithered. Barcode modules are never anti-aliased. |
| PDF | One page per label, sized exactly to the label, with vector barcodes and text, and images embedded as Flate-compressed bitmaps. Output is byte-for-byte deterministic: no timestamps or random document IDs. |

Batch exports produce a multi-page PDF or a ZIP of PNGs named by asset ID (see
[Printing](printing.md)).
