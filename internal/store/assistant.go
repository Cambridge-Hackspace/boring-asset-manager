package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// The assistant beside the page (docs/assistant.md): the instance's
// provider and limits, the guides an admin writes, and each person's
// conversations, which no one else reads.

var (
	// ErrConversationNotFound is returned for a conversation that is not
	// there, or is someone else's: they read the same.
	ErrConversationNotFound = errors.New("conversation not found")
	// ErrGuideNotFound is returned for a guide that is not there.
	ErrGuideNotFound = errors.New("guide not found")
	// ErrInvalidGuide is wrapped by a guide that cannot be saved.
	ErrInvalidGuide = errors.New("invalid guide")
)

// The providers the assistant can talk to.
const (
	// AssistantAnthropic is Anthropic's Messages API.
	AssistantAnthropic = "anthropic"
	// AssistantOpenAI is any chat completions API that takes tools, as
	// OpenAI's does.
	AssistantOpenAI = "openai"
)

// AssistantSettings are the instance's choices for the assistant. Each
// limit of 0 is no limit.
type AssistantSettings struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	// APIKey is written and never returned; KeySet says whether there is
	// one.
	APIKey string `json:"-"`
	KeySet bool   `json:"key_set"`
	// Name is what the assistant is called, in the panel and in history.
	Name string `json:"name"`
	// MaxTokens is the most one reply from the model may take.
	MaxTokens int `json:"max_tokens"`
	// MaxSteps is the most calls to the model one message may lead to.
	MaxSteps int `json:"max_steps"`
	// MaxTools is the most tools offered, 0 for every one the person may
	// use.
	MaxTools int `json:"max_tools"`
	// The limits a day, per person and for the instance.
	PersonMessages   int   `json:"person_messages"`
	PersonTokens     int64 `json:"person_tokens"`
	InstanceMessages int   `json:"instance_messages"`
	InstanceTokens   int64 `json:"instance_tokens"`
	// KeepDays is how long a conversation is kept after its last message.
	KeepDays int `json:"keep_days"`
	// AcknowledgedBy and AcknowledgedAt are who turned the assistant on,
	// told what goes to the provider, and when.
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

// The assistant's defaults, and the bounds of its settings.
const (
	DefaultAssistantName             = "BAM! assistant"
	DefaultAssistantMaxTokens        = 16000
	DefaultAssistantMaxSteps         = 20
	DefaultAssistantMaxTools         = 128
	DefaultAssistantPersonMessages   = 100
	DefaultAssistantPersonTokens     = 2_000_000
	DefaultAssistantInstanceMessages = 1000
	DefaultAssistantInstanceTokens   = 20_000_000
	DefaultAssistantKeepDays         = 30
	MaxAssistantMaxTokens            = 128000
	MaxAssistantSteps                = 100
	MaxAssistantKeepDays             = 3650
	// MaxAssistantMessage is the longest message a person may send, in
	// characters.
	MaxAssistantMessage = 20000
)

// DefaultAssistantEndpoint is each provider's own address.
var DefaultAssistantEndpoint = map[string]string{
	AssistantAnthropic: "https://api.anthropic.com",
	AssistantOpenAI:    "https://api.openai.com/v1",
}

// DefaultAssistantModel is each provider's model unless one is chosen.
var DefaultAssistantModel = map[string]string{
	AssistantAnthropic: "claude-opus-5-5",
	AssistantOpenAI:    "gpt-5",
}

// DefaultAssistantSettings are the settings before an admin makes any.
func DefaultAssistantSettings() AssistantSettings {
	return AssistantSettings{
		Provider: AssistantAnthropic, Endpoint: DefaultAssistantEndpoint[AssistantAnthropic], Model: DefaultAssistantModel[AssistantAnthropic],
		Name: DefaultAssistantName, MaxTokens: DefaultAssistantMaxTokens, MaxSteps: DefaultAssistantMaxSteps, MaxTools: DefaultAssistantMaxTools,
		PersonMessages: DefaultAssistantPersonMessages, PersonTokens: DefaultAssistantPersonTokens,
		InstanceMessages: DefaultAssistantInstanceMessages, InstanceTokens: DefaultAssistantInstanceTokens,
		KeepDays: DefaultAssistantKeepDays,
	}
}

