# Stock

Some assets are counted rather than single things: screws, filament, cable ties. Give such an asset a quantity and, if you like, a unit such as `pcs` or `g`.

- **Use** takes some away; **Add** puts some back.
- A **low stock threshold** flags the asset as *running low* when it drops below it. **Mark running low** flags it by hand, and **Mark restocked** clears the flag.
- A category can be **consumable**: it is flagged running low when too few of its assets are left, or, with a unit, when their stock adds up to too little, such as spools of filament by weight.

Everything used and restocked is kept in the asset's history, and the **Running low** report makes a shopping list.
