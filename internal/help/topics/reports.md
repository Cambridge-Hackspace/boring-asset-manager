# Reports

A report finds assets with a filter, such as everything running low or everything lent out, and shows them as tables and charts. Open **Reports** to run one; it prints without the menus, and each table downloads as CSV or as an Excel workbook. **Download all as Excel** puts every table in one workbook. **PDF…** downloads the whole report as a PDF, with its charts, on Letter or A4, upright or turned, and, if you ask, each asset's photo and label beside it.

**PDF…** on the asset list also makes a **sheet per asset**, a page each with its photo, its codes drawn, its label, where it is, its condition, its loan, its fields and its recent history, for insurance or a machine's binder; **Sheet PDF** on an asset's page makes its own. Or it makes a **stocktake checklist**: the assets grouped by where they are, in order, so you can walk the rooms ticking each one found and writing down how many.

Those who manage reports create and edit them. A report can keep the assets bought from a vendor, those bought between two dates, or those whose warranty ends before a date, or those a contract covers. A table's columns use the same placeholders as labels, among them where an asset came from, such as `{{asset.purchase.price}}`, which is blank for those who may not see prices, the contract covering it, such as `{{asset.contract.end_date}}`, and how many assets need it through links such as *depends on*, `{{asset.needed_by_all}}`, to rank what would stop most if it did ([Links](/help/links)); a chart groups and counts assets, or shows stock used and loans made over time.

**Scan templates** are kept here too: what scanning an asset shows. See [Scanning](/help/scanning).
