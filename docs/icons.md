# Icons

A category may carry an icon, an isometric drawing written in
[isolex](https://forge.axonibyte.com/axonibyte/isolex). Assets are drawn with the icon of their
nearest category that has one, or with an icon of their own. Icons show in lists, on asset and
scan pages, on labels, in scenes of what a location holds, and as downloadable images.

Icons are a feature, not a requirement. bam ships no icons except `bam-unknown`, a box with a
question mark, which is drawn in place of an icon that the lexicon does not have. With no icons
anywhere, nothing shows one (migration `034`).

## How icons fit together

The categories' sources, the library's sources and the palette make one **lexicon**, compiled as
a whole. The category tree is its inheritance: a subcategory's icon can `extends` its parent's
and change one part.

Since isolex 0.4, a part can be turned by quarter-turns, mirrored, rotated, skewed or laid on a
face of a box. An icon can also carry words, set in a font that the lexicon defines like any
other definition. bam ships no fonts. `isolex font import` converts a TrueType, OpenType or WOFF
font whose license allows it, and the converted file goes in the library.

### Category sources

A category's source (`category_icons`) draws its icon, named by `icon_name`. A category has one
source for each **rendition** it has an icon for (see [Renditions](#renditions)).

Icon names follow these rules:

- A name is 1 to 64 letters, digits, `-`, `_` and `.`, and does not start with `.`.
- Names starting `bam-` are bam's own.
- A name belongs to one icon, whatever its rendition.
- The first save sets the name. After that it changes only by renaming, because other icons
  refer to it by name.

In a build, the source is the file `categories/<icon_name>.ilx`.

### The library

The library (`icon_library`) holds sources that no one category owns, such as bases, fragments,
axes and fonts, at paths such as `shared/racks.ilx`. In a build they are `library/<path>`.

### Drafts and published sources

Every change is compiled together with everything else before it is kept:

- A source that compiles clean, and draws the category's icon under its name, is **published**.
  Its build becomes the one icons are drawn from.
- A source that doesn't is kept as a **draft**, with its errors, wherever in the lexicon they
  are. The published version and the build stay as they were, so one bad edit never breaks an
  icon.

A draft is fixed by saving again, or discarded. Every published version is kept
(`icon_revisions`), and publishing a category's icon records a `category.icon_changed` event.

Some changes have no draft to fall back on: deleting or renaming an icon or a library file, and
changing the palette or the settings. Such a change is refused if the lexicon wouldn't compile
after it, with the errors that say why (for example, the icon that `extends` the one being
deleted).

### The palette and settings

The palette (`icon_palette`) is the vocabulary. Its tokens are exactly what icons may paint
with, plus `category`, which paints in the category's own color. Each token has a light and a
dark color, written `#rgb`, `#rrggbb` or `#rrggbbaa`. bam seeds the tokens isolex paints with by
default, in its zinc and red.

The **settings** (`icon_settings`) hold the stroke widths allowed, the tokens only a `grant` may
paint with, and the compiler's limits. bam writes the `lexicon {}` block itself, from the
palette and the settings; no source declares one.

### The build

The build (`icon_build`) is the last build that compiled clean, with its IR, its warnings and a
hash of its inputs, the isolex version among them.

A change compiles outside any transaction. It is kept under the `icons` lock only if nothing it
was compiled against has changed meanwhile; otherwise it compiles again.

Deleting a category deletes its icon and its history. It is refused (`409`, with the errors) if
another icon still uses that icon, unless the server cannot compile icons, when nothing can
change them anyway.

## Using icons in the web app

### On a category's page

The **Icon** section of a category's page (`CategoryIconSection`) shows the icon the category
shows. That is its own icon, or the nearest category above it with one, inherited, painted in
the category's own color.

Those who may manage categories can draw a category its own icon by extending what it inherits,
from a box, or from another category's icon. Its name is suggested from the category's.

The source editor checks the source a moment after typing stops, through `…/icon/check`:

- Problems in this source are marked in the editor. Those it causes in other icons are listed
  with links to their categories.
- The preview shows the icon light and dark, large and as in lists, its variants, and the icons
  of its subcategories, all from the check, so an edit is seen where it spreads.
- A source that does not compile shows the published icon, saying so.

Saving publishes the source, or keeps it as a draft with its problems, which can be discarded.
The history loads any published version back into the editor. Renaming and deleting show what
still uses the icon when they are refused.

### The Icons page

The Icons page (`/icons`, under Catalog, for those who manage categories or icons) shows:

- the build, with export and import;
- what needs attention: category and library drafts with problems, the build's warnings, and the
  icons assets have as their own that nothing draws, each linked to the assets (`/assets?icon=`);
- every icon, by isolex category, searchable, each with the category it belongs to and as an SVG
  or PNG to download;
- the library, written in the source editor;
- the palette as swatches, light and dark, reordered, added to and removed from;
- the settings: stroke widths, reserved colors, and the limits, each empty for the compiler's
  default or set to none;
- asset states: the variant each state is drawn with, and the icons the build draws in it.

A refusal lists the icons it would break, as links. The page credits isolex, its authors
Axonibyte Innovations, LLC, and its license.

### Elsewhere in the interface

Icons show only where there are some (`lib/iconset`, read once a page draws one, never signed
out):

- A category's pill shows its icon, on the page's own background, so what paints in the
  category's color stays seen.
- Lists show an asset by its first photo, with its icon as a chip in the corner, or by its icon
  alone.
- An asset's page and its scan page show its icon beside its name. Its details say where the
  icon comes from.
- The asset form's **Icon** row shows what the asset is drawn with and from where.
  **Choose another…** opens a picker of every icon the build draws, searched by name or
  description. **Use the category's** drops its own.

### How pages draw icons

Icons are drawn in the page by `IsoIcon`, from the build's IR. Every token is painted with a CSS
property, `--iso-<token>`, from the palette (`lib/isolex/palette`), so dark mode needs no
redraw.

Its shapes match isolex's own SVG, attribute for attribute, for isolex 0.3.1's sample and for
isolex 0.4.0's sample, transforms and text. It draws their IR version 2 groups through their
matrices, cut to their clips, with strokes that keep their width. A clip path is named for the
drawing, so an icon shown many times on one page, as in a list or a scene, cuts each copy to its
own. The help topic **Icons** explains it all.

## Assets' icons

An asset is drawn with the icon of its nearest category that has one, chosen as its templates
are (see [Scanning](scanning.md)): the closest category up from its own, then the deepest, the
one with fewest subcategories, the oldest. The `category` token paints in its own deepest
category's color.

### An asset's own icon

An asset can have an icon of its own (`assets.icon_name`, migration `036`), drawn in place of its
categories'. It may be any name the build draws, variants included (`switch-down`), but no base
or fragment.

The icon is checked against the build when it is set, and is not tied to the build after that.
If the build stops drawing it, the asset is drawn with the unknown icon and says so (`missing`),
never quietly with its categories'. An edit that leaves it as it is keeps it.

Renaming a category's icon carries assets along: those naming it, and those naming one of its
variants that the renamed build draws under the new name. A name that is some category's own
icon never moves.

- A copy (`GET …/copy`) has the original's icon, while the build still draws it.
- The asset's history records a change (`asset.edited`, `"what": "icon"`).
- The CSV has an `icon` column when any asset has one (see [CSV](csv.md)): blank for its
  categories', and checked as an edit is.

### Parameters from fields

An asset's fields can set its icon's params (`icon_param_bindings`, migration `038`). A
category's own icon may bind any of its params, its own or inherited, to a number field, on the
category's page under **Parameters**: `u` to "Rack units", say. An asset drawn with that icon is
then drawn with each param set to its value of the field.

Params are late bound, so the geometry of the icons it extends sees them. A server with 2 in
Rack units is drawn 2 units high.

#### Wrappers

Each build generates, for each icon with bindings, a wrapper for each distinct set of values
that the active assets that may be drawn with it have. Those assets are the ones with it as
their own icon, and those in a category it is drawn for. There are at most 256 wrappers an icon.
A wrapper looks like this, with a `-` in a value written `m`:

```
icon "bam--server--u_2" extends="server" { param u 2 }
```

A wrapper has the icon's variants too. The wrappers are part of the build's inputs, so a value
changed makes a new build. They are left out of the export and of every list of icons, and no
asset can be given one as its own.

#### When wrappers are built

An asset with no number in a bound field is drawn with the param's default; so is one whose
wrapper is not yet built.

A change to an asset through `POST` or `PUT /api/v1/assets` builds before it answers. Any other
change through the API is followed, half a second after the last, by a build if the inputs
changed.

A wrapper never stops a build:

- One with errors on its own line is left out.
- If a value only breaks the icon it extends (`1 / (u - 3)` for 3), the values that do are
  found by halving them, and a warning says how many were left out.

Their assets are drawn plain. `resolved_icon` then has `bound`, the wrapper, and `params`, and
`base` is the icon by its name. Labels draw the wrapper; `{{asset.icon}}` as text prints the
icon's name.

### Texts

An asset's own texts can be written on its icon (`icon_text_bindings`, migration `040`). A text
param, `param label (text)"?"`, is bound under **Parameters** to one of:

- the asset's name;
- its first code (its QR code, else its Micro QR, else its Code 128, else whichever it has);
- a field's value.

`PUT …/icon/params` takes `name` or `code` in place of a field's ID. A binding is checked against
the icon: a param it has, a text param to a text and a number param to a number field.

#### How texts are compiled

Texts differ asset by asset, so they get no wrappers. Each asset is compiled alone with its
texts, as isolex's `compile_one` compiles one icon, from the files the current build was made
from. Builds keep those files (`icon_build.inputs`).

Every read of assets compiles those it holds in one call to isolex's `compile_many`, which
parses the lexicon and builds each font once. What is drawn is kept in memory, at most 4,096,
keyed by the build, the icon and the texts, so it is compiled again only when one of them
changes.

Measured under wazero with Noto Sans (102 glyphs) and 10 to 200 icons, one icon alone takes 25
to 48 ms, and in one call, 1 to 3 ms each. So 300 assets take 0.3 to 1 s the first time they are
read.

#### Where texts are drawn

`resolved_icon` then has `texts`, by param, and `drawn`: the icon drawn with them, by the name
it is drawn under, and, when that shows a state, without it too. Pages, images (`?asset=`),
labels (through `drawn_icons` in their data, so the live preview draws them too), scenes and
MCP's `texts` use it in place of the build's.

An asset is drawn with the param's default when:

- it has no value there;
- its icon does not draw with its texts, or cannot be drawn now;
- its build was made before builds kept their files, until it is made again, which a server
  does when it starts.

### States

An asset's state shows in its icon. These states count, worst first:

1. `out_of_service`
2. `needs_maintenance`
3. `overdue`
4. `on_loan`
5. `out_of_stock`
6. `low_stock` (running low)

An asset in any of them is drawn as its icon's variant for the worst of them that the build
draws: `<icon>-<variant>`, an isolex axis variant, its own icon's as well as its categories'.
With none drawn, it is drawn plain, so nothing changes until an icon has variants.

#### Choosing variants

The variants are chosen on the Icons page under **Asset states** (`icon_settings.states`,
migration `037`). Unless chosen otherwise they are `down`, `maintenance`, `overdue`, `lent`,
`empty` and `low`, and any can be none. An icon has them by opting into an axis with them:
`axes "state"`.

**Start the state badges**, in the library, starts `shared/states.ilx` from the server's
`starter`. That file holds the `state` axis, with a variant for each state's that puts a badge
in the icon's top right corner, and the badges. They are drawn at a stroke width the settings
allow and grant the reserved colors they paint with.

#### Where states show

The state is drawn wherever the asset's icon is: lists, its page (which says so), scans, images
(`?asset=`) and MCP. A label shows it only when asked (see [Labels](labels.md)). A copy takes the
plain icon. Pages show the state as they are loaded.

## Renditions

A rendition (migration `047`) is the form an icon takes for where it is drawn:

- `screen` is every icon on screen, and what every icon was before renditions.
- `label` is drawn on labels.

An icon that reads well on screen, shaded faces in the palette's colors, often prints badly on a
1-bit thermal printer. There, faces dither into noise and the `category` token becomes an
unpredictable gray; a label reads best as flat line art. Adding a rendition is a change to bam,
not a setting.

### Icons per rendition

A category has an icon for each rendition. Each has its own source, name, versions, draft and
param bindings, and is saved, checked, renamed and deleted alone (`category_icons`,
`icon_revisions`, `icon_param_bindings` and `icon_text_bindings` are keyed by category and
rendition). By convention a label icon is named `<icon>-label`. A category may have a label icon
and no screen icon.

Label icons are resolved by the same rule as screen icons. An asset's label icon is that of the
nearest category up from its categories with a label icon, ties broken as for templates. With
none anywhere, it is its screen icon. So nothing changes until a label icon exists, and giving
one to a parent gives it to every subcategory without one.

### An asset's own icon per rendition

An asset's own icon is for one rendition (`assets.label_icon_name` beside `icon_name`). An asset
with its own screen icon and no label one has its labels drawn with its own screen icon, not its
categories' label icon, since it was made to look different on purpose.

In order, an asset's label icon is:

1. its own label icon;
2. its own screen icon;
3. its categories' label icon;
4. their screen icon.

### Which rendition is drawn where

Lists, asset and scan pages, the Icons page, scenes and images draw `screen`. A label template's
icon element draws `{{asset.icon}}` in its `rendition` (see [Labels](labels.md)), `label` unless
it asks for `screen`. An icon by name is the one named.

States and params carry over. A label icon has its own variants (`crate-label-down`, with
`axes "state"`), and an asset in a state has its label drawn as its label icon's variant when
asked. Params bind per rendition, each being its own icon, and wrappers are built for the icons
each asset may be drawn with in either.

## Scenes

A scene is what is in a location, drawn where it stands (`internal/scene`). An asset's scene is
the assets in it directly, through location links. Each is drawn with its icon (its wrapper and
state variant included, so a 2U server takes 2 units) or, with none in three dimensions, as a
plain box of one tile.

### Layout

Each thing takes whole tiles, its footprint rounded up. One someone placed stands where they
placed it. The rest are packed around those in rows across the floor, by name and then ID, never
into the space a placed one stands over:

- The rows are as wide as the location's own icon's footprint, or near square for one with none,
  never narrower than the widest thing.
- The floor is as deep as that footprint or the rows take.
- The floor grows to take anything placed beyond it.

Things may overlap: each lists those it shares more than a face with.

Things are drawn back to front. Each is drawn after every one it covers on the page and stands
in front of (a topological sort of their boxes); the rest are ordered by the middle of their
boxes, then ID. Positions are isolex's projection, `P(x, y, z)`, so each icon is drawn
translated to where it stands. Golden tests hold the layouts.

### Placing things

A thing's place (`asset_placements`, migration `039`) is kept on its location link, as x, y and
z in tiles from the floor's corner. So moving it to another location leaves it to be packed
there.

### The scene page

The scene page (`/assets/{id}/scene`), reached from **See inside** on an asset that holds
things, shows the floor's grid and what stands on it, in the palette's colors.

- The wheel or the buttons zoom, from a tenth to four times its size. A drag pans. **Fit**
  shows it whole.
- Hovering says what each thing is. Choosing one, by a click or the keyboard, says its name,
  code and condition, with **Open** and, for one that holds things, **Look inside**.
- A double click (or Enter) goes inside one that holds things, and opens the rest.
- A breadcrumb leads back out.
- **Show in place**, beside where an asset is, opens its location's scene with it outlined in
  amber and the rest muted.
- Whatever overlaps something is outlined in red dashes, and, chosen, says what it overlaps.

**Arrange**, for one who may change something in the scene, lets things be moved:

- Dragging one moves it across the floor a tile at a time. The chosen one's handle lifts it, a
  quarter of a tile at a time or onto the top of what is under it. Dragging the floor still
  pans.
- The arrow keys move the chosen one a tile, and Page Up and Page Down lift and lower it.
- Each move is saved when the drag ends, or the keys stop.
- **Put back** packs it with the rest again.
- The panel says where it stands.

**Save as SVG** and **Save as PNG** (1,024 pixels high) download the scene as the server draws
it, in the light theme, with what is shown in place outlined.

## Icons as images

`GET /api/v1/icons/image` (`assets.read`) draws one of:

- `name`;
- `category`, the icon it is drawn with, its own or inherited;
- `asset`, its own or its categories', as its scopes allow it to be read.

| Parameter | Values |
|---|---|
| `format` | `svg` (the default), drawn as isolex draws it, or `png`, drawn by bam's own rasterizer |
| `theme` | `light` (the default) or `dark`, the palette's colors for either |
| `height` | 1 to 1,024 pixels, 64 unless given; the width is in proportion |
| `color` | What the `category` token paints in. Unless given: the category's own color (for a name, the category whose icon it is; for an asset, as `resolved_icon` says), or the palette's `ink-muted` for a category with none |
| `rendition` | For a category's or an asset's icon (see [Renditions](#renditions)) |

The PNG rasterizer (`internal/isolex/raster`) fills by the nonzero rule, and strokes with miter
joins (beveled past SVG's limit of 4), butt caps and dashes, at each shape's opacity, on a
transparent background. A group is drawn through its matrix and cut to its clip, its strokes
scaling with it as SVG's do. A group that keeps its strokes' width draws them that many pixels
wide at any size, as Chromium draws `vector-effect="non-scaling-stroke"`.

A name the build does not draw is drawn as the unknown icon, and `X-Icon-Missing` names it. A
category or asset with no icon is `404`. The image's `ETag` follows the build and what is drawn,
so `If-None-Match` answers `304` until either changes. A Playwright test holds the PNG to
Chromium's drawing of the SVG, light and dark, pixel for pixel near enough.

## API

### Reading icons

Reading needs `assets.read`:

| Route | Gives |
|---|---|
| `GET /api/v1/icons` | Whether this server can compile icons (`available`), which isolex it pins, and the current build: its hash, warnings, and its icons by name and isolex category. Also `category_icons`, `unknown`, `missing_icons` and `rendition_icons` (below). |
| `GET /api/v1/icons/ir` | That build's IR, which pages draw icons from. Its `ETag` is the build's hash, so `If-None-Match` answers `304` until a build lands. |
| `GET /api/v1/icons/palette` | The palette. |
| `GET /api/v1/icons/settings` | The settings. |
| `GET /api/v1/icons/states` | The asset states, worst first, the variant each is drawn with (`variants`), bam's defaults, and `starter`, a library file drawing them. |
| `GET /api/v1/categories/{id}/icon/params` | The params of a category's icon bound to what assets have: `[{"param", "source", "field_id", "field_key", "field_label"}]`, `source` being `field`, `name` or `code`. |
| `GET /api/v1/category-icons` | The category sources and their published versions. |
| `GET /api/v1/categories/{id}/icon` and `/icon/revisions` | A category's source and its published versions. |
| `GET /api/v1/icon-library` | The library sources. |
| `GET /api/v1/icon-library/{id}` and `/revisions` | A library source and its published versions. |

`GET /api/v1/icons` also gives:

- `category_icons`: the icon each category is drawn with, its own or inherited, by name;
- `unknown`: the unknown icon's IR, for pages to draw a name the build does not;
- `missing_icons`: the names active assets have as their own that the build does not draw, each
  with how many (`[{"name", "assets"}]`);
- `rendition_icons`: the icon each category is drawn with for each rendition, a category with no
  label icon given its screen icon.

`GET /api/v1/assets?icon={name}` lists the assets with an icon as their own, as `search_assets`
does with `icon`.

### A category's icon

A category's own icon needs `categories.manage`. Each of these routes takes `?rendition=`
(`screen` unless given, `400` for one that is not a rendition):

| Route | Does |
|---|---|
| `PUT /api/v1/categories/{id}/icon` | Takes `{"icon_name", "source"}` and answers `{"icon", "save"}`. In `save`, `published` says whether the source was published, and `errors` and `warnings` give the compiler's diagnostics as `{"file", "line", "col", "message"}`. A draft is still a `200`. |
| `POST …/icon/check` | Compiles the same way and keeps nothing, answering the errors, the warnings and, if clean, the IR, for checking as one types. |
| `POST …/icon/rename` | Takes `{"icon_name"}`. |
| `DELETE …/icon` | Deletes the icon. |
| `DELETE …/icon/draft` | Discards the draft. |
| `PUT …/icon/params` | Takes `{"params": {param: field_id}}`, replaces the bindings, each to a number field (`400` otherwise), and builds the icons with them. Takes `name` or `code` in place of a field's ID for a text param. |

### The library, palette, settings and states

What every icon is drawn with needs `icons.manage`:

| Route | Does |
|---|---|
| `POST /api/v1/icon-library` | Takes `{"path", "source"}` (`201`). |
| `PUT /api/v1/icon-library/{id}` | Takes `{"source"}`. |
| `POST /api/v1/icon-library/{id}/rename` | Takes `{"path"}`. |
| `DELETE /api/v1/icon-library/{id}` and `…/draft` | Deletes a library file, or its draft. |
| `PUT /api/v1/icons/palette` | Takes `{"palette": [{"token", "light", "dark"}]}`. |
| `PUT /api/v1/icons/settings` | Takes `{"stroke_widths", "reserved", "limits"}`. |
| `PUT /api/v1/icons/states` | Takes `{"variants": {state: variant}}`; a state left out, or `""`, is drawn with none. It compiles nothing. |

### Export and import

`GET /api/v1/icons/export` (`assets.read`) is a zip of what is published as an isolex library
folder (see [Backups](backups.md)), laid out as a build compiles it:

- `lexicon.ilx`: the block bam writes, and the palette as a `palette` block, so the folder
  carries its own colors;
- `library/`;
- `categories/`;
- `icons.json` beside them.

isolex studio opens it as it is, and a package of the icons alone is the same zip. The isolex
compiler checks it as it is (`isolex check`), and the same icons always make the same bytes.

A palette block holds `#rgb` and `#rrggbb` colors. So a token whose colors have transparency is
left out of it, with a comment saying so. `category` is colored by each category, so it is never
in it.

`POST /api/v1/icon-library/import` (`icons.manage`, multipart, the zip as `file`, up to 16 MiB)
takes the `.ilx` files of a zip into the library at their paths. New ones are added and
existing ones replaced, as one change: compiled once and published together, or refused whole
(`409`, with the errors) if the icons would not compile with them.

- A folder holding the whole archive is not part of the paths, nor is `library/`, so an export
  comes back in as it went out.
- Its `categories/`, `lexicon.ilx` and `icons.json`, and anything not `.ilx`, are skipped and
  listed.
- An archive may hold 4,096 sources, expanding to 64 MiB at most, whatever its entries claim.

**Admin › Import & export** takes the whole folder back, categories' icons and palette included
(see [Backups](backups.md)). Neither the export nor the import is reachable over MCP, which reads
and saves sources one at a time.

### Refusals, failures and limits

| Case | Answer |
|---|---|
| A change refused because the icons wouldn't compile | `409`, with the errors in `details` |
| A compile that ran out of time or memory | `422` |
| Any change on a server built without the compiler | `503` |
| A request carrying a source | Up to 4 MiB |
| A source | Up to 262,144 characters |

### Icons on assets

An asset has:

- `icon_name`, null for its categories';
- `resolved_icon`: `{"name", "from"}` (`asset` or `category`), `source_category_id` for a
  category's, `missing`, and `color`; and, drawn in a state, `base` (the icon `name` is the
  variant of), `state` and `variant`. With bound params or texts it also carries `bound`,
  `params`, `texts` and `drawn` (see [Parameters from fields](#parameters-from-fields) and
  [Texts](#texts));
- `resolved_icons`: its icon for each rendition it has one for, `resolved_icon` among them, each
  with `rendition`, whose icon it is (`screen` for a label drawn with the screen icon).

Setting it:

- `POST /api/v1/assets` takes `icon_name`.
- `PUT /api/v1/assets/{id}` takes it too: absent keeps it, null or `""` gives it its
  categories', and a name gives it that one. It takes `icon_rendition` beside `icon_name`, to
  set its own label icon.

A scan's asset carries `icon`. Label data carries `label_icon`, as `icon`, `icon_color`,
`bound_icon` and `state_icon` are the screen icon, when the label icon is another. Archives keep
each icon's rendition (see [Backups](backups.md)).

### Scenes

`GET /api/v1/assets/{id}/scene` (`assets.read`) answers:

- the location;
- the way down to it (`path`, outermost first);
- the floor (width and depth in tiles);
- the `viewbox`;
- the `items` in drawing order.

Each item has its `id`, `name`, `code`, `condition`, `icon` (as `resolved_icon`), `x`, `y`, `z`
and `size` in tiles, `tx` and `ty` to draw it at, `holds`, `arranged` when it was placed,
`overlaps` (the IDs of those it overlaps), `movable` when the caller may arrange it, and, drawn
as a box, its `shapes`. What the caller cannot read is left out. An asset says how many things
are in it as `holds`.

`PUT /api/v1/assets/{id}/placement` (`assets.write` on it) places a thing, `{"x", "y", "z"}`,
each 0 to 10,000, with `in` naming the location when it is in more than one. `DELETE` (`?in=`)
puts it back with the rest. Either answers with the location's scene. A thing in no location is
`409`. Its history records each move (`asset.edited`, `"what": "place"`, the location as `key`
and `label`, the positions as `from` and `to`, "2, 1, 0.5").

`GET /api/v1/assets/{id}/scene/image` (`assets.read`) composes the scene into one drawing
(`scene.Picture`), drawn by the same SVG writer and rasterizer as icons' images:

- the floor in the palette's `virt` and its grid in `ink-muted`;
- then each thing, its icon or box moved to where it stands, its `category` token painting in
  its own category's color (`ink-muted` for one with none).

Its parameters:

- `show`, an asset's ID, outlines that one in `accent`.
- `format` (`svg` or `png`) and `theme` are as for icons.
- `height` is 1 to 2,048 pixels, 512 unless given.

What the caller cannot read is left out.

### Over MCP

| Tool | Icon behavior |
|---|---|
| Category icon tools | Take `rendition`, `screen` unless given. |
| `draw_icon` | Takes `rendition` for a category's or an asset's icon. |
| `get_asset` | Gives `label_icon` when its labels draw another icon. |
| `update_asset` | Takes `icon_rendition` for the icon `icon` or `category_icon` sets. |
| `search_assets` | Takes `icon`, as `GET /api/v1/assets?icon=` does. |
| `get_scene` | The scene, as `GET /api/v1/assets/{id}/scene`. |
| `place_asset` | Places a thing, as `PUT /api/v1/assets/{id}/placement`. |
| `render_scene` | The scene image, as a PNG. |

MCP's `texts` uses the drawn icon with an asset's texts. MCP reads and saves sources one at a
time; neither the export nor the import is reachable over it.

## The isolex compiler

Icons are written in [isolex](https://forge.axonibyte.com/axonibyte/isolex), a small language
for isometric icons by **Axonibyte Innovations, LLC**, licensed under the Apache License 2.0
(`internal/isolex/NOTICE` and `LICENSE-isolex`).

The compiler is isolex's own, built to WebAssembly. It runs in-process under
[wazero](https://wazero.io) (pure Go, no cgo), in package `internal/isolex`.

### Pinning

The compiler is pinned by checksum:

- `internal/isolex/wasm.lock` pins an isolex release and the SHA-256 of its module.
- `tools/isolex/fetch.sh` downloads that module into `internal/isolex/wasm/` (ignored by git),
  and the release build runs it before `go build`.
- Anything that does not match the checksum is refused and nothing is written.
- The server checks the module it embedded again before using it.

A binary built without the module runs with icons switched off.

### Containment

The module is contained. It imports nothing, so it is granted nothing: no files, clock or
network. Each compile gets a fresh instance, has its memory capped (256 MiB) and its time
bounded (10 s), and shares nothing with any other compile.

### Conformance

It compiles the same here as anywhere else. The pinned release's own fixtures (isolex 0.6.0's
sample, transforms and text) compile here to its IR goldens, byte for byte
(`internal/isolex/testdata/isolex-0.6.0`).

The IR is read by `internal/isolex/ir`, and drawn as SVG by `internal/isolex/svg` exactly as
isolex's own renderer draws it. The sample's IR, compiled here and drawn here, is isolex's SVG
golden byte for byte.

It reads two IR versions:

- **Version 2:** isolex 0.4's groups, each drawn through a matrix, cut to a convex clip and with
  strokes that may keep their width. These are drawn here as isolex 0.4.0 to 0.6.0 draw their
  sample, transforms and text goldens, byte for byte (`internal/isolex/testdata/isolex-0.6.0`).
- **Version 1:** so that a build an older isolex made is drawn until a new one replaces it, and
  stays drawn if the lexicon does not compile under the new one.

What each token is painted with is the caller's: a palette, or CSS variables for a page that
themes icons. Both packages use only the standard library, and pass their tests under TinyGo, so
the label renderer can use them too.

### At startup

The server compiles the module to machine code in the background at startup, so the first save
doesn't wait. It then builds what is published if the current build isn't of it (none yet, or
one made by another isolex). If the new isolex doesn't compile it, the last good build stays,
and the log says why.
