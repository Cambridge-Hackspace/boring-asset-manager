# Icons

A category can have an **icon**: a small isometric drawing of what its assets are. A subcategory shows its parent's icon until it has one of its own. With no icons anywhere, BAM! shows none.

## Drawing one

Icons are written, not drawn: each is a short text in **isolex**, a language for icons. Open a category, go to **Icon**, and choose a way to start:

- **Extend** the icon of the category above it, and change one part.
- **Start from a box**, a plain cube to build on.
- **Copy** another category's icon.

A part can be turned a quarter at a time, mirrored, rotated, slanted or laid flat on a face of a box, and an icon can carry words, such as a rack position or a port count, set in a font kept in the **Library**.

The editor checks what you write as you type, and shows it beside the editor, in light and in dark, with the icons of the category's subcategories. A mistake is underlined, and listed under **Problems**; choose one to go to it. **Ctrl+Space** suggests what can go where the cursor is, such as a shape or a color. **Tab** indents; to leave the editor with the keyboard, press **Esc**, then **Tab**.

## Saving

**Save** publishes the icon if it, and every icon that uses it, still draws. Otherwise it is kept as a **draft**, with its problems, and the icon shown stays as it was, so a mistake never breaks an icon. Fix the draft and save again, or **Discard the draft**.

## Colors

Icons paint with the colors of a **palette**, such as *ink* and *body-top*, never with a color of their own, so they suit light and dark mode alike. The color *category* paints in the category's own color.

## Assets' icons

An asset shows the icon of its nearest category with one: beside its name on its page and when it is scanned, and in lists, in place of a photo or, on one, in its corner. Its **Details** say which category it comes from.

To draw one asset differently, edit it and, under **Icon**, choose **Choose another…**: any icon there is can be picked, such as a broken variant of its usual one. **Use the category's** gives it back its categories' icon. If the icon it was given is later taken away, it shows a question mark, and says so, until it is given another.

## Sizes from fields

An icon can be drawn from an asset's own values. If its source has a **param**, such as `param u 1` for how many rack units high it is, open its category and, under **Parameters**, choose a number field to set it from, such as **Rack units**. Each asset is then drawn with its own value: a server with 2 rack units twice as tall as one with 1. An asset with nothing in the field is drawn with the param's default. A change shows once the icons are built again, a moment later.

An icon can carry an asset's own words, too: its name, its code, or what a field says. Give its source a **text param**, such as `param label (text)"Label"`, and write it with `text`, in a font from the **Library**; then, under **Parameters**, choose what sets it: **The asset's name**, **Its code**, or a field. Each asset is then written with its own, wherever its icon is drawn: in lists, on its page, on labels and in scenes.

## States

An asset's icon can show what state it is in: **out of service**, **needs maintenance**, **overdue**, **on loan**, **out of stock** or **running low**. An icon shows a state by having a **variant** for it, such as *switch-down*, a switch with a red badge; an asset in that state is drawn with it, everywhere its icon is. An asset in several states shows the first in that list that its icon has. Icons with no variants are drawn as they always are.

To start, on the **Icons** page, under **Library**, choose **Start the state badges** and save: it draws a badge for each state. Then add `axes "state"` to an icon, or to the one it extends, and it has a variant for each. Under **Asset states**, choose which variant each state is drawn with, or none.

## Seeing where things are

Anything that holds other things, such as a room, a rack or a shelf, can be seen from inside: on its page, choose **See inside**. What is in it is drawn standing on its floor, each with its icon, or as a plain box if it has none, laid out in rows by name. Scroll or use **+** and **−** to zoom, drag to look around, and **Fit** to see it all. Choose something to see what it is, with **Open** and, if things are in it too, **Look inside**; double-click to do the same. The links above lead back out.

On any asset's page, **Show in place**, beside where it is, shows it in its place, outlined, with the rest faded.

**Save as SVG** and **Save as PNG** save the picture, to print or to share. Something shown in place is outlined in it too.

## Arranging what is in a place

If you can edit what is in a place, choose **Arrange** to move things where they really are. Drag something to move it across the floor, a tile at a time. To stack things, as on a shelf or in a rack, choose one and drag the handle above it up or down: it stops on the top of whatever is under it. The arrow keys move the chosen thing too, and Page Up and Page Down lift and lower it. Every move is saved and shown in the thing's history. **Put back** returns it to being laid out with the rest.

Things may be put where something else already is. They are outlined in red, and choosing one says what it overlaps.

## On labels

A label template can print icons too: add an **Icon** in the template editor, for each asset's icon or one you choose. See **Labels**.

An icon that reads well on screen doesn't always print well. Labels mostly print in black and white on small thermal tape, where shaded faces turn to noise and the category's color to an unpredictable gray; what prints best is flat line art. So a category can have a **label icon** too: open the category, go to **Icon**, choose **On labels**, and draw one, by convention named after its screen icon with `-label` on the end. The preview shows it as it prints, in black and white at a label's size, shaded and as an outline.

Labels print each asset's label icon: that of its nearest category with one, as for its screen icon. With no label icon anywhere above it, a label prints the screen icon, so nothing changes until you draw one, and drawing one for a category gives it to every subcategory without one of its own. An asset with an icon of its own prints that on its labels too, unless it has a label icon of its own: edit it, and under **Icon**, choose **Choose a label icon…**.

## History, renaming and deleting

**History** lists every version published; any can be loaded back into the editor. Renaming an icon renames it for the assets given it too. An icon can be renamed or deleted only when no other icon still uses it, such as a subcategory's that extends it. A category whose icon others use cannot be deleted until they no longer do.

Whoever may change categories can draw their icons. Changing the palette and the shared library of shapes needs **Manage icons**.

## The Icons page

**Icons**, under **Catalog**, shows on its first tab every icon, by group, each with **SVG** and **PNG** links to download it as an image, and what **needs attention**: drafts waiting with problems, warnings, and icons that assets were given that no longer exist; follow one to the assets, to give them another. Whoever may **Manage icons** changes what every icon is drawn with, each on a tab of its own:

- **Library**: shapes icons share and no one category owns, such as a base to extend or a fragment to insert, written and saved as a category's icon is; and fonts, for the words icons carry. isolex can convert a TrueType or OpenType font into one, if the font's license allows it; the converted font keeps its license.
- **Palette**: the colors icons paint with, each named, with a light and a dark color. A color an icon still uses cannot be taken away.
- **Settings**: the line widths icons may use, the colors only an icon that grants itself one may use, and how much drawing one icon may take.
- **Asset states**: the variant each state is drawn with (see **States**).

**Export as a zip** downloads every icon as an isolex library folder, which isolex studio opens as it is. Bring it back, edited, on **Admin › Import & export**: what changed is replaced, colors and categories' icons too, and the rest is left alone. On the **Library** tab, **Import** takes a zip of .ilx files into the library at once, or none of them if any icon would stop drawing.

Icons are compiled by isolex, the work of Axonibyte Innovations, LLC.
