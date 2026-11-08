package notify

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// summary is what a notification says, whatever carries it: a title, a
// description, a link to what it is about, and a color for how much it
// matters. Discord shows it as an embed and email as a message.
type summary struct {
	Title, Description, URL string
	Color                   int
}

// summarize describes a notification. Text that comes from people (names,
// notes) passes through esc, which makes it safe for the medium: Discord
// escapes its markdown, while email's templates escape HTML themselves.
func summarize(n Notification, esc func(string) string) summary {
	e := n.Event
	p := e.Payload
	var out summary
	name := "a category"
	base := strings.TrimRight(n.BaseURL, "/")
	switch {
	case p.Category != nil:
		name = esc(truncate(p.Category.Name, 200))
		if base != "" {
			out.URL = base + "/categories/" + url.PathEscape(p.Category.ID)
		}
	case p.Asset != nil:
		name = esc(truncate(p.Asset.Name, 200))
		if base != "" {
			out.URL = base + "/assets/" + url.PathEscape(p.Asset.ID)
		}
	}
	by := ""
	if p.Actor != nil && p.Actor.Display() != "" {
		by = esc(p.Actor.Signature())
	}

	switch {
	case n.Test:
		out.Title = "Test message from BAM!"
		out.Description = "This channel is set up correctly."
		out.Color = discordTest
		out.URL = ""
	case e.Kind == models.EventRunningLow:
		out.Title = "Running low: " + name
		out.Color = categoryColor(p.Category)
		if p.Trigger == models.TriggerThreshold && p.ActiveCount != nil && p.Threshold != nil {
			level, threshold := categoryLevel(p, esc)
			out.Description = fmt.Sprintf("Down to %s, below the threshold of %s.", level, threshold)
			if by != "" {
				out.Description += " Last change by " + by + "."
			}
		} else if by != "" {
			out.Description = "Flagged as running low by " + by + "."
		} else {
			out.Description = "Flagged as running low."
		}
	case e.Kind == models.EventRestocked:
		out.Title = "Restocked: " + name
		out.Color = discordNeutral
		if p.Trigger == models.TriggerThreshold && p.ActiveCount != nil && p.Threshold != nil {
			level, threshold := categoryLevel(p, esc)
			out.Description = fmt.Sprintf("Back to %s, at or above the threshold of %s.", level, threshold)
			if by != "" {
				out.Description += " Last change by " + by + "."
			}
		} else if by != "" {
			out.Description = "Marked as restocked by " + by + "."
		} else {
			out.Description = "Marked as restocked."
		}
	case e.Kind == models.EventAssetRunningLow:
		out.Title = "Running low: " + name
		out.Color = discordWarning
		out.Description = assetDescription(p, by, esc, "Down to %s, below the threshold of %s.", "Flagged as running low")
	case e.Kind == models.EventAssetRestocked:
		out.Title = "Restocked: " + name
		out.Color = discordNeutral
		out.Description = assetDescription(p, by, esc, "Back to %s, at or above the threshold of %s.", "Marked as restocked")
	case e.Kind == models.EventAssetConditionChanged:
		to := ""
		if len(p.Changes) > 0 {
			to = p.Changes[0].To
		}
		switch to {
		case models.ConditionOutOfService:
			out.Title, out.Color = "Out of service: "+name, discordDanger
		case models.ConditionNeedsMaintenance:
			out.Title, out.Color = "Needs maintenance: "+name, discordWarning
		default:
			out.Title, out.Color = "Working again: "+name, discordNeutral
		}
		if p.Note != "" {
			out.Description = esc(truncate(p.Note, 1000))
		}
		if by != "" {
			if out.Description != "" {
				out.Description += "\n"
			}
			out.Description += "Reported by " + by + "."
		}
	case e.Kind == models.EventAssetOverdue:
		out.Title, out.Color = "Overdue: "+name, discordWarning
		who := "someone"
		if p.Borrower != nil {
			who = esc(truncate(p.Borrower.Display(), 200))
		}
		out.Description = "Lent to " + who
		if p.DueAt != nil {
			out.Description += ", due back " + p.DueAt.UTC().Format("2 January 2006") + " (UTC)"
		}
		out.Description += "."
	case e.Kind == models.EventAuthThrottled && p.Throttle != nil:
		// About no asset or category: the link is to the people page.
		out.URL = ""
		if base != "" {
			out.URL = base + "/users"
		}
		out.Color = discordDanger
		until := p.Throttle.Until.UTC().Format("15:04 on 2 January 2006") + " (UTC)"
		if p.Throttle.User != nil {
			out.Title = "Sign-ins held back: " + esc(truncate(p.Throttle.User.Display(), 200))
			out.Description = "Too many wrong passwords or codes for their account. Signing in waits until " + until +
				", longer with each further wrong one. Someone may be guessing it."
		} else {
			out.Title = "Sign-ins held back from " + esc(truncate(p.Throttle.Address, 64))
			out.Description = "Too many wrong passwords or codes from this address, across accounts. It may sign in again at " + until + "."
		}
	case e.Kind == models.EventAuthFactorsReset && p.Account != nil:
		out.URL = ""
		if base != "" {
			out.URL = base + "/users"
		}
		out.Color = discordWarning
		out.Title = "Second factors removed: " + esc(truncate(p.Account.Display(), 200))
		out.Description = "They sign in with their password alone until they set one up again, and were signed out everywhere."
		if by != "" {
			out.Description += " Removed by " + by + "."
		} else {
			out.Description += " Removed on the command line."
		}
	case e.Kind == models.EventAuthRecoveryCodeUsed && p.Account != nil:
		out.URL = ""
		if base != "" {
			out.URL = base + "/users"
		}
		out.Color = discordWarning
		out.Title = "Recovery code used: " + esc(truncate(p.Account.Display(), 200))
		out.Description = "They signed in with a recovery code in place of their authenticator app. If they did not, their codes may have been seen by someone else."
	case e.Kind == models.EventAuthBreakGlass && p.Account != nil:
		out.URL = ""
		if base != "" {
			out.URL = base + "/users"
		}
		out.Color = discordWarning
		out.Title = "Break-glass sign-in: " + esc(truncate(p.Account.Display(), 200))
		out.Description = "They signed in with a password while passwords are off, as only those who manage users can. If no provider is down, ask them why."
	case e.Kind == models.EventRoleTwoFactorOff:
		out.URL = ""
		if base != "" {
			out.URL = base + "/roles"
		}
		out.Color = discordWarning
		out.Title = "Two-factor sign-in no longer required: " + esc(truncate(p.Note, 200))
		by := "on the command line"
		if p.Actor != nil {
			by = "by " + esc(truncate(p.Actor.Display(), 200))
		}
		out.Description = "Whoever holds the role can now sign in with a password alone. It was turned off " + by + "."
	case e.Kind == models.EventNotifierDisabled && p.Channel != nil:
		// About no asset or category: the link is to the channels.
		out.URL = ""
		if base != "" {
			out.URL = base + "/notifications"
		}
		out.Color = discordDanger
		out.Title = "Notification channel turned off: " + esc(truncate(p.Channel.Name, 200))
		out.Description = esc(p.Note) + " Its recent deliveries say why; turn it on again once it is fixed."
	case (e.Kind == models.EventRelayOnline || e.Kind == models.EventRelayOffline) && p.Relay != nil:
		// About no asset or category: the link is to the relays.
		out.URL = ""
		if base != "" {
			out.URL = base + "/relays"
		}
		relay := esc(truncate(p.Relay.Name, 200))
		if e.Kind == models.EventRelayOnline {
			out.Title, out.Color = "Relay online: "+relay, discordNeutral
			out.Description = "It is linked to BAM! again, and what it serves can be reached."
		} else {
			out.Title, out.Color = "Relay offline: "+relay, discordWarning
			out.Description = "It is not linked to BAM! over any transport. The printers and devices it serves cannot be reached until it is back."
		}
	case (e.Kind == models.EventInstanceUpgraded || e.Kind == models.EventInstanceUpdateFailed || e.Kind == models.EventInstanceUpdateAvailable) && p.Instance != nil:
		out = instanceSummary(e.Kind, p.Instance, base, esc)
	case e.Kind == models.EventFlowOverdue:
		out.Title, out.Color = "Past its date: "+name, discordWarning
		out.Description = "Left"
		if p.Flow != nil {
			out.Description += " through " + esc(truncate(p.Flow.Name, 200))
		}
		if p.DueAt != nil {
			out.Description += ", to be collected by " + p.DueAt.UTC().Format("2 January 2006")
		}
		out.Description += "."
	case e.Kind == models.EventCommentAdded && p.Comment != nil:
		out.Color = discordNeutral
		on := name
		switch p.Comment.On {
		case models.CommentOnLoan:
			on = "the loan of " + name
		case models.CommentOnFiling:
			on = name + ", as filed"
			if p.Comment.Flow != nil {
				on += " by " + esc(truncate(p.Comment.Flow.Name, 200))
			}
		}
		out.Title = "Comment on " + on
		names := map[string]string{}
		for _, m := range p.Comment.Mentions {
			names[m.ID] = m.Name
		}
		out.Description = esc(truncate(markdown.PlainMentions(p.Comment.Text, func(id string) string { return names[id] }), 1000))
		if by != "" {
			out.Description += "\nBy " + by + "."
		}
		if out.URL != "" {
			out.URL += "#comment-" + url.PathEscape(p.Comment.ID)
		}
	case e.Kind == models.EventWarrantyExpiring:
		out.Title, out.Color = "Warranty ending: "+name, discordWarning
		out.Description = "Its warranty is ending soon."
		if p.DueAt != nil {
			out.Description = "Its warranty ends on " + p.DueAt.UTC().Format("2 January 2006") + "."
		}
	case (e.Kind == models.EventContractExpiring || e.Kind == models.EventContractExpired) && p.Contract != nil:
		out = contractSummary(e.Kind, p, base, esc)
	case (e.Kind == models.EventOrderOrdered || e.Kind == models.EventOrderReceived) && p.PurchaseOrder != nil:
		out = orderSummary(e.Kind, p, base, esc)
		if by != "" {
			out.Description += " By " + by + "."
		}
	case e.Kind == models.EventAssetHook:
		label := "Hook"
		if p.Hook != nil {
			label = p.Hook.Label
		}
		out.Title, out.Color = esc(truncate(label, 100))+": "+name, discordNeutral
		switch {
		case p.Hook != nil && p.Hook.Trigger == models.HookTriggerSchedule:
			out.Description = "Fired on its schedule."
		case p.Hook != nil && p.Hook.Trigger == models.HookTriggerEvent && p.Hook.Cause != nil:
			out.Description = "Fired " + hookCause(p.Hook.Cause.Kind) + "."
		case by != "":
			out.Description = "Pressed by " + by + "."
		}
	default:
		out.Title = truncate(e.Kind, 200)
		out.Color = discordNeutral
	}
	return out
}

