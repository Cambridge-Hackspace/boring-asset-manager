# Hooks

**Hooks** are buttons on assets, such as **Identify**. Pressing one triggers the hook, which tells whatever is listening for it: a cabinet that lights up the drawer the asset is in, or a channel on the **Notifications** page that posts it somewhere. Older releases called them actions.

## Triggering one

An asset's hooks are under **Hooks** on its page, and on its scan page when its scan template includes them. Some ask first before they are sent. Triggering needs the **Trigger hooks** permission. If a device answers, its answer shows under the button. An asset's hooks can be triggered once every couple of seconds, so a cabinet is not asked to light up again before it has finished.

Every hook triggered is kept in the asset's history.

## Setting them up

Whoever manages categories defines hooks on the **Hooks** page: a label people see, such as "Identify"; a key devices listen for, made from the label as you type it, such as `identify`, which **Change** sets otherwise; and, if wanted, a question to ask first. Devices, and flows that trigger the hook, know it by its key, so changing the key of a hook in use says so first.

Then, on a category's **Links and hooks** tab, choose which of them it offers on its assets, and whether on its subcategories' assets too.

## What a device is told

Each time, it says which hook, which asset, and every container it is in, outermost first, each with its codes and field values. So give each drawer a field such as **LED** with the number of its light, and triggering Identify on anything inside lights its drawer.

## Devices that listen

A device, such as a cabinet's controller, connects to BAM itself, so it needs no address of its own that BAM can reach. Give it an **API token** on the **API tokens** page, with a role holding **Receive events live** in the scopes its cabinet covers; it then opens a WebSocket to `/api/v1/events/live` with that token and is sent each hook triggered, and any other event it asks for, as it happens. It may answer each one, such as "Lit drawer A3" or "No such drawer", and the person who pressed the button sees the answer under it for a few seconds, or that nothing answered. The **API tokens** page shows which tokens are connected now.

A device that loses its connection should connect again; what happened while it was away is not sent again.
