package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Loan reminders (docs/stock-and-lending.md#loan-reminders): an email to a
// borrower before a loan is due, the day it becomes overdue, and weekly
// after, up to a limit.

// The instance's settings for loan reminders.
const (
	// LoanRemindersSetting is "on" when reminders are sent; off by default.
	LoanRemindersSetting = "loans.reminders"
	// LoanReminderDaysSetting is how many days before a loan is due its
	// reminder goes; 0 sends none before.
	LoanReminderDaysSetting = "loans.reminder_days"
	// LoanReminderOverdueSetting is how many reminders an overdue loan
	// gets: the day it becomes overdue, and weekly after.
	LoanReminderOverdueSetting = "loans.reminder_overdue_times"
)

// ErrInvalidSetting is wrapped by a setting out of its range.
var ErrInvalidSetting = errors.New("invalid setting")

// LoanReminderSettings are the instance's choices.
type LoanReminderSettings struct {
	On           bool `json:"on"`
	DaysBefore   int  `json:"days_before"`
	OverdueTimes int  `json:"overdue_times"`
}

// The defaults, and the most each setting takes.
const (
	DefaultReminderDays    = 1
	DefaultReminderOverdue = 4
	MaxReminderDays        = 30
	MaxReminderOverdue     = 52
)

// LoanReminders reads the instance's settings.
func (s *Store) LoanReminders() (LoanReminderSettings, error) {
	out := LoanReminderSettings{DaysBefore: DefaultReminderDays, OverdueTimes: DefaultReminderOverdue}
	var err error
	if out.On, err = s.On(LoanRemindersSetting); err != nil {
		return out, err
	}
	for key, into := range map[string]*int{LoanReminderDaysSetting: &out.DaysBefore, LoanReminderOverdueSetting: &out.OverdueTimes} {
		v, err := s.Setting(key, "")
		if err != nil {
			return out, err
		}
		if n, err := strconv.Atoi(v); err == nil {
			*into = n
		}
	}
	return out, nil
}

// SetLoanReminders saves the instance's settings.
func (s *Store) SetLoanReminders(in LoanReminderSettings) error {
	if in.DaysBefore < 0 || in.DaysBefore > MaxReminderDays {
		return fmt.Errorf("%w: days before must be 0 to %d", ErrInvalidSetting, MaxReminderDays)
	}
	if in.OverdueTimes < 0 || in.OverdueTimes > MaxReminderOverdue {
		return fmt.Errorf("%w: overdue reminders must be 0 to %d", ErrInvalidSetting, MaxReminderOverdue)
	}
	on := "off"
	if in.On {
		on = "on"
	}
	for key, value := range map[string]string{LoanRemindersSetting: on, LoanReminderDaysSetting: strconv.Itoa(in.DaysBefore),
		LoanReminderOverdueSetting: strconv.Itoa(in.OverdueTimes)} {
		if err := s.SetSetting(key, value); err != nil {
			return err
		}
	}
	return nil
}

// ReminderDue is the reminder a loan due at due should have had by now,
// and whether one is due at all: "due" from daysBefore days before it is
// due, then "overdue-0" from when it is due, and "overdue-1" and on a week
// apart, up to times of them. Only the latest is due, so a server stopped
// for a while sends one, not each it missed.
func ReminderDue(due, now time.Time, daysBefore, times int) (string, bool) {
	if now.Before(due) {
		if daysBefore > 0 && !now.Before(due.Add(-time.Duration(daysBefore)*24*time.Hour)) {
			return "due", true
		}
		return "", false
	}
	week := int(now.Sub(due) / (7 * 24 * time.Hour))
	if week >= times {
		return "", false
	}
	return "overdue-" + strconv.Itoa(week), true
}

// LoanReminder is a reminder due for a loan, with what its email says.
type LoanReminder struct {
	LoanID    string
	Reminder  string
	AssetID   string
	AssetName string
	// Code is one of the asset's codes, for returning it by scan; empty
	// when it has none.
	Code         string
	Borrower     models.Person
	LentBy       *models.Person
	CheckedOutAt time.Time
	DueAt        time.Time
}

// Overdue reports whether the reminder is for a loan past due.
func (r LoanReminder) Overdue() bool { return r.Reminder != "due" }

// DueLoanReminders lists the reminders due now that have not been sent:
// for open loans with a due date, to active borrowers who have not turned
// reminders off.
func (s *Store) DueLoanReminders(now time.Time, set LoanReminderSettings) ([]LoanReminder, error) {
	rows, err := s.Query(`SELECT l.id, l.asset_id, a.name, l.checked_out_at, l.due_at, b.id, b.email, b.name, lb.id, lb.email, lb.name,
		(SELECT MIN(g.assigned_code) FROM asset_generator_keys g WHERE g.asset_id = a.id)
		FROM asset_loans l
		JOIN assets a ON a.id = l.asset_id
		JOIN users b ON b.id = l.borrower_id
		LEFT JOIN users lb ON lb.id = l.lent_by
		WHERE l.checked_in_at IS NULL AND l.due_at IS NOT NULL AND a.archived_at IS NULL
			AND b.is_active = TRUE AND b.archived_at IS NULL AND b.loan_reminders = TRUE AND b.email <> ''
		ORDER BY l.due_at, l.id`)
	if err != nil {
		return nil, fmt.Errorf("listing loans to remind: %w", err)
	}
	var due []LoanReminder
	for rows.Next() {
		var r LoanReminder
		var lbID, lbEmail, lbName, code sql.NullString
		if err := rows.Scan(&r.LoanID, &r.AssetID, &r.AssetName, &r.CheckedOutAt, &r.DueAt, &r.Borrower.ID, &r.Borrower.Email, &r.Borrower.Name,
			&lbID, &lbEmail, &lbName, &code); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning a loan to remind: %w", err)
		}
		if lbID.Valid {
			r.LentBy = &models.Person{ID: lbID.String, Email: lbEmail.String, Name: lbName.String}
		}
		r.Code = code.String
		var ok bool
		if r.Reminder, ok = ReminderDue(r.DueAt, now, set.DaysBefore, set.OverdueTimes); ok {
			due = append(due, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(due) == 0 {
		return nil, err
	}

	// Those sent already are left out.
	sent := map[[2]string]bool{}
	srows, err := s.Query(`SELECT r.loan_id, r.reminder FROM loan_reminders r
		JOIN asset_loans l ON l.id = r.loan_id WHERE l.checked_in_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("listing reminders sent: %w", err)
	}
	defer srows.Close()
	for srows.Next() {
		var k [2]string
		if err := srows.Scan(&k[0], &k[1]); err != nil {
			return nil, fmt.Errorf("scanning a reminder sent: %w", err)
		}
		sent[k] = true
	}
	if err := srows.Err(); err != nil {
		return nil, err
	}
	out := due[:0]
	for _, r := range due {
		if !sent[[2]string{r.LoanID, r.Reminder}] {
			out = append(out, r)
		}
	}
	return out, nil
}

// ClaimLoanReminder claims a loan's reminder to send, answering false when
// another server claimed it first, so each is sent once.
func (s *Store) ClaimLoanReminder(loanID, reminder string, now time.Time) (bool, error) {
	if _, err := s.Exec("INSERT INTO loan_reminders (loan_id, reminder, sent_at) VALUES (?, ?, ?)", loanID, reminder, dbTime(now)); err != nil {
		if IsUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("claiming the reminder: %w", err)
	}
	return true, nil
}

// ReleaseLoanReminder gives a claimed reminder back, as when sending it
// failed, so it is tried again.
func (s *Store) ReleaseLoanReminder(loanID, reminder string) error {
	if _, err := s.Exec("DELETE FROM loan_reminders WHERE loan_id = ? AND reminder = ?", loanID, reminder); err != nil {
		return fmt.Errorf("releasing the reminder: %w", err)
	}
	return nil
}

// SetUserLoanReminders sets whether a borrower gets loan reminders. Like the
// theme, it is their preference, so updated_at is left as it is. Archived
// users cannot change it.
func (s *Store) SetUserLoanReminders(userID string, on bool) (*models.User, error) {
	if _, err := s.Exec("UPDATE users SET loan_reminders = ? WHERE id = ? AND archived_at IS NULL", on, userID); err != nil {
		return nil, fmt.Errorf("setting the user's loan reminders: %w", err)
	}
	u, err := s.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if u.ArchivedAt != nil {
		return nil, ErrUserNotFound
	}
	return u, nil
}
