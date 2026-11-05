package notify

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Loan reminders (docs/stock-and-lending.md#loan-reminders) go out with the
// worker's turn: each due is claimed in the database before it is sent, so
// servers sharing it send each once, and given back if sending fails, to be
// tried again.

// SendLoanReminders sends the loan reminders due now, when the instance
// sends them and email is set up, answering how many it sent.
func (w *Worker) SendLoanReminders(ctx context.Context) (int, error) {
	w.defaults()
	if w.Mailer == nil {
		return 0, nil
	}
	set, err := w.Store.LoanReminders()
	if err != nil || !set.On {
		return 0, err
	}
	now := w.Now()
	due, err := w.Store.DueLoanReminders(now, set)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, r := range due {
		claimed, err := w.Store.ClaimLoanReminder(r.LoanID, r.Reminder, now)
		if err != nil {
			return sent, err
		}
		if !claimed {
			continue
		}
		msg, err := reminderMessage(r, strings.TrimRight(w.BaseURL, "/"), now)
		if err == nil {
			sctx, cancel := context.WithTimeout(ctx, w.SendTimeout)
			err = w.Mailer.Send(sctx, msg)
			cancel()
		}
		if err != nil {
			log.Printf("loan reminders: emailing %s about %s: %v", r.Borrower.Email, r.AssetName, err)
			if rerr := w.Store.ReleaseLoanReminder(r.LoanID, r.Reminder); rerr != nil {
				return sent, rerr
			}
			continue
		}
		sent++
	}
	return sent, nil
}

// reminderDate is a day as a reminder writes it, "Monday, November 9".
func reminderDate(t time.Time) string { return t.UTC().Format("Monday, January 2, 2006") }

// reminderMessage is a reminder's email: what is borrowed, when it was due,
// who lent it, and links to the asset and to returning it by scan.
func reminderMessage(r store.LoanReminder, base string, now time.Time) (email.Message, error) {
	lender := "the space"
	if r.LentBy != nil {
		lender = r.LentBy.Name
		if lender == "" {
			lender = r.LentBy.Email
		}
	}
	borrowed := fmt.Sprintf("You borrowed %s from %s on %s.", r.AssetName, lender, reminderDate(r.CheckedOutAt))
	var subject, accent string
	var paragraphs []string
	switch {
	case !r.Overdue():
		subject, accent = r.AssetName+" is due back "+reminderDate(r.DueAt), "#2563eb"
		paragraphs = []string{borrowed, "It is due back " + reminderDate(r.DueAt) + "."}
	case r.Reminder == "overdue-0":
		subject, accent = r.AssetName+" is overdue", "#dc2626"
		paragraphs = []string{borrowed, "It was due back " + reminderDate(r.DueAt) + ", and is now overdue. Please bring it back."}
	default:
		days := int(now.Sub(r.DueAt) / (24 * time.Hour))
		subject, accent = r.AssetName+" is still overdue", "#dc2626"
		paragraphs = []string{borrowed, fmt.Sprintf("It was due back %s, %d days ago. Please bring it back.", reminderDate(r.DueAt), days)}
	}
	link, label := "", ""
	if base != "" {
		page := base + "/assets/" + url.PathEscape(r.AssetID)
		if r.Code != "" {
			link, label = base+"/scan/"+url.PathEscape(r.Code), "Return it"
			paragraphs = append(paragraphs, "Return it by scanning its code, or with the button below. Its page: "+page)
		} else {
			link, label = page, "Its page"
			paragraphs = append(paragraphs, "Return it from its page.")
		}
	}
	footer := "Sent to " + r.Borrower.Email + " about a loan."
	if base != "" {
		footer += " Turn loan reminders off under Your account: " + base + "/account"
	}
	html, text, err := email.Render(email.Content{Title: subject, Paragraphs: paragraphs, Link: link, LinkLabel: label, Accent: accent, Footer: footer})
	if err != nil {
		return email.Message{}, err
	}
	return email.Message{To: []string{r.Borrower.Email}, Subject: subject, HTML: html, Text: text}, nil
}
