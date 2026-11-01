# Template editor

The template editor is where label templates are designed. It draws the label exactly as it will
print, with the server's own renderer, and lets you place, transform and edit text, codes, images
and shapes on it. Its layout and gestures follow Krita, with a few ideas from Figma and the CSS box
model.

## Layout

The editor is laid out as Krita is:

- **Toolbox**, down the left: select, text, code, image, rectangle, ellipse, line, polygon, and
  SVG import.
- **Tool options**, across the top. For the selection, these give its position, size and angle
  in numbers.
- **The label**, in the middle.
- **Panels**, down the right: Properties, Layers, Arrange, Label, Printer and Preview data. Each
  collapses and moves, which is remembered in the browser, and says what it holds in a line beside
  its title.
- **Status bar**, along the bottom, with the pointer's place and what the modifiers do.

Tools that add something draw it where it is dragged or clicked. Chosen from the keyboard, they
add it in the middle of the label.

Handles are drawn the same size at every zoom.

On a phone, the parts stack: tools, then the label, then the panels.

## Selecting and transforming

Selecting and transforming follow Krita's shape tool. The selection has one box with eight
handles, turned with the element. The handles' cursors show the way they pull.

### Turning

Grab just outside a corner to turn the selection.

- By default it snaps to 45° when near. This is switchable, and the choice is remembered.
- With Ctrl, it turns in 15° steps.
- With Shift, it turns freely.

### Resizing and moving

| Modifier | Effect |
|---|---|
| Shift, resizing | Keeps proportions. |
| Shift, moving | Holds the move to one axis. |
| Shift, any drag | Frees the drag from the grid. |
| Ctrl, resizing | Resizes from the center. |

A modifier pressed or let go mid-drag takes effect at once. The size, angle or place floats beside
the pointer while dragging.

### Numbers in the tool options

The tool options give X, Y, W and H at an anchor chosen on a 3 × 3 grid. Ctrl+click on a handle
sets the anchor too. There is an aspect lock.

Lengths can be typed with units: `5mm`, `1.5cm`, `0.2in`, `72pt`.

### Choosing what is selected

- A box dragged left to right selects what it covers.
- A box dragged right to left selects what it touches.
- With Alt, a box can be started over an element.
- Ctrl+click takes the element beneath the one selected at that point.
- A click on one of several selected elements takes it alone.

### Context menu

Right-click, Shift+F10 or the menu key opens a menu with:

- stacking
- group
- turn
- flip
- lock
- copy, paste and delete

## Layers

The **Layers** panel works as Krita's layer docker. It shows each element top first, with:

- a thumbnail of what it draws
- an eye that hides it while editing. A hidden element still prints, and the panel says so.
- a lock

Drag a row to reorder. Double-click a row, or press F2, to rename it.

Groups are folders that open and close, with an eye, a lock and a name of their own.

## Arrange

**Arrange** lines elements up and spreads them out, either with equal gaps or with centers evenly
apart. A single element is arranged against the label.

## History

**History** lists every change by name. Clicking one goes back to just after it, or on to one
that was undone.

## Snapping and guides

Snapping works as Krita's.

### Snap to

One **Snap to** menu chooses what to snap to, remembered in the browser:

- the grid
- guides
- other elements' edges and centers
- the label's edges and center
- turns to 45°

Shift turns snapping off while held.

### What snaps

- A move snaps the selection's edges or middle.
- A handle snaps the point it drags.
- A drawing tool snaps its points. Alt turns this off.

Each axis snaps by itself, and the line snapped to is drawn.

### Guides and grid

Guides are dragged out of a ruler, moved, and dragged off the label to delete them.

The **Grid and guides** panel:

- shows or locks the guides
- deletes all guides
- sets the grid: its spacing (kept equal or not), offset and subdivisions, and whether it is shown

## Drawing tools

Drawing works as Krita's tools.

A rectangle, ellipse, text box, code or image is dragged out, with its size beside the pointer.

| Modifier | Effect |
|---|---|
| Shift | Keeps it square. |
| Ctrl | Draws it from its center. |
| Alt | Moves it as it is drawn. |