// instanceSummary describes a change of the release BAM! runs. It is about
// no asset or category: the link is to the release's notes, or else to
// the instance, which the message names too.
func instanceSummary(kind string, in *models.EventInstance, base string, esc func(string) string) summary {
	var out summary
	from, to := esc(truncate(in.From, 64)), esc(truncate(in.To, 64))
	var first string
	switch kind {
	case models.EventInstanceUpgraded:
		if in.Downgrade {
			out.Title, out.Color = "BAM! downgraded to "+to, discordWarning
			first = "It ran " + from + " before. An older release may not work with a database a newer one has migrated; " +
				"if it does not, restore the backup taken before the upgrade."
		} else {
			out.Title, out.Color = "BAM! upgraded to "+to, discordNeutral
			first = "It ran " + from + " before."
		}
		switch {
		case in.SchemaFrom > 0 && in.SchemaTo != in.SchemaFrom:
			first += fmt.Sprintf(" Its database went from schema %d to %d.", in.SchemaFrom, in.SchemaTo)
		case in.SchemaFrom > 0:
			first += " Its database's schema did not change."
		}
	case models.EventInstanceUpdateFailed:
		out.Title, out.Color = "Update to "+to+" rolled back", discordDanger
		why := "it did not come up"
		if in.Reason != "" {
			why = esc(truncate(in.Reason, 500))
		}
		first = "bam-update installed " + to + ", but " + why + ", so it put " + from + " back."
		if in.Restored {
			first += " The database was restored from the backup taken before the update."
		} else {
			first += " The database was not restored from a backup, so any changes " + to + " made to it remain."
		}
	default:
		out.Title, out.Color = "BAM! "+to+" is available", discordTest
		first = "This instance runs " + from + "."
	}
	lines := []string{first}
	if in.NotesURL != "" {
		lines = append(lines, "Release notes: "+in.NotesURL)
	}
	if in.UpdatingURL != "" {
		lines = append(lines, "Updating: "+in.UpdatingURL)
	}
	if base != "" {
		lines = append(lines, "The instance: "+base)
	}
	out.Description = strings.Join(lines, "\n")
	out.URL = in.NotesURL
	if out.URL == "" {
		out.URL = base
	}
	return out
}

