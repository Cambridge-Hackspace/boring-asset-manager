package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// Mentions (docs/comments.md#mentions): a comment names a person with
// <@id>, by their user ID, so that the mention follows them when they are
// renamed. Each person a comment mentions is recorded once, in
// comment_mentions, when the comment or an edit of it first mentions them;
// the notify worker emails them from there, once, if they may read what the
// comment is on and have not turned mentions off.

// recordMentionsTx records the people a comment's text mentions who are
// not yet recorded for it, leaving out whoever wrote it, and answers
// everyone it mentions, with their names, for its event. IDs that name no
// one are left as they are, mentioning no one.
func (s *Store) recordMentionsTx(tx *sql.Tx, commentID, body string) ([]models.EventMention, error) {
	ids := markdown.Mentions(body)
	if len(ids) == 0 {
		return nil, nil
	}
	known, err := s.peopleTx(tx, ids)
	if err != nil {
		return nil, err
	}
	recorded := map[string]bool{}
	rows, err := s.TxQuery(tx, "SELECT user_id FROM comment_mentions WHERE comment_id = ?", commentID)
	if err != nil {
		return nil, fmt.Errorf("reading the comment's mentions: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		recorded[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	author, _ := s.actorID().(string)
	now := dbTime(time.Now())
	var out []models.EventMention
	for _, id := range ids {
		p, ok := known[id]
		if !ok {
			continue
		}
		out = append(out, models.EventMention{ID: id, Name: p.Name})
		if recorded[id] || id == author {
			continue
		}
		if _, err := s.TxExec(tx, "INSERT INTO comment_mentions (comment_id, user_id, mentioned_at) VALUES (?, ?, ?)", commentID, id, now); err != nil {
			return nil, fmt.Errorf("recording a mention: %w", err)
		}
	}
	return out, nil
}

// peopleTx reads the users of these IDs, by ID: archived ones too, as
// their names still show.
func (s *Store) peopleTx(tx *sql.Tx, ids []string) (map[string]models.Person, error) {
	out := map[string]models.Person{}
	if len(ids) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := "SELECT id, email, name, archived_at FROM users WHERE id IN (" + marks + ")"
	var rows *sql.Rows
	var err error
	if tx != nil {
		rows, err = s.TxQuery(tx, query, args...)
	} else {
		rows, err = s.Query(query, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the people mentioned: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p models.Person
		var archived sql.NullTime
		if err := rows.Scan(&p.ID, &p.Email, &p.Name, &archived); err != nil {
			return nil, err
		}
		p.Archived = archived.Valid
		out[p.ID] = p
	}
	return out, rows.Err()
}

// UnnamedUser stands for a person who has no name yet, as asset pages
// show them: never by their email.
const UnnamedUser = "Unnamed user"

// PersonName is how a person shows where people see who: their name, or
// UnnamedUser.
func PersonName(p models.Person) string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return UnnamedUser
}

// MentionNames are the names, now, of the people with these IDs, as
// PersonName gives them; an ID naming no one is left out.
func (s *Store) MentionNames(ids []string) (map[string]string, error) {
	people, err := s.peopleTx(nil, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(people))
	for id, p := range people {
		out[id] = PersonName(p)
	}
	return out, nil
}

// CommentMentions lists the people recorded as mentioned by a comment, by
// user ID, in order.
func (s *Store) CommentMentions(commentID string) ([]string, error) {
	rows, err := s.Query("SELECT user_id FROM comment_mentions WHERE comment_id = ? ORDER BY user_id", commentID)
	if err != nil {
		return nil, fmt.Errorf("reading the comment's mentions: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CommentScopes are the scopes of what a comment is on: a category's, or
// the asset's, for a comment on it, its loan or its filing.
func (s *Store) CommentScopes(c *models.Comment) ([]string, error) {
	if c.On == models.CommentOnCategory {
		return s.ScopesForCategories([]string{c.OnID})
	}
	scopes, err := s.AssetScopes([]string{c.AssetID})
	if err != nil {
		return nil, err
	}
	return scopes[c.AssetID], nil
}

// Reader is what a person may see of what a comment is on: whether they
// read it, and whether they see who, the people around it.
type Reader struct {
	Reads, SeesWho bool
}

// readerOf decides, from a person's permissions, what they may see of
// what a comment is on, by its kind and scopes, as the API decides for a
// request: a category's comments go with the catalog, the rest with the
// asset.
func readerOf(perms map[string]bool, on string, scopes []string) Reader {
	r := Reader{SeesWho: rbac.Allows(perms, rbac.PermAssetsTrack, scopes)}
	if on == models.CommentOnCategory {
		r.Reads = rbac.HasPermission(perms, rbac.PermAssetsRead)
	} else {
		r.Reads = rbac.Allows(perms, rbac.PermAssetsRead, scopes)
	}
	return r
}

// personPermissions are what a signed-in person holds: their roles', and
// the public role's, which everyone holds.
func (s *Store) personPermissions(userID string, public map[string]bool) (map[string]bool, error) {
	perms, err := s.GetUserPermissions(userID)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = map[string]bool{}
	}
	for k, v := range public {
		if v {
			perms[k] = true
		}
	}
	return perms, nil
}

// CommentReader decides what a person may see of what a comment is on,
// with its kind and scopes. An archived or inactive user holds nothing but
// the public role's.
func (s *Store) CommentReader(userID, on string, scopes []string) (Reader, error) {
	public, err := s.PublicPermissions()
	if err != nil {
		return Reader{}, err
	}
	perms, err := s.personPermissions(userID, public)
	if err != nil {
		return Reader{}, err
	}
	return readerOf(perms, on, scopes), nil
}

// PeopleWhoRead lists the active people who may read what a comment, of
// this kind and in these scopes, is on, by name, each named as PersonName
// names them: those a comment there may usefully mention.
func (s *Store) PeopleWhoRead(on string, scopes []string) ([]models.CommentMention, error) {
	people, err := s.ListPeople()
	if err != nil {
		return nil, err
	}
	public, err := s.PublicPermissions()
	if err != nil {
		return nil, err
	}
	out := []models.CommentMention{}
	for _, p := range people {
		perms, err := s.personPermissions(p.ID, public)
		if err != nil {
			return nil, err
		}
		if readerOf(perms, on, scopes).Reads {
			out = append(out, models.CommentMention{ID: p.ID, Name: PersonName(p)})
		}
	}
	return out, nil
}

// PendingMention is a mention not yet dealt with: the comment, and the
// person it mentions, with whether they may be emailed at all.
type PendingMention struct {
	CommentID   string
	MentionedAt time.Time
	Person      models.Person
	// Emailed is whether the person may be emailed about mentions: an
	// active user who has not turned them off.
	Emailed bool
}

// DueMentions lists the mentions not yet dealt with, made since since,
// oldest first. Older ones are left: a mention is news only for so long.
func (s *Store) DueMentions(since time.Time) ([]PendingMention, error) {
	rows, err := s.Query(`SELECT m.comment_id, m.mentioned_at, u.id, u.email, u.name, u.is_active, u.archived_at, u.mention_emails
		FROM comment_mentions m JOIN users u ON u.id = m.user_id
		WHERE m.notified_at IS NULL AND m.mentioned_at >= ?
		ORDER BY m.mentioned_at, m.comment_id, m.user_id`, dbTime(since))
	if err != nil {
		return nil, fmt.Errorf("reading the mentions due: %w", err)
	}
	defer rows.Close()
	var out []PendingMention
	for rows.Next() {
		var m PendingMention
		var active, on bool
		var archived sql.NullTime
		if err := rows.Scan(&m.CommentID, &m.MentionedAt, &m.Person.ID, &m.Person.Email, &m.Person.Name, &active, &archived, &on); err != nil {
			return nil, err
		}
		m.MentionedAt = m.MentionedAt.UTC()
		m.Person.Archived = archived.Valid
		m.Emailed = active && !archived.Valid && on
		out = append(out, m)
	}
	return out, rows.Err()
}

// ClaimMention claims a mention to deal with it, answering false when
// another server already has: it is marked dealt with at now.
func (s *Store) ClaimMention(commentID, userID string, now time.Time) (bool, error) {
	res, err := s.Exec("UPDATE comment_mentions SET notified_at = ? WHERE comment_id = ? AND user_id = ? AND notified_at IS NULL", dbTime(now), commentID, userID)
	if err != nil {
		return false, fmt.Errorf("claiming a mention: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ReleaseMention gives a claimed mention back, when emailing it failed, to
// be tried again.
func (s *Store) ReleaseMention(commentID, userID string) error {
	if _, err := s.Exec("UPDATE comment_mentions SET notified_at = NULL WHERE comment_id = ? AND user_id = ?", commentID, userID); err != nil {
		return fmt.Errorf("releasing a mention: %w", err)
	}
	return nil
}

// CommentPlaceName names what a comment is on, as a notice of it says: the
// category, the asset, "the loan of" the asset, or the asset "as filed by"
// the flow.
func (s *Store) CommentPlaceName(c *models.Comment) (string, error) {
	var name string
	var err error
	if c.On == models.CommentOnCategory {
		err = s.QueryRow("SELECT name FROM categories WHERE id = ?", c.OnID).Scan(&name)
	} else {
		err = s.QueryRow("SELECT name FROM assets WHERE id = ?", c.AssetID).Scan(&name)
	}
	if err != nil {
		return "", fmt.Errorf("reading what the comment is on: %w", err)
	}
	switch c.On {
	case models.CommentOnLoan:
		return "the loan of " + name, nil
	case models.CommentOnFiling:
		var flow string
		if err := s.QueryRow("SELECT name FROM flows WHERE id = ?", c.FlowID).Scan(&flow); err != nil {
			return "", fmt.Errorf("reading the flow: %w", err)
		}
		return name + ", as filed by " + flow, nil
	}
	return name, nil
}

// SetUserMentionEmails sets whether a person is emailed when mentioned.
// Like the theme, it is their preference, so updated_at is left as it is.
// Archived users cannot change it.
func (s *Store) SetUserMentionEmails(userID string, on bool) (*models.User, error) {
	if _, err := s.Exec("UPDATE users SET mention_emails = ? WHERE id = ? AND archived_at IS NULL", on, userID); err != nil {
		return nil, fmt.Errorf("setting the user's mention emails: %w", err)
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