A line keeps to 15° steps with Shift. Its length and angle are shown beside the pointer.

## Pen and point editing

### The pen

The **Pen** makes a `path` shape.

- Each click places a point.
- Dragging pulls out smooth handles. With Alt, it pulls out just the one handle.
- Right-click takes back the last point.
- A double-click or Enter finishes.
- A click on the first point closes the shape.

### Editing points

To edit the points of a selected polygon or path (imported artwork too), double-click it, or
choose **Edit points** in its menu. Then:

- Points and their handles drag. A smooth point's other handle follows; Alt breaks it.
- A double-click on the outline adds a point.
- Shift+click makes a point sharp or smooth. A polygon given a curve becomes a path.
- Delete removes the chosen point.
- Escape is done.

Points and handles keep to the label.

## Measure

**Measure** drags between two points to show the distance, its angle, and how far across and
down. It adds nothing to the label.

## Text

Text is typed on the label, as with Krita's text tool.

- A click with the **Text** tool gives a single line.
- A box dragged out with it wraps.
- Either is typed in at once.
- A double-click on a selected text types in it again.

The text is typed in the font labels print in, which the live renderer hands the page. It is shown
at the size it is drawn at, whether shrunk or filling its box; the renderer reports this as each
key is typed. It is lined up as it prints, and left out of the label drawn while it is being
typed. Fields are typed in braces.

A text bar across the top sets:

- the font
- the size
- bold
- alignment
- wrapping
- **Fill the box**

It also puts a field in at the caret.

Escape or a click elsewhere finishes. A text left empty is removed.

## Code margins

A code's margin is its padding, as in Figma and the CSS box model.

- The code is resized with the ordinary handles.
- Its padding is shaded inside its box.
- Padding is set in the template's units: one field for all sides, or each side on its own.
  Units can be typed.
- The standard quiet zone is the placeholder, and a **Standard** button restores it.

The editor writes margins as lengths. Margins given in modules are turned into lengths when first
changed.

Where the box is not a whole number of modules, what is left over is shaded apart and described in
words.

## Keyboard shortcuts

The shortcuts are few and standard.

| Keys | Action |
|---|---|
| Ctrl+C | Copy. |
| Ctrl+V | Paste. SVG pasted is imported. |
| Ctrl+A | Select all. |
| Ctrl+Shift+A | Select none. |
| Ctrl+Z | Undo. |
| Ctrl+Shift+Z | Redo. |
| Delete or Backspace | Delete the selection, or, when editing points, the chosen point. |
| Escape | Cancel what is in progress (a drag is put back as it was), or deselect. |
| Arrow keys | Move the selection. |
| Shift+arrow | Move by ten grid steps. |
| Alt+arrow | Move by a tenth of a grid step. |
| Tab, with nothing focused | Hide everything but the label, keeping it where it is on the screen. |
| Ctrl+Enter | Open the action search. |
| Right-click, Shift+F10 or the menu key | Open the context menu. |
| F2 | Rename the layer. |
| Space, or the middle button, dragged | Pan the view, from any tool. |
| + and - | Step the zoom toward the pointer. |
| 1 | Zoom to 100%. |
| 2 | Fit the label. |
| 3 | Fit the label's width. |
| 0 | Switch between fitting and the zoom before. |

### Main menu

The menu button beside **Save**, like a drawing program's main menu, holds what concerns the
template rather than what is on it:

- **Copy ID** puts the ID the API names a saved template by on the clipboard. The editor does
  not show the ID.
- The action search.

### Action search

Ctrl+Enter opens the action search, as in Krita. Type a few letters of any action's name, or of a
word it goes by, in any order ("remove" finds Delete). It lists the actions that can be taken now,
with their keys. The arrow keys or the pointer choose one, and Enter or a click takes it.

## Touch gestures

On a touch screen, the gestures are Krita's.