// Validate checks the settings, trimming their text.
func (a *AssistantSettings) Validate() error {
	a.Provider = strings.TrimSpace(a.Provider)
	a.Endpoint = strings.TrimSpace(a.Endpoint)
	a.Model = strings.TrimSpace(a.Model)
	a.Name = strings.TrimSpace(a.Name)
	switch a.Provider {
	case AssistantAnthropic, AssistantOpenAI:
	default:
		return fmt.Errorf("%w: the provider is %q or %q", ErrInvalidSetting, AssistantAnthropic, AssistantOpenAI)
	}
	if a.Endpoint == "" {
		a.Endpoint = DefaultAssistantEndpoint[a.Provider]
	}
	if !strings.HasPrefix(a.Endpoint, "https://") && !strings.HasPrefix(a.Endpoint, "http://") {
		return fmt.Errorf("%w: the endpoint is an http:// or https:// address", ErrInvalidSetting)
	}
	if len(a.Endpoint) > 2048 {
		return fmt.Errorf("%w: the endpoint is too long", ErrInvalidSetting)
	}
	if a.Model == "" {
		a.Model = DefaultAssistantModel[a.Provider]
	}
	if err := textcheck.Check("model", a.Model, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSetting, err)
	}
	if a.Name == "" {
		a.Name = DefaultAssistantName
	}
	if err := textcheck.Check("name", a.Name, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSetting, err)
	}
	switch {
	case a.MaxTokens < 1 || a.MaxTokens > MaxAssistantMaxTokens:
		return fmt.Errorf("%w: the most tokens a reply takes is 1 to %d", ErrInvalidSetting, MaxAssistantMaxTokens)
	case a.MaxSteps < 1 || a.MaxSteps > MaxAssistantSteps:
		return fmt.Errorf("%w: the most steps a message takes is 1 to %d", ErrInvalidSetting, MaxAssistantSteps)
	case a.MaxTools < 0:
		return fmt.Errorf("%w: the most tools offered is 0 (all) or more", ErrInvalidSetting)
	case a.PersonMessages < 0 || a.PersonTokens < 0 || a.InstanceMessages < 0 || a.InstanceTokens < 0:
		return fmt.Errorf("%w: a limit is 0 (none) or more", ErrInvalidSetting)
	case a.KeepDays < 1 || a.KeepDays > MaxAssistantKeepDays:
		return fmt.Errorf("%w: conversations are kept 1 to %d days", ErrInvalidSetting, MaxAssistantKeepDays)
	}
	return nil
}

const assistantSettingsColumns = "enabled, provider, endpoint, model, api_key, name, max_tokens, max_steps, max_tools, person_messages, person_tokens, " +
	"instance_messages, instance_tokens, keep_days, acknowledged_by, acknowledged_at, updated_at"

// AssistantSettings reads the instance's settings, with the key, or the
// defaults where none were saved.
func (s *Store) AssistantSettings() (AssistantSettings, error) {
	out := DefaultAssistantSettings()
	var (
		key, ackBy sql.NullString
		ackAt      sql.NullTime
		updated    time.Time
	)
	err := s.QueryRow("SELECT "+assistantSettingsColumns+" FROM assistant_settings WHERE id = 'instance'").Scan(
		&out.Enabled, &out.Provider, &out.Endpoint, &out.Model, &key, &out.Name, &out.MaxTokens, &out.MaxSteps, &out.MaxTools,
		&out.PersonMessages, &out.PersonTokens, &out.InstanceMessages, &out.InstanceTokens, &out.KeepDays, &ackBy, &ackAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("reading the assistant's settings: %w", err)
	}
	out.APIKey, out.KeySet = key.String, key.String != ""
	if ackBy.Valid {
		out.AcknowledgedBy = &ackBy.String
	}
	out.AcknowledgedAt = timePtr(ackAt)
	out.UpdatedAt = &updated
	return out, nil
}

