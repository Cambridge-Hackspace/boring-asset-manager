# The spool

The **Spool** is a queue of labels waiting to be printed. Add labels over the day as things come in, then sort and print them together. Everyone who prints labels shares one spool; tick **Only labels I added** to see just yours.

## Adding labels

- On the asset list, **Add all to the spool** adds every asset the filters show, and with some ticked, **Add to the spool** adds those.
- On a category's page, **Add its assets to the spool** adds its active assets, with its subcategories' unless you untick that.
- An asset's page and its scan page each have **Add its label to the spool**.
- Making an asset, or importing a CSV file, can add the new assets' labels too. The choice starts ticked and remembers what you chose last.

An asset's label already waiting with the same template is not added twice. Archived assets are never labeled.

## Templates and printers

Each label takes the template its category names, under **Labels** on the category's page; a subcategory takes its parent's unless it names its own. When only one printer suits that template, the label goes to it; when only one pool does, and holds every printer that suits it, the label goes to the pool.

On the spool, labels are grouped by printer and template. Tick some, or a whole group, then set their template, printer or copies together. Only printers and pools a template suits are offered, and only templates a printer or pool takes. **Download** stands in for a printer, for labels printed on sheets.

Anything that keeps a label from printing is marked beside it: no template or printer, a template the printer does not suit, or an archived asset. So is why it failed last time, such as a printer that could not be reached; that does not stop it printing again, and changing the label clears it.

## Printing

**Print everything ready** sends every label that can print as it stands; a group's **Print** sends that group's, and with labels ticked, **Print** sends those. Each printer and template becomes a job, of at most 500 labels.

Jobs print in the background, so you can go on working. Each printer takes one job at a time, so jobs never get mixed up on it, and different printers print at the same time. Printing straight from an asset's page waits its turn with them. Labels in a job show it beside them until it is done, and cannot be changed meanwhile.

If a label cannot be produced, it stays in the spool with the reason, and the rest print. If the printer cannot be reached, the job's labels all stay, with the error, to print again. If the server stops while sending, they stay marked as perhaps printed: look at the printer before printing them again.

## Pools

A **pool** is a set of printers loaded with the same labels, such as three Rollos with 4 × 6 in labels, set up under **Pools** on the **Printers** page. Send labels to a pool rather than a printer, and its printers share them.

A pool's job is cut into batches, 10 labels each unless the pool says otherwise. Each printer takes the next batch as soon as it has sent its last, so a faster printer ends up printing more and none waits for another. A label's copies always come out of one printer. Printers behind a spooler (IPP or Windows) take every job at once whatever their speed, so they get a fixed share of the batches instead, by their share; tick **Every printer takes a fixed share** to share out every pool job that way.

A printer whose labels do not suit a job's template sits that job out, with a warning when you print. A printer that cannot be reached hands its batch to the rest of the pool and drops out of that job; the job fails only when every printer has. A printer can be in several pools, and still be used on its own.

## Jobs

**Jobs** lists the print jobs and downloads of the last 30 days: the printer or pool, the template, how many labels, and how each went; a pool's job shows how many each of its printers has printed. **Cancel** stops a job not yet started, and its labels wait in the spool again. **Send again** sends what a finished job printed once more, to the same printer, to another, or as a download: its labels go back in the spool and, to a printer, print at once.

## Downloading

A group going to **Download** has **Download PDF** and **Download ZIP**: one file with every label ready, each as many times as its copies. Downloaded labels count as printed, and the download is listed under **Jobs**. Labels that cannot be produced, such as one whose template needs a code the asset lacks, stay in the spool with the reason.

## Printed

**Printed** lists what was printed or downloaded, with the template, the printer and who printed it, from the last 30 days unless you ask for older. Tick some and **Print again** to put them back in the spool. Every printed label is also recorded in its asset's history, as are labels printed straight from an asset's page.
