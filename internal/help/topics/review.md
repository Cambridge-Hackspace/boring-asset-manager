# Review

When many people record what is in their area, or an assistant drafts records for someone, review states tell what has been checked from what has not, and ask for records to be checked again before they go stale. They are off until a category turns them on, on its **Review** tab; an asset in no category that uses them has none.

## The states

- **Draft**: recorded, not yet checked. Anything added is a draft, unless whoever adds it may confirm it and ticks **Confirm it**.
- **Confirmed**: someone responsible for it says it is right. **Confirm** on the asset's page needs the *Confirm assets* permission.
- **Verified**: checked again by someone else, such as whoever keeps the inventory. **Verify** needs the *Verify assets* permission, and so does **Return to draft**, which asks what is wrong with it.

Someone cannot verify what they confirmed themselves, unless the category allows it. Each move is kept in the asset's history, with who made it and what they said.

## A category's settings

Each setting is the category's own, or taken from the category above it, as fields are:

- **Review states**: on or off for its assets and those below it.
- **Review every**: how many months after an asset is confirmed or verified it is due to be checked again, or no review dates.
- **Editing a confirmed or verified asset**: returns it to draft, unless the category says it keeps its state.
- **Whoever confirmed it verifying it**: allowed, or someone else verifies.

## Reviews due

An asset's review is **due** a number of days before its date, 14 unless your admin changes it under **Notifications**, and **overdue** after it. It shows on the asset's page, and the home page's **To review** shows those who can confirm how many are due, with drafts to confirm, assets waiting to be verified and assistants' changes waiting. A notification channel can announce *An asset's review falls due*, once for each date. **Confirm again** or **Verify again** starts the next period.

The asset list's **Review** filter finds drafts, confirmed or verified assets, reviews due or overdue, and assistant changes. Reports and labels can show `{{asset.review_state}}`, `{{asset.review_due}}`, `{{asset.confirmed_at}}` and `{{asset.confirmed_by}}`. Exports have a `review_state` column; an import ignores it, and what it adds is a draft unless you tick **Confirm the assets it adds**.

## Assistants' changes

Changes made through an assistant land as drafts, unless your admin turns that off: the assistant proposes, and a person decides. The asset's page says what changed it, with:

- **Accept**: the changes become yours, and the asset is as your own edit would have left it.
- **Accept and confirm**: the same, and confirmed, if you may confirm it.
- **Reject**: puts the asset back as it was before the changes, in the state it had, or archives an asset the assistant added. Its attachments and quantity stay as they are.

Editing an asset with an assistant's changes waiting accepts them. An assistant cannot confirm, verify or accept anything itself.

Flows can file what they add as confirmed, and a Change step can confirm.
