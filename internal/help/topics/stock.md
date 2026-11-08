# Stock

Some assets are counted rather than single things: screws, filament, cable ties. Give such an asset a quantity and, if you like, a unit such as `pcs` or `g`.

- **Use** takes some away; **Add** puts some back.
- A **low stock threshold** flags the asset as *running low* when it drops below it. **Mark running low** flags it by hand, and **Mark restocked** clears the flag.
- A category can be **consumable**: it is flagged running low when too few of its assets are left, or, with a unit, when their stock adds up to too little, such as spools of filament by weight.

Everything used and restocked is kept in the asset's history, and the **Running low** report makes a shopping list.

## Stock kept in several places

The same thing is often kept in more than one place: M5 bolts in a workshop drawer and a bin by the laser cutter. On a category's **Counts in** tab, **Also counts in…** names other categories its assets count in as well, wherever they are; **Also includes…** does the same from the other side. Make a consumable category with a unit, such as *M5 bolts* in `pcs`, and have each place's category count in it: it becomes a **pool**.

- **One threshold watches the total.** The pool totals every place, and runs low only when they all together do.
- **Quiet bins.** A bin's own running-low flag holds off while its pool has enough, so an empty drawer raises nothing while the laser bay holds 300.
- **Where else to look.** A bin's page and scan page list the pool's other places that have stock: *140 pcs here, 300 pcs in laser bay*.
- **Move to another place** takes some from one bin and adds it to another in one step, recorded in both histories, without alerting the pool.
- **Views across the tree.** A category such as *PPE* can gather gloves, goggles and ear defenders from wherever they are filed, for one report, without moving anything.
- **Only membership comes through.** A link brings no scopes, fields or templates: who can see an asset, and what its form asks for, stay as they were. A link that would lead back to where it starts is refused, and one whose assets count in no unit the pool totals warns that it adds nothing.

Reports can list stock **by pool**, a line for each pool with its total and threshold and its places under it, or **by bin**, each place on its own line. People see only the places they may, and totals of those.
