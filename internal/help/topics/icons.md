# Icons

A category can have an **icon**: a small isometric drawing of what its assets are. A subcategory shows its parent's icon until it has one of its own. With no icons anywhere, BAM! shows none.

## Drawing one

Icons are written, not drawn: each is a short text in **isolex**, a language for icons. Open a category, go to **Icon**, and choose a way to start:

- **Extend** the icon of the category above it, and change one part.
- **Start from a box**, a plain cube to build on.
- **Copy** another category's icon.

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

## States

An asset's icon can show what state it is in: **out of service**, **needs maintenance**, **overdue**, **on loan**, **out of stock** or **running low**. An icon shows a state by having a **variant** for it, such as *switch-down*, a switch with a red badge; an asset in that state is drawn with it, everywhere its icon is. An asset in several states shows the first in that list that its icon has. Icons with no variants are drawn as they always are.

To start, on the **Icons** page, under **Library**, choose **Start the state badges** and save: it draws a badge for each state. Then add `axes "state"` to an icon, or to the one it extends, and it has a variant for each. Under **Asset states**, choose which variant each state is drawn with, or none.

## On labels

A label template can print icons too: add an **Icon** in the template editor, for each asset's icon or one you choose. See **Labels**.

## History, renaming and deleting

**History** lists every version published; any can be loaded back into the editor. Renaming an icon renames it for the assets given it too. An icon can be renamed or deleted only when no other icon still uses it, such as a subcategory's that extends it. A category whose icon others use cannot be deleted until they no longer do.

Whoever may change categories can draw their icons. Changing the palette and the shared library of shapes needs **Manage icons**.

## The Icons page

**Icons**, under **Catalog**, shows every icon, by group, each with **SVG** and **PNG** links to download it as an image, and what **needs attention**: drafts waiting with problems, warnings, and icons that assets were given that no longer exist; follow one to the assets, to give them another. Whoever may **Manage icons** changes there what every icon is drawn with:

- **Library**: shapes icons share and no one category owns, such as a base to extend or a fragment to insert, written and saved as a category's icon is.
- **Palette**: the colors icons paint with, each named, with a light and a dark color. A color an icon still uses cannot be taken away.
- **Settings**: the line widths icons may use, the colors only an icon that grants itself one may use, and how much drawing one icon may take.
- **Asset states**: the variant each state is drawn with (see **States**).

**Export as a zip** downloads every icon, as the isolex compiler reads them. **Import** takes a zip of .ilx files into the library at once, or none of them if any icon would stop drawing.

Icons are compiled by isolex, the work of Axonibyte Innovations, LLC.