| Gesture | Action |
|---|---|
| One finger | The tool's, as the mouse is. |
| Two fingers, pinched | Zoom toward them. |
| Two fingers, dragged | Pan. A second finger puts back what the first had begun. |
| Quick tap with two fingers | Undo. |
| Quick tap with three fingers | Redo. |

## View and zoom

The view works as Krita's.

- Ctrl+wheel zooms toward the pointer, and the wheel scrolls.
- Space or the middle button drags the view from any tool.
- The keys + and -, 1, 2, 3 and 0 zoom (see [Keyboard shortcuts](#keyboard-shortcuts)).
- The status bar's zoom box takes a percentage, typed or slid, from 5% to 6400%.

### Real size

At 100%, one printer dot is one screen pixel. With **Print size**, the label is shown at its real
size instead.

A browser never says how large its screen is. So each screen is calibrated once against a ruler,
and remembered in that browser by its size and pixel ratio. The calibration follows the window as
it moves between screens. A screen not yet calibrated takes the browser's 96 pixels an inch, and
the editor says the size is approximate.

## Rulers and overview

Rulers along the top and left mark the pointer and the selection's extent. Their numbers are
spaced to the zoom. Right-click a ruler to choose millimeters or inches.

The **Overview** panel shows the whole label with the part in view outlined. Dragging in it moves
the view, and scrolling zooms.

## Fields and data

Bindings are offered from the field corpus and asset placeholders.

The label is previewed with placeholder data or a chosen asset.

## Printer and preview

Units, size and DPI can be set directly, or taken from a printer. Choosing a printer applies its
loaded media's size and its DPI. The fit check runs as the user edits.

Exports and test prints come straight from the editor.

## Saving

Templates save through the API. Unsaved changes are kept in the browser in case the tab closes.

## Internals

### The live renderer

The editor never reimplements layout in the browser. The canvas shows the label as the server's
own renderer draws it, compiled to WebAssembly (`cmd/bamlive`, `internal/live`) and run in a Web
Worker.

It redraws exactly as the label will print, at every frame of a drag, a turn or a keystroke:
1-bit, images dithered, dot for dot as the server draws it. The web interface's tests compare
every golden label's dots with the server's.

- The renderer is loaded only by the editor. It is about 0.7 MB with Brotli.
- One label is drawn at a time. Of what is asked for meanwhile, only the newest is drawn next.

Paths are filled in tiles of at most 512 dots, where the rasterizer adds up coverage in integers.
A label is therefore the same dot for dot on every platform the server or browser runs on.

Over the drawn label sits an interactive layer of element outlines. Text measurement, barcode
module snapping and quiet zones always match what prints.

### Data for the preview

The renderer draws with the data that `POST /api/v1/templates/preview-data` gives. That is either
an asset's (`asset_id`), or the sample data, whose fields it fills with placeholders as the
server's preview does.

### Fallback to the server

Until the live renderer has loaded, or where it cannot run, the canvas shows the label as the
server renders it, and says so. This is an SVG from `POST /api/v1/templates/preview`, requested at
most a few times a second while the user works.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `POST /api/v1/templates/preview-data` | `templates.manage` | The data the live renderer draws with: an asset's (`asset_id`) or the sample data. |
| `POST /api/v1/templates/preview` | (see [Labels](labels.md)) | The label as the server renders it: SVG by default, or PNG or JSON. |

### Preview formats

`POST /api/v1/templates/preview` answers:

- as SVG, by default
- as PNG, with `"format": "png"`; 1-bit with `"mono": true`
- as JSON, with `"format": "json"`

The JSON answer gives the label's size, units and DPI, and each element's box. For each element it
gives:

- `rotation` and `bounds`, once turned
- `value`, the text or code it prints for the asset
- for a code, `code`

A code's `code` gives:

- its module
- its margins as drawn, in the template's units and in dots
- `padding`: the margin asked for on each side, or the standard quiet zone. What the margin has
  beyond it was left over, where the box is not a whole number of modules.
- `standard`: the standard quiet zone
- `narrow`: the sides narrower than the standard quiet zone

Each narrow side is also described in `warnings`. Assistants read the JSON answer (see
[MCP](mcp.md)).
