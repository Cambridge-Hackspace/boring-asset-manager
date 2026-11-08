# Vendors

**Vendors** are the suppliers your space buys from, and how to reach them. Find them under **Vendors**, in **Catalog**. Search the list by anything a vendor has: its name, website, account number, address, notes, or a contact's name, role, email or phone.

## A vendor

- **Name**, **website** and the space's **account number** with them. The website is where things are reordered, so it must start with `https://` or `http://`.
- **Address**, and **notes**, written in Markdown: `**bold**`, `*italics*`, lists, and links to `https://` addresses, email addresses (`mailto:`) and pages of BAM! itself. Anything else, such as HTML, shows as you typed it.
- **Contacts**: as many as you need, each with a name, a role (such as *sales* or *support*), an email and a phone. Their emails and phones are links that start an email or a call.
- **Attachments**: files such as price lists, terms and quotes, which work as a category's do.
- **History**: when it was made, and each change to its details, contacts, scopes and files, with who made it to those who can see who changed assets.

Its **Assets** tab lists the assets bought from it that you can see, with when each was ordered, its price where you may see prices, and when its warranty ends. An asset names its vendor in its **Purchase** section (see [Assets](/help/assets)). Its **Contracts** tab lists its warranties, service and maintenance agreements, leases and licenses (see [Contracts](/help/contracts)).

## Catalogs

A vendor's **Catalog** tab lists what it sells, one entry for each thing: the vendor's **part number**, a **description**, a link to the **product page**, the **pack** (such as 100 screws), and its **price**, with the date it was checked. Only the last checked price is kept, and prices are typed in, never fetched from the vendor.

Each entry is for a **category**, for things counted by items, such as a category of screw boxes, or for an **asset** counted by quantity, such as a reel of solder. A category's **Catalog** tab, and such an asset's page, list what every vendor sells of it. Several vendors may sell the same thing; mark one entry **preferred**, and it is the one to reorder from.

**Export CSV** on the vendor's Catalog tab writes its catalog as a spreadsheet; edit it and import it again, or import a vendor's price list in the same columns. A file is checked first, and imported all or nothing.

Prices in catalogs are hidden from those without **See prices**, as assets' prices are. Adding and changing entries takes **Manage vendors** for the vendor.

## Reordering

A category can name the vendors it is bought from, in order of preference, on its **Vendors** tab. When the category is **running low**, the home page lists them under it, with a **Reorder** link to each vendor's website.

When a category or an asset is running low and has a **preferred** catalog entry, the home page shows it first: the vendor, part number and price, and a **Reorder** link to the product page.

## Who can see and change them

A vendor's **scopes** decide who sees it, as a category's decide who sees its assets: anyone who can read assets in one of its scopes sees it. A vendor in no scope is seen by everyone who can read assets, and one in the **public** scope by everyone signed in. Unlike a public asset, a vendor is never shown to people who are signed out.

Making and changing vendors takes the **Manage vendors** permission. It can be given in some scopes only: then its holders make and change vendors in those scopes, and cannot take a vendor out of them, or leave one in no scope, which would show it to everyone. See [People and permissions](/help/people).
