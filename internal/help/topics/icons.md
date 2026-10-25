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

## History, renaming and deleting

**History** lists every version published; any can be loaded back into the editor. An icon can be renamed or deleted only when no other icon still uses it, such as a subcategory's that extends it. A category whose icon others use cannot be deleted until they no longer do.

Whoever may change categories can draw their icons. Changing the palette and the shared library of shapes needs **Manage icons**.

## The Icons page

**Icons**, under **Catalog**, shows every icon, by group, and what **needs attention**: drafts waiting with problems, and warnings. Whoever may **Manage icons** changes there what every icon is drawn with:

- **Library**: shapes icons share and no one category owns, such as a base to extend or a fragment to insert, written and saved as a category's icon is.
- **Palette**: the colors icons paint with, each named, with a light and a dark color. A color an icon still uses cannot be taken away.
- **Settings**: the line widths icons may use, the colors only an icon that grants itself one may use, and how much drawing one icon may take.

**Export as a zip** downloads every icon, as the isolex compiler reads them. **Import** takes a zip of .ilx files into the library at once, or none of them if any icon would stop drawing.

Icons are compiled by isolex, the work of Axonibyte Innovations, LLC.
