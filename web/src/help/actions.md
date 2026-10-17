# Actions

**Actions** are buttons on assets, such as **Identify**. Pressing one tells whatever is listening for it: a cabinet that lights up the drawer the asset is in, or a channel on the **Notifications** page that posts it somewhere.

## Pressing one

An asset's actions are under **Actions** on its page, and on its scan page when its scan template includes them. Some ask first before they are sent. Pressing needs the **Take actions** permission. An asset's actions can be pressed once every couple of seconds, so a cabinet is not asked to light up again before it has finished.

Every press is kept in the asset's history.

## Setting them up

Whoever manages categories defines actions on the **Actions** page: a label people see, such as "Identify"; a key devices listen for, such as `identify`; and, if wanted, a question to ask first.

Then, on a category's page under **Actions**, choose which of them it offers on its assets, and whether on its subcategories' assets too.

## What a device is told

Each press says which action, which asset, and every container it is in, outermost first, each with its codes and field values. So give each drawer a field such as **LED** with the number of its light, and pressing Identify on anything inside lights its drawer.
