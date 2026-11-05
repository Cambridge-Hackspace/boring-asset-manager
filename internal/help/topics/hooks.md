# Hooks

**Hooks** tell whatever is listening for them that something is happening to an asset, such as **Identify**: a cabinet that lights up the drawer the asset is in, or a channel on the **Notifications** page that posts it somewhere. A hook can be a button people press, fire by itself when something happens to an asset, or fire on a schedule. Older releases called them actions.

## Triggering one

An asset's hooks are under **Hooks** on its page, and on its scan page when its scan template includes them. Some ask first before they are sent. Triggering needs the **Trigger hooks** permission. If a device answers, its answer shows under the button. An asset's hooks can be triggered once every couple of seconds, so a cabinet is not asked to light up again before it has finished.

Every hook triggered is kept in the asset's history.

## Setting them up

Whoever manages categories defines hooks on the **Hooks** page: a label people see, such as "Identify"; a key devices listen for, made from the label as you type it, such as `identify`, which **Change** sets otherwise; and, if wanted, a question to ask first. Devices, and flows that trigger the hook, know it by its key, so changing the key of a hook in use says so first.

Under **Fired by**, choose what fires it:

- **A button people press on assets**, as above.
- **Events**: when an asset is lent, returned, runs low, has its condition change, and so on. Narrow it to when the asset's condition, or one of its fields, has a value, and to at most once every so many minutes, so a burst of changes fires it once.
- **A schedule**, in cron's five fields (minute, hour, day of the month, month, day of the week) and a time zone: `0 9 * * 1` is 9:00 on Mondays. It fires on each asset that is running low, lent, overdue or in a condition, as you choose.

A hook with no button is not shown on assets. Whatever fires it, a hook only ever fires on assets in the categories that offer it, and its history says what fired it.

Then, on a category's **Links and hooks** tab, choose which of them it offers on its assets, and whether on its subcategories' assets too.

## What a device is told

Each time, it says which hook, which asset, and every container it is in, outermost first, each with its codes and field values. So give each drawer a field such as **LED** with the number of its light, and triggering Identify on anything inside lights its drawer.

## Devices that listen

A device, such as a cabinet's controller, connects to BAM itself, so it needs no address of its own that BAM can reach. Give it an **API token** on the **API tokens** page, with a role holding **Receive events live** in the scopes its cabinet covers; it then opens a WebSocket to `/api/v1/events/live` with that token and is sent each hook triggered, and any other event it asks for, as it happens. It may answer each one, such as "Lit drawer A3" or "No such drawer", and the person who pressed the button sees the answer under it for a few seconds, or that nothing answered. The **API tokens** page shows which tokens are connected now.

A device that loses its connection should connect again; what happened while it was away is not sent again.

## Incoming hooks

An **incoming hook** works the other way round: a web address that something outside BAM calls to make something happen, such as a button by a cabinet, a Home Assistant automation, a form on another site, or a scale. Whoever manages settings makes them on the **Incoming hooks** page. Each one does one thing on the asset it finds: triggers a hook, records a use or restock, sets fields, or runs a flow to its end. It finds the asset from a code, an ID or a field's value in what it is sent, or is always about the same asset.

Each has a secret, shown once when it is made: the sender sends it with each request, or signs each request with it. **New secret** replaces it, and the old one stops working at once. Each acts as the roles it is given, narrowed to scopes as an API token's can be, so it can only reach the assets those allow. What it changes is kept in the asset's history under its name. It takes so many requests a minute, and no more.
