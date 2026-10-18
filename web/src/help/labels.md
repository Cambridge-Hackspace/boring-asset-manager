# Labels and printing

**Templates** lay out labels: text, codes such as QR codes, and images, at the size of your label stock. Text uses placeholders filled in for each asset, such as `{{asset.name}}`, `{{asset.location}}` or `{{field.serial}}`; the editor suggests them as you type.

`{{scan_url}}` is the address of an asset's scan page: a QR code of it takes whoever scans the label there. See [Scanning](/help/scanning).

## Printing

On an asset's page, or for several assets ticked on the list, choose a template to download labels as SVG, PNG or PDF, or send them to a printer set up on the **Printers** page. **Images** holds logos and pictures for templates.

Its category's templates are listed first, and the first of them that can label it and that a printer takes is chosen. Only the printers a template fits can be chosen; the others are listed with why not, such as a roll of the wrong size loaded. A template that cannot label an asset, such as one printing a QR code for an asset that has none, says so in place of its preview. On an asset's page, **Add to the spool** keeps its label to print later with others.

Each printer has a roll loaded. Brother QL printers take Brother's own rolls, all of which are listed; TSPL printers such as the Rollo, and DYMO LabelWriters, also take rolls of any size, added under **Label stock** on the **Printers** page with their width, and for die-cut labels their length and the gap between them.

A template is **fixed**, printing only at its own size and DPI, or **adaptive**: designed at one size, it is laid out again for each label it prints on, and at each printer's resolution, so one design serves 29 mm and 62 mm tape, or a Brother and a Rollo. Choose under **Sizing** in the editor, with the narrowest, widest, shortest and tallest labels it takes. Each element then says how it follows the label across and down: in proportion, pinned to an edge, to both edges (stretching), or to the middle. It can also have a smallest and largest size, and say what happens where it does not fit: the label is refused, it is left off, or something else takes its place, such as the code as text. The editor shows the template on each printer as loaded, and at any size you type, with the sizes it fits in its shape given the longest codes assigned now.

A template prints on a printer when its size and DPI suit the roll loaded. The template editor checks against a printer as you design: the one you last chose for the template, or else the first that takes it (choose **None** to stop checking). Printers do not reach every edge of every label: the canvas shades what the printer does not print, and a dashed line marks how near a die-cut label's edge content may be cut off as labels shift. Elements reaching into either are outlined in amber and named. Warnings never stop printing. **Use its label size** takes the size from the printer's roll and scales the elements with it, and **Test print** prints one label as the template is on screen, saved or not, for the asset being previewed or with placeholders.

The template list shows which printers each template prints on, as their rolls are loaded now. Changing a printer's roll never changes a template: a template that does not suit the new roll waits until one it suits is loaded again. Before saving a new roll, the printer form says which labels waiting in the spool would no longer fit, which categories' templates no printer would suit, and which pools' printers would no longer be loaded alike.

To gather labels over time and print them together, sorted onto several printers, use the [spool](/help/spool).
