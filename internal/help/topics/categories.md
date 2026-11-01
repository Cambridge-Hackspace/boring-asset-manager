# Categories and fields

**Categories** group assets, and an asset can be in several. A category can sit inside another: *Power tools* inside *Tools*. A subcategory inherits from the categories above it.

## Fields

**Fields** are the details assets carry, such as a serial number or a purchase date. Create them once on the **Fields** page, then give them to categories: an asset fills in every field its categories, and the categories above them, ask for. A field can be required. A field that is a **choice from a list** has a box for each choice; the value stored for it is made from what you type, and shown under it, with **Change** to set another. Values already stored never change by themselves.

If an asset leaves a category, the values it had for that category's fields are kept, and listed on its page under **Values kept from former categories**, by their fields' labels, or by key where a field no longer exists.

## A category's page

A category's page is in tabs, and each part of it saves with its own button. A tab with changes not saved says so, and they are kept while you look at another.

- **About**: its name, the category it sits in, and its color.
- **Fields**: what its assets fill in, its own and those it inherits.
- **Stock and lending**: whether it is consumable, with an optional unit and threshold (see [Stock](/help/stock)), and whether its assets can be lent (see [Lending](/help/loans)).
- **Labels**: the templates its assets' labels are printed with, and a button that adds them all to the print queue.
- **Icon**: a small drawing of what its assets are, which its subcategories inherit. See [Icons](/help/icons).
- **Links and hooks**: the kinds of link offered first for its assets, what scanning them shows (see [Scanning](/help/scanning)), and the [hooks](/help/hooks) offered on them: buttons such as Identify.
- **Who can see it**: its scopes, which decide who can see and change its assets; **public** lets anyone see them, even signed out. See [People and permissions](/help/people).

## Duplicating one

**Duplicate**, on a category's **About** tab, makes a copy beside it, named *… copy*, with everything it sets and its own fields, but none of its subcategories or assets. It copies the category as saved, so save any changes first. The copy uses the same scan and label templates, rather than copies of them.

A yellow triangle marks a category that ties with another over which scan template some assets show.
