# Links between assets

Links say how assets go together. Each kind of link reads from both ends: a bin *contains* screws, and the screws are *contained in* the bin.

- **Contains** gives where things are. A thing is in one place at a time, so linking it to a new bin moves it.
- **Has part** joins a whole and its parts.
- **Used with** reads the same both ways.
- **Depends on** reads *needed by* from the other end, and is followed through several hops: the laser cutter depends on the extractor, which depends on the three-phase supply.

Add links on an asset's page. Those who manage categories can add kinds of link on the **Link types** page; a kind that gives where things are must keep each thing in one place.

## Following links

A kind of link marked *followed*, as **depends on** is, makes chains. An asset's **Graph** tab draws it with what needs it to the left and what it needs to the right, as far as the hops you choose (up to 10): choose *What needs it* to see what stops working if it does. Click an asset in the drawing to open it, with its graph drawn around it. Below the drawing, the same graph is a list, by hops, for the keyboard and screen readers. Download the drawing as SVG or PDF to print it.

The **Graph** button on the asset list, and on a category's page, draws every asset in the list or category with the followed links between them: the whole map.

Loops are allowed (A needs B, and B needs A): following stops where it comes back round, and the graph says where. A graph holds at most 500 assets, and says when it was cut short. Assets you may not see are never named: the graph counts them, and does not follow links through them.

Reports can count followed links: add a column bound to `{{asset.needed_by_all}}` and sort by it, highest first, to rank the assets whose loss would reach furthest. `{{asset.needed_by}}`, `{{asset.needs}}` and `{{asset.needs_all}}` count the rest. See [Reports](/help/reports).

Those who manage categories mark a kind of link as followed on the **Link types** page, even while links use it.

Links can be imported and exported too: see [Importing and exporting](/help/import).