// contractSummary describes a contract about to end, or whose notice
// deadline is near, and one ending (docs/contracts.md). The link is to the
// contract.
func contractSummary(kind string, p models.EventPayload, base string, esc func(string) string) summary {
	c := p.Contract
	var out summary
	if base != "" {
		out.URL = base + "/contracts/" + url.PathEscape(c.ID)
	}
	name := esc(truncate(c.Name, 200))
	with := ""
	if c.VendorName != "" {
		with = " with " + esc(truncate(c.VendorName, 200))
	}
	what := "Its " + c.Kind + " contract" + with
	if c.Kind == models.ContractWarranty {
		what = "Its warranty" + with
	}
	day := func(s string) string {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return s
		}
		return t.Format("2 January 2006")
	}
	due := day(c.EndDate)
	if p.DueAt != nil {
		due = p.DueAt.UTC().Format("2 January 2006")
	}
	switch {
	case kind == models.EventContractExpired && c.AutoRenews:
		out.Title, out.Color = "Contract renewed: "+name, discordNeutral
		out.Description = what + " reached the end of its term on " + day(c.EndDate) + ", and renews automatically. Give it its new end date."
	case kind == models.EventContractExpired:
		out.Title, out.Color = "Contract ended: "+name, discordDanger
		out.Description = what + " ended on " + day(c.EndDate) + "."
	case c.AutoRenews && c.NoticeDays > 0:
		out.Title, out.Color = "Contract notice due: "+name, discordWarning
		out.Description = what + " renews automatically on " + day(c.EndDate) + " unless notice is given by " + due + "."
	case c.AutoRenews:
		out.Title, out.Color = "Contract renewing: "+name, discordWarning
		out.Description = what + " renews automatically on " + due + "."
	default:
		out.Title, out.Color = "Contract ending: "+name, discordWarning
		out.Description = what + " ends on " + due + "."
	}
	return out
}