// SaveAssistantSettings saves the instance's settings, by the person
// saving them. Its APIKey is saved as it is: read the stored one first to
// keep it.
func (s *Store) SaveAssistantSettings(in AssistantSettings, by string) error {
	if err := in.Validate(); err != nil {
		return err
	}
	var key, byID any
	if in.APIKey != "" {
		key = in.APIKey
	}
	if by != "" {
		byID = by
	}
	now := dbTime(time.Now())
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM assistant_settings WHERE id = 'instance'"); err != nil {
		return fmt.Errorf("saving the assistant's settings: %w", err)
	}
	if _, err := s.TxExec(tx, "INSERT INTO assistant_settings (id, "+assistantSettingsColumns+", updated_by) VALUES ('instance', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		in.Enabled, in.Provider, in.Endpoint, in.Model, key, in.Name, in.MaxTokens, in.MaxSteps, in.MaxTools,
		in.PersonMessages, in.PersonTokens, in.InstanceMessages, in.InstanceTokens, in.KeepDays,
		ptrValue(in.AcknowledgedBy), dbTimePtr(in.AcknowledgedAt), now, byID); err != nil {
		return fmt.Errorf("saving the assistant's settings: %w", err)
	}
	return tx.Commit()
}

func ptrValue(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// Guide starts a conversation with a purpose (docs/assistant.md#guides).
type Guide struct {
	ID       string   `json:"id"`
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Purpose  string   `json:"purpose"`
	Enabled  bool     `json:"enabled"`
	Category []string `json:"category_ids"`
	LinkType []string `json:"link_type_ids"`
	// Questions are what it makes sure are answered.
	Questions []string  `json:"questions"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// The bounds of a guide.
const (
	MaxGuidePurpose   = 4000
	MaxGuideQuestions = 50
	MaxGuideQuestion  = 500
)

// GuideInput is a guide as made or changed.
type GuideInput struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Purpose   string   `json:"purpose"`
	Enabled   bool     `json:"enabled"`
	Category  []string `json:"category_ids"`
	LinkType  []string `json:"link_type_ids"`
	Questions []string `json:"questions"`
}

func (in *GuideInput) normalize() error {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Name = strings.TrimSpace(in.Name)
	in.Purpose = strings.TrimSpace(in.Purpose)
	if !rbac.ValidScopeName(in.Slug) {
		return fmt.Errorf("%w: its address is 1 to 64 lowercase letters, digits, - and _, starting with a letter or digit", ErrInvalidGuide)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidGuide)
	}
	if err := textcheck.Check("guide name", in.Name, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGuide, err)
	}
	if in.Purpose == "" {
		return fmt.Errorf("%w: say what it is for", ErrInvalidGuide)
	}
	if err := textcheck.Check("purpose", in.Purpose, MaxGuidePurpose); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGuide, err)
	}
	var qs []string
	for _, q := range in.Questions {
		if q = strings.TrimSpace(q); q == "" {
			continue
		}
		if err := textcheck.Check("question", q, MaxGuideQuestion); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidGuide, err)
		}
		qs = append(qs, q)
	}
	if len(qs) > MaxGuideQuestions {
		return fmt.Errorf("%w: at most %d questions", ErrInvalidGuide, MaxGuideQuestions)
	}
	in.Questions = qs
	in.Category = uniqueStrings(in.Category)
	in.LinkType = uniqueStrings(in.LinkType)
	return nil
}

func uniqueStrings(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

const guideColumns = "id, slug, name, purpose, category_ids, link_type_ids, questions, enabled, created_at, updated_at"

func scanGuide(row interface{ Scan(...any) error }) (Guide, error) {
	var (
		g                 Guide
		cats, links, ques string
	)
	if err := row.Scan(&g.ID, &g.Slug, &g.Name, &g.Purpose, &cats, &links, &ques, &g.Enabled, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return g, err
	}
	_ = json.Unmarshal([]byte(cats), &g.Category)
	_ = json.Unmarshal([]byte(links), &g.LinkType)
	_ = json.Unmarshal([]byte(ques), &g.Questions)
	for _, p := range []*[]string{&g.Category, &g.LinkType, &g.Questions} {
		if *p == nil {
			*p = []string{}
		}
	}
	return g, nil
}

// ListGuides lists the guides by name.
func (s *Store) ListGuides() ([]Guide, error) {
	rows, err := s.Query("SELECT " + guideColumns + " FROM assistant_guides ORDER BY name, slug")
	if err != nil {
		return nil, fmt.Errorf("listing guides: %w", err)
	}
	defer rows.Close()
	out := []Guide{}
	for rows.Next() {
		g, err := scanGuide(rows)
		if err != nil {
			return nil, fmt.Errorf("reading a guide: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetGuide reads the guide at an address, or ErrGuideNotFound.
func (s *Store) GetGuide(slug string) (*Guide, error) { return s.getGuide("slug", slug) }

// GetGuideByID reads a guide, or ErrGuideNotFound.
func (s *Store) GetGuideByID(id string) (*Guide, error) { return s.getGuide("id", id) }

func (s *Store) getGuide(column, value string) (*Guide, error) {
	g, err := scanGuide(s.QueryRow("SELECT "+guideColumns+" FROM assistant_guides WHERE "+column+" = ?", value))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGuideNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading guide: %w", err)
	}
	return &g, nil
}

// SaveGuide makes a guide, when id is empty, or changes one. An address in
// use fails as a unique violation.
func (s *Store) SaveGuide(id string, in GuideInput, by string) (*Guide, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	cats, _ := json.Marshal(in.Category)
	links, _ := json.Marshal(in.LinkType)
	ques, _ := json.Marshal(in.Questions)
	now := dbTime(time.Now())
	if id == "" {
		id = uuid.NewString()
		var byID any
		if by != "" {
			byID = by
		}
		if _, err := s.Exec("INSERT INTO assistant_guides (id, slug, name, purpose, category_ids, link_type_ids, questions, enabled, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, in.Slug, in.Name, in.Purpose, string(cats), string(links), string(ques), in.Enabled, byID, now, now); err != nil {
			return nil, fmt.Errorf("saving guide: %w", err)
		}
		return s.GetGuideByID(id)
	}
	res, err := s.Exec("UPDATE assistant_guides SET slug = ?, name = ?, purpose = ?, category_ids = ?, link_type_ids = ?, questions = ?, enabled = ?, updated_at = ? WHERE id = ?",
		in.Slug, in.Name, in.Purpose, string(cats), string(links), string(ques), in.Enabled, now, id)
	if err != nil {
		return nil, fmt.Errorf("saving guide: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return nil, ErrGuideNotFound
	}
	return s.GetGuideByID(id)
}

// DeleteGuide deletes a guide. Conversations it started go on without it.
func (s *Store) DeleteGuide(id string) error {
	res, err := s.Exec("DELETE FROM assistant_guides WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting guide: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrGuideNotFound
	}
	return nil
}

// Conversation is a person's conversation with the assistant.
type Conversation struct {
	ID     string `json:"id"`
	UserID string `json:"-"`
	Title  string `json:"title"`
	// GuideID is the guide that started it, if one did.
	GuideID string `json:"guide_id,omitempty"`
	// Progress holds the answers to its guide's questions, by question.
	Progress map[string]string `json:"progress,omitempty"`
	// PendingCall is the tool call waiting for the person to confirm it.
	PendingCall string    `json:"pending_call,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ConversationMessage is one message of a conversation: the person's
// (role user), the model's (assistant), or what tools answered (tool).
type ConversationMessage struct {
	ID      string          `json:"id"`
	Seq     int             `json:"seq"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Context json.RawMessage `json:"context,omitempty"`
	// Raw is the model's reply as its provider gave it, sent back to the
	// same provider unchanged; never shown.
	Raw         json.RawMessage `json:"-"`
	RawProvider string          `json:"-"`
	Tokens      int64           `json:"tokens"`
	CreatedAt   time.Time       `json:"created_at"`
}

// MaxConversationTitle is the longest title, in characters.
const MaxConversationTitle = 120

// UntitledConversation is the title of a conversation until its first
// message names it.
const UntitledConversation = "New conversation"

const conversationColumns = "id, user_id, title, guide_id, progress, pending_call, created_at, updated_at"

func scanConversation(row interface{ Scan(...any) error }) (Conversation, error) {
	var (
		c                       Conversation
		guide, progress, called sql.NullString
	)
	if err := row.Scan(&c.ID, &c.UserID, &c.Title, &guide, &progress, &called, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return c, err
	}
	c.GuideID, c.PendingCall = guide.String, called.String
	if progress.String != "" {
		_ = json.Unmarshal([]byte(progress.String), &c.Progress)
	}
	return c, nil
}

// CreateConversation starts a conversation for a person, with a title and
// the guide that starts it, if any.
func (s *Store) CreateConversation(userID, title, guideID string) (*Conversation, error) {
	id, now := uuid.NewString(), dbTime(time.Now())
	var guide any
	if guideID != "" {
		guide = guideID
	}
	if _, err := s.Exec("INSERT INTO assistant_conversations (id, user_id, title, guide_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, userID, clipTitle(title), guide, now, now); err != nil {
		return nil, fmt.Errorf("starting a conversation: %w", err)
	}
	return s.GetConversation(userID, id)
}

// clipTitle is a title as kept: one line, at most MaxConversationTitle
// characters.
func clipTitle(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > MaxConversationTitle {
		t = string(r[:MaxConversationTitle-1]) + "…"
	}
	if t == "" {
		t = UntitledConversation
	}
	return t
}

// ListConversations lists a person's conversations, most recent first.
func (s *Store) ListConversations(userID string) ([]Conversation, error) {
	rows, err := s.Query("SELECT "+conversationColumns+" FROM assistant_conversations WHERE user_id = ? ORDER BY updated_at DESC, id", userID)
	if err != nil {
		return nil, fmt.Errorf("listing conversations: %w", err)
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("reading a conversation: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConversation reads a person's conversation. Anyone else's reads as
// not there, ErrConversationNotFound.
func (s *Store) GetConversation(userID, id string) (*Conversation, error) {
	c, err := scanConversation(s.QueryRow("SELECT "+conversationColumns+" FROM assistant_conversations WHERE id = ? AND user_id = ?", id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading conversation: %w", err)
	}
	return &c, nil
}

// DeleteConversation deletes a person's conversation, with its messages.
func (s *Store) DeleteConversation(userID, id string) error {
	if _, err := s.GetConversation(userID, id); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := deleteConversationsTx(s, tx, "id = ? AND user_id = ?", id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteConversationsTx deletes the conversations where says, and their
// messages.
func deleteConversationsTx(s *Store, tx *sql.Tx, where string, args ...any) (err error) {
	if _, err = s.TxExec(tx, "DELETE FROM assistant_messages WHERE conversation_id IN (SELECT id FROM assistant_conversations WHERE "+where+")", args...); err != nil {
		return fmt.Errorf("deleting conversations' messages: %w", err)
	}
	if _, err = s.TxExec(tx, "DELETE FROM assistant_conversations WHERE "+where, args...); err != nil {
		return fmt.Errorf("deleting conversations: %w", err)
	}
	return nil
}

// DeleteConversationsBefore deletes every conversation whose last message
// is older than before, returning how many.
func (s *Store) DeleteConversationsBefore(before time.Time) (int, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM assistant_conversations WHERE updated_at < ?", dbTime(before)).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting old conversations: %w", err)
	}
	if n == 0 {
		return 0, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := deleteConversationsTx(s, tx, "updated_at < ?", dbTime(before)); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// ConversationMessages are a conversation's messages, in order.
func (s *Store) ConversationMessages(conversationID string) ([]ConversationMessage, error) {
	rows, err := s.Query("SELECT id, seq, role, content, context, raw, raw_provider, tokens, created_at FROM assistant_messages WHERE conversation_id = ? ORDER BY seq", conversationID)
	if err != nil {
		return nil, fmt.Errorf("reading the conversation: %w", err)
	}
	defer rows.Close()
	out := []ConversationMessage{}
	for rows.Next() {
		var (
			m                   ConversationMessage
			content             string
			context, raw, rawBy sql.NullString
		)
		if err := rows.Scan(&m.ID, &m.Seq, &m.Role, &content, &context, &raw, &rawBy, &m.Tokens, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("reading a message: %w", err)
		}
		m.Content = json.RawMessage(content)
		if context.String != "" {
			m.Context = json.RawMessage(context.String)
		}
		if raw.String != "" {
			m.Raw = json.RawMessage(raw.String)
		}
		m.RawProvider = rawBy.String
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddConversationMessage appends a message to a conversation, which counts
// as its latest. The conversation's pending call is set to pending.
func (s *Store) AddConversationMessage(conversationID string, m ConversationMessage, pending string) (*ConversationMessage, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	var seq sql.NullInt64
	if err := s.TxQueryRow(tx, "SELECT MAX(seq) FROM assistant_messages WHERE conversation_id = ?", conversationID).Scan(&seq); err != nil {
		return nil, fmt.Errorf("adding a message: %w", err)
	}
	m.ID, m.Seq, m.CreatedAt = uuid.NewString(), int(seq.Int64)+1, dbTime(time.Now())
	nullable := func(raw []byte) any {
		if len(raw) == 0 {
			return nil
		}
		return string(raw)
	}
	var rawBy any
	if m.RawProvider != "" {
		rawBy = m.RawProvider
	}
	if _, err := s.TxExec(tx, "INSERT INTO assistant_messages (id, conversation_id, seq, role, content, context, raw, raw_provider, tokens, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		m.ID, conversationID, m.Seq, m.Role, string(m.Content), nullable(m.Context), nullable(m.Raw), rawBy, m.Tokens, m.CreatedAt); err != nil {
		return nil, fmt.Errorf("adding a message: %w", err)
	}
	if err := setPendingTx(s, tx, conversationID, pending, m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, tx.Commit()
}

func setPendingTx(s *Store, tx *sql.Tx, conversationID, pending string, at time.Time) error {
	var p any
	if pending != "" {
		p = pending
	}
	if _, err := s.TxExec(tx, "UPDATE assistant_conversations SET pending_call = ?, updated_at = ? WHERE id = ?", p, at, conversationID); err != nil {
		return fmt.Errorf("updating the conversation: %w", err)
	}
	return nil
}

// SetConversationPending sets the call waiting for the person to confirm
// it, or none.
func (s *Store) SetConversationPending(conversationID, pending string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := setPendingTx(s, tx, conversationID, pending, dbTime(time.Now())); err != nil {
		return err
	}
	return tx.Commit()
}

// SetConversationProgress records the answers to a guide's questions.
func (s *Store) SetConversationProgress(conversationID string, progress map[string]string) error {
	raw, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	if _, err := s.Exec("UPDATE assistant_conversations SET progress = ? WHERE id = ?", string(raw), conversationID); err != nil {
		return fmt.Errorf("recording progress: %w", err)
	}
	return nil
}

// AssistantUse is how much of the assistant has been used on a day.
type AssistantUse struct {
	Messages int   `json:"messages"`
	Tokens   int64 `json:"tokens"`
}

// AssistantDay is the day, in UTC, that limits count t in.
func AssistantDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }

// AssistantUsage is a person's use on a day, and the instance's.
func (s *Store) AssistantUsage(userID, day string) (person, instance AssistantUse, err error) {
	err = s.QueryRow("SELECT COALESCE(SUM(messages), 0), COALESCE(SUM(tokens), 0) FROM assistant_usage WHERE user_id = ? AND day = ?", userID, day).Scan(&person.Messages, &person.Tokens)
	if err == nil {
		err = s.QueryRow("SELECT COALESCE(SUM(messages), 0), COALESCE(SUM(tokens), 0) FROM assistant_usage WHERE day = ?", day).Scan(&instance.Messages, &instance.Tokens)
	}
	if err != nil {
		err = fmt.Errorf("reading the assistant's use: %w", err)
	}
	return person, instance, err
}

// AddAssistantUsage counts messages and tokens against a person's day.
func (s *Store) AddAssistantUsage(userID, day string, messages int, tokens int64) error {
	for try := 0; ; try++ {
		res, err := s.Exec("UPDATE assistant_usage SET messages = messages + ?, tokens = tokens + ? WHERE user_id = ? AND day = ?", messages, tokens, userID, day)
		if err != nil {
			return fmt.Errorf("counting the assistant's use: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			return nil
		}
		_, err = s.Exec("INSERT INTO assistant_usage (user_id, day, messages, tokens) VALUES (?, ?, ?, ?)", userID, day, messages, tokens)
		if err == nil {
			return nil
		}
		// Another request counted the first use of the day meanwhile.
		if !IsUniqueViolation(err) || try > 2 {
			return fmt.Errorf("counting the assistant's use: %w", err)
		}
	}
}

// DeleteAssistantUsageBefore forgets the use counted before a day.
func (s *Store) DeleteAssistantUsageBefore(day string) error {
	if _, err := s.Exec("DELETE FROM assistant_usage WHERE day < ?", day); err != nil {
		return fmt.Errorf("forgetting the assistant's use: %w", err)
	}
	return nil
}

// AssistantDraftsForAssets reports whether an assistant's changes to the
// assets named, each by ID or code, land as drafts (docs/review.md#assistants):
// the instance says so, and every one is found, alone, and in a category
// that uses review states.
func (s *Store) AssistantDraftsForAssets(refs []string) (bool, error) {
	set, err := s.ReviewSettings()
	if err != nil || !set.AssistantDrafts || len(refs) == 0 {
		return false, err
	}
	t, err := readReviewTree(s)
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		id, ok, err := s.oneAsset(ref)
		if err != nil || !ok {
			return false, err
		}
		cats, err := s.assetCategoriesOf(id)
		if err != nil {
			return false, err
		}
		if !t.assetRules(cats).Enabled {
			return false, nil
		}
	}
	return true, nil
}

// AssistantDraftsForCategories reports whether an asset an assistant adds
// in these categories lands as a draft.
func (s *Store) AssistantDraftsForCategories(ids []string) (bool, error) {
	set, err := s.ReviewSettings()
	if err != nil || !set.AssistantDrafts || len(ids) == 0 {
		return false, err
	}
	t, err := readReviewTree(s)
	if err != nil {
		return false, err
	}
	return t.assetRules(ids).Enabled, nil
}

// oneAsset is the asset an ID, or a code only one asset has, names.
func (s *Store) oneAsset(ref string) (string, bool, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM assets WHERE id = ?", ref).Scan(&n); err != nil {
		return "", false, fmt.Errorf("finding the asset: %w", err)
	}
	if n > 0 {
		return ref, true, nil
	}
	ids, err := s.AssetsByCode(ref)
	if err != nil || len(ids) != 1 {
		return "", false, err
	}
	return ids[0], true, nil
}

func (s *Store) assetCategoriesOf(id string) ([]string, error) {
	rows, err := s.Query("SELECT category_id FROM asset_categories WHERE asset_id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("reading the asset's categories: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetConversationTitle names a conversation, as its first message does.
func (s *Store) SetConversationTitle(id, title string) (*Conversation, error) {
	var userID string
	if err := s.QueryRow("SELECT user_id FROM assistant_conversations WHERE id = ?", id).Scan(&userID); err != nil {
		return nil, ErrConversationNotFound
	}
	if _, err := s.Exec("UPDATE assistant_conversations SET title = ? WHERE id = ?", clipTitle(title), id); err != nil {
		return nil, fmt.Errorf("naming the conversation: %w", err)
	}
	return s.GetConversation(userID, id)
}
