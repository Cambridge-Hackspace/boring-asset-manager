# Orders

A **purchase order** is what your space orders from one of its vendors: lines drawn from the vendor's catalog or written freely, with how many packs of each and what each costs. Find them under **Orders**, in **Catalog**, newest first. Choose a state to see only those, or search by number, the vendor's reference, the vendor, or a line.

## Starting an order

- **New order** makes a draft: choose the vendor, then add lines **From the vendor's catalog**, which brings each entry's part number, pack and price, or **Add a line** for anything else, such as delivery.
- **Start orders from what is low**, on the **Orders** page or the home page's **Running low** list, suggests a draft for each vendor: everything running low with a preferred catalog entry, with enough packs to bring it back to its threshold. Untick what you don't want, change the packs, and choose **Create drafts**. What is already on an open order is listed apart, so nothing is ordered twice.
- A new order is delivered where the last one was, unless you change it.

An order has one currency. A catalog entry priced in another comes without its price.

## Placing, receiving and canceling

1. **Place order** marks a draft as sent to the vendor. **PDF** downloads it to send: to whom, where to deliver it, its lines and total.
2. **Receive…** opens the delivery. Each line still to come shows the packs arriving, all of them unless you change it, and what that does:
   - a line for something **counted by quantity** restocks it, by the packs times the pack's size unless you say otherwise;
   - a line for a **category** makes its assets, one per pack unless you say otherwise, **like** one already there (its fields, place, and the next codes in its series) or with a name you give. Each gets the order's vendor, dates, number and price in its **Purchase** section;
   - anything else is counted in.

   Tick **Add the new assets' labels to the print queue** to print them. An order received in parts is **Partly received** until the last comes.
3. **Cancel order** stops one that has been placed: what came stays, and the rest is not expected. A draft is deleted instead.

While an order is on its way you can still add lines, change quantities and prices, and set when it is expected; a line something has come of keeps at least what came. Once received or canceled, only its reference and notes change.

## Numbers

Orders are numbered one after another, shown as **PO-0012** unless whoever manages settings chose another prefix or padding, under **Admin › Import & export**. Changing it changes how every order is shown from then on; assets already received keep the order number they were given.

## Who can see and change them

An order follows its vendor: anyone who can see the vendor sees its orders. Its prices show only to those who may see prices, the **See prices** permission, in the vendor's scopes, or in every scope for a vendor in none.

Making, changing, placing, canceling and deleting orders takes the **Manage vendors** permission, in the vendor's scopes. Receiving takes **Record stock** for what it restocks, and **Edit assets** where the assets it makes go. See [People and permissions](/help/people).

A notification channel can tell you when an order is placed, or a delivery is received (see [Notifications](/help/notifications)).
