# Importing and exporting

**Export CSV** on the asset list downloads the assets shown, one per row, with their categories, codes, fields, stock and links. Open it in a spreadsheet, change it, and bring it back with **Import CSV**.

## Importing

1. Choose the file. BAM! suggests where each column goes; check each one, or choose *Ignore this column*.
2. BAM! checks every row before anything is written, and lists any problem with its line.
3. **Import** writes every row, or, if anything changed meanwhile, none.

Rows with an ID update that asset, and blank cells leave its values alone; rows without one create assets. Categories must already exist.

## Links

The **links** column holds entries such as `contained_in:Bin A3;used_with:Drill`. Assets are named by name, or by ID or code where a name is shared. An import adds and moves links; tick **Also remove links that no row states** to remove the rest, so an edited export comes back exactly.