// orderSummary describes a purchase order placed, or a delivery received
// against one (docs/purchase-orders.md): its number, its vendor, and each
// line's packs, never a price. The link is to the order.
func orderSummary(kind string, p models.EventPayload, base string, esc func(string) string) summary {
	o := p.PurchaseOrder
	var out summary
	if base != "" {
		out.URL = base + "/orders/" + url.PathEscape(o.ID)
	}
	name := esc(truncate(o.Number, 200))
	from := ""
	if o.VendorName != "" {
		from = esc(truncate(o.VendorName, 200))
	}
	var lines []string
	for i, l := range p.OrderLines {
		if i == 10 {
			lines = append(lines, fmt.Sprintf("and %d more", len(p.OrderLines)-10))
			break
		}
		text := strings.TrimSpace(l.PartNumber + " " + l.Description)
		lines = append(lines, fmt.Sprintf("%d × %s", l.Quantity, esc(truncate(text, 200))))
	}
	if kind == models.EventOrderOrdered {
		out.Title, out.Color = "Order placed: "+name, discordNeutral
		out.Description = "Placed with " + from + "."
	} else {
		out.Title, out.Color = "Delivery received: "+name, discordNeutral
		out.Description = "Received from " + from + "."
	}
	if from == "" {
		out.Description = strings.Replace(strings.Replace(out.Description, " with .", ".", 1), " from .", ".", 1)
	}
	if len(lines) > 0 {
		out.Description += " " + strings.Join(lines, "; ") + "."
	}
	return out
}

// hookCause says what event fired a hook, as "Fired ..." ends.
func hookCause(kind string) string {
	switch kind {
	case models.EventAssetCreated:
		return "as it was added"
	case models.EventAssetEdited:
		return "as it was edited"
	case models.EventAssetRestored:
		return "as it was restored"
	case models.EventQuantityChanged:
		return "as its quantity changed"
	case models.EventAssetRunningLow:
		return "as it ran low"
	case models.EventAssetRestocked:
		return "as it was restocked"
	case models.EventAssetConditionChanged:
		return "as its condition changed"
	case models.EventAssetCheckedOut:
		return "as it was lent"
	case models.EventAssetCheckedIn:
		return "as it was returned"
	case models.EventAssetOverdue:
		return "as its loan became overdue"
	case models.EventFlowOverdue:
		return "as it passed its date"
	case models.EventAssetLabelPrinted:
		return "as its label was printed"
	}
	return "by an event"
}
