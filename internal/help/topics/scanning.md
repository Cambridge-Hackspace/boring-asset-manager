# Scanning

Scanning an asset's label opens its **scan page**: what it is, where it is, what it holds, and quick actions such as using some, borrowing it, reporting a problem, or adding another like it or something in it. You see only the actions you are allowed to take, and details with nothing in them are left off. Assets that are public can be scanned without signing in; anything else asks you to sign in, and comes back to it after.

A phone's camera opens a label whose QR code holds its scan link, `{{scan_url}}`. New QR codes in the template editor hold it already, and the editor warns about a QR code that does not hold a link: a phone scanning it sees only the code. Each asset's page links to its scan page too.

The **Scan** page, in the menu, looks a code up: type or paste it (or its scan link), let a USB scanner type it, or, in browsers that can read codes, scan it with the camera. The home page has a box for it as well.

## Scan templates

What a scan page shows is a **scan template**, made on the **Reports** page: details of the asset, a table of what it holds, and which quick actions to offer.

A category chooses the template for its assets, and its subcategories use it too unless they choose their own. Assets in no such category show the default.

When an asset is in categories that are equally close to it and choose different templates, BAM! picks one: the deeper category, then the one with fewer subcategories, then the older one. A **yellow triangle** marks these assets and categories; choose a template on one of the categories to settle it.
