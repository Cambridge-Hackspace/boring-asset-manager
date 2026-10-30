# Importing and exporting

**Export CSV** on the asset list downloads the assets shown, one per row, with their categories, codes, fields, stock and links. Open it in a spreadsheet, change it, and bring it back with **Import CSV**.

## Importing

1. Choose the file. BAM! suggests where each column goes; check each one, or choose *Ignore this column*.
2. BAM! checks every row before anything is written, and lists any problem with its line.
3. **Import** writes every row, or, if anything changed meanwhile, none.

Rows with an ID update that asset, and blank cells leave its values alone; rows without one create assets. Categories must already exist.

## Links

The **links** column holds entries such as `contained_in:Bin A3;used_with:Drill`. Assets are named by name, or by ID or code where a name is shared. An import adds and moves links; tick **Also remove links that no row states** to remove the rest, so an edited export comes back exactly.

## Packages

**Admin › Import & export** (with the *settings.manage* permission) packages part of BAM! to share with another: categories, the fields, actions, link types, templates, reports, flows and icons they use, and their assets if you like.

1. Tick what to share. Ticking a category ticks those under it.
2. What your choice needs is ticked for you, marked *Needed by…*. It stays ticked until you untick what needs it.
3. Anything that stays behind, such as a printer a flow prints to, is listed with why.
4. Give the package a name, and **Export**. Your browser saves it, however large.

A package never holds people, passwords or printers, and holds only what you can see.

### Importing a package

**Choose a package or backup** on the same page. It is uploaded and checked, and nothing changes until you **Import** it.

1. Choose the category its top categories go under, or the top level.
2. What it holds that is here already is listed, with what to do with each: **Use the one here**, **Import as a copy** (renamed, with everything in the package that names it changed to match) or **Skip**. What is the same is used as it is; what differs is copied.
3. If you change either, **Check it with these choices**, so that what you import is what you see.
4. **Import**. Everything comes in new, so the same package can be imported twice, under different categories.

An icon set, a zip of an isolex library folder such as **Export as a zip** on the Icons page or isolex studio makes, imports the same way: it replaces the icons and palette colors it changes, and leaves the rest alone.

From a backup, choose which kinds to merge. People, settings, passwords and sessions never come in this way. What it needs that you may not do, such as adding flows without *flows.manage*, is said before anything changes.
