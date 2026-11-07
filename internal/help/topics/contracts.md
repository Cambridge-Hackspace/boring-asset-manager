# Contracts

**Contracts** are the agreements your space has with its vendors: warranties, service and maintenance agreements, leases and licenses. Find them under **Contracts**, in **Catalog**. The list is in order of when each ends, soonest first; choose the **Ends** heading to turn it around. Search it by a contract's name, kind, reference, vendor or notes.

## A contract

- Its **kind**: a warranty, a service or maintenance agreement, a lease, a license, or something else.
- Its **vendor**, from the **Vendors** list, its **name**, and the vendor's **reference** for it, such as an agreement number.
- When it **starts** and **ends**. One with no end runs until it is ended.
- Whether it **renews automatically**, and its **notice period**: how many days before its end you must tell the vendor to stop it renewing. Its page shows the **notice deadline** this gives.
- What it **costs**, in a currency, and how often that is paid: once, monthly, quarterly or yearly.
- What it **covers**: assets, and categories. A category covers every asset in it, and in the categories below it.
- **Attachments**: the contract itself, and anything else about it. They work as a vendor's do.
- **Notes**, written in Markdown, as a vendor's are.
- **History**: when it was made, each change to it, what it covers and its files, and the reminders below.

## Where contracts show

A vendor's page has a **Contracts** tab listing its contracts, with a link to make a new one. A category's page lists the contracts naming it. Each asset a contract covers says so on its page, such as *Under maintenance contract with Acme until June 2027*, with a link to the contract, whether the contract names the asset or one of its categories.

An asset's own **warranty end**, in its **Purchase** section, is the warranty it came with. An extended warranty, or one bought separately, is a contract of the kind **warranty**: both show on the asset's page, and each has its own reminder.

## Reminders

A notification channel can tell you when a contract is about to end, or, for one that renews automatically, when its notice deadline is near, so that you can decide in time; and again on the day it ends. Choose those events for the channel on the **Notifications** page (see [Notifications](/help/notifications)). How many days ahead you are told is set by whoever runs BAM!: 30 unless they changed it. Each reminder comes once; changing a contract's end gives it new ones.

A contract that renews automatically keeps the dates you gave it: once it has renewed, give it its new end.

## Reports and labels

A report can keep only the assets a contract covers, and show the contract's name, kind, vendor, reference, dates and cost with placeholders such as `{{asset.contract.name}}` and `{{asset.contract.end_date}}`. A report that keeps one contract's assets shows that contract; otherwise, and on labels, each asset shows the contract in force that covers it longest. See [Reports](/help/reports).

## Who can see and change them

A contract follows its vendor: anyone who can see the vendor sees its contracts. Of the assets a contract covers, you see those you can read. Its cost shows only to those who may see prices, the **See prices** permission, in the vendor's scopes, or in every scope for a vendor in none.

Making, changing and deleting contracts takes the **Manage vendors** permission, in the vendor's scopes. A vendor with contracts cannot be deleted until they are deleted, or moved to another vendor. See [People and permissions](/help/people).
