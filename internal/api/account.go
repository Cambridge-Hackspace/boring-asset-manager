package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Account emails (docs/access.md#account-emails): invitations, password
// resets and removing a lost second factor, each a link that works once;
// and notices of changes to someone's own account. They are about their
// own account, so they are sent whatever they chose for email to their
// roles.

// sendAccountMail sends m without holding up the request, logging a
// failure: how account emails are sent unless a test replaces it.
func (s *Server) sendAccountMail(m email.Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.mailer.Send(ctx, m); err != nil {
			log.Printf("emailing %s: %v", strings.Join(m.To, ", "), err)
		}
	}()
}

// canEmailLinks reports whether links can be emailed: email is set up, and
// the server's base URL known, for the links to lead back to.
func (s *Server) canEmailLinks() bool {
	return s.sendMail != nil && strings.TrimSpace(s.cfg.Server.BaseURL) != ""
}

func (s *Server) base() string { return strings.TrimRight(s.cfg.Server.BaseURL, "/") }

// accountMail lays out and sends an email to user about their account.
func (s *Server) accountMail(user *models.User, subject, accent string, paragraphs []string, link, linkLabel string) {
	if s.sendMail == nil {
		return
	}
	html, text, err := email.Render(email.Content{Title: subject, Paragraphs: paragraphs, Link: link, LinkLabel: linkLabel,
		Accent: accent, Footer: "Sent to " + user.Email + " about their own account."})
	if err != nil {
		log.Printf("writing the email to %s: %v", user.Email, err)
		return
	}
	s.sendMail(email.Message{To: []string{user.Email}, Subject: subject, HTML: html, Text: text})
}

// The notices of a change to someone's own account.
const (
	noticePasswordChanged = iota
	noticeFactorAdded
	noticePasskeyAdded
	noticeFactorRemoved
)

// tellAccount emails someone that their account changed, so that a change
// they did not make does not go unseen.
func (s *Server) tellAccount(user *models.User, notice int) {
	if user == nil {
		return
	}
	var subject, what string
	switch notice {
	case noticePasswordChanged:
		subject, what = "Your BAM! password was changed", "The password of your BAM! account was changed."
	case noticeFactorAdded:
		subject, what = "A second factor was added to your BAM! account", "A second factor, an authenticator app, was added to your BAM! account. Signing in now asks for a code from it."
	case noticePasskeyAdded:
		subject, what = "A passkey was added to your BAM! account", "A second factor, a passkey, was added to your BAM! account. Signing in can now ask for it."
	case noticeFactorRemoved:
		subject, what = "A second factor was removed from your BAM! account", "A second factor was removed from your BAM! account."
	}
	paragraphs := []string{what, "If it was you, there is nothing to do. If it was not, change your password now, and tell whoever manages users."}
	link := ""
	if b := s.base(); b != "" {
		link = b + "/account"
	}
	s.accountMail(user, subject, "#f59e0b", paragraphs, link, "Your account")
}

// linkURL is where a link of purpose leads, with its token.
func (s *Server) linkURL(path, token string) string {
	return s.base() + path + "?" + url.Values{"token": {token}}.Encode()
}

// invite makes an invitation for user and emails it, replacing any sent
// before.
func (s *Server) invite(user *models.User, by string) error {
	token, err := s.store.CreateAccountLink(user.ID, store.LinkInvite, store.InviteTTL)
	if err != nil {
		return err
	}
	who := "Someone"
	if by != "" {
		who = by
	}
	s.accountMail(user, "You are invited to BAM!", "#2563eb", []string{
		who + " made you an account on BAM!, the asset manager, as " + user.Email + ".",
		"Follow the link to set a password, or to sign in through one of the sign-in buttons. It works once, and for seven days.",
	}, s.linkURL("/invite", token), "Accept the invitation")
	return nil
}

// noEmailForLinks answers a request for a link emails cannot carry here.
func noEmailForLinks(w http.ResponseWriter) {
	writeJSONError(w, http.StatusConflict, "Email is not set up, or [server] base_url is not, so no link can be emailed. See docs/notifications.md#email.")
}

// handleInviteUser sends someone an invitation again: POST
// /api/v1/users/{id}/invite. The one sent before stops working.
func (s *Server) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	if !s.canEmailLinks() {
		noEmailForLinks(w)
		return
	}
	user, err := s.store.GetUserByID(r.PathValue("id"))
	if errors.Is(err, store.ErrUserNotFound) {
		writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the user")
		return
	}
	if !user.IsActive || user.ArchivedAt != nil {
		writeJSONError(w, http.StatusConflict, "Their account is disabled: enable it first")
		return
	}
	if err := s.invite(user, s.personName(r)); err != nil {
		writeServerError(w, err, "make the invitation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Invitation sent to " + user.Email})
}

// personName is who is making a request, as an email says it.
func (s *Server) personName(r *http.Request) string {
	if u := CurrentUser(r.Context()); u != nil {
		if u.Name != "" {
			return u.Name
		}
		return u.Email
	}
	return ""
}

// invitation is what the invitation page shows.
type invitation struct {
	Email          string           `json:"email"`
	Name           string           `json:"name"`
	LocalPasswords bool             `json:"local_passwords"`
	Providers      []publicProvider `json:"providers"`
}

// handleGetInvitation says whom an invitation is for, and how they can
// accept it: GET /api/v1/auth/invite?token=…
func (s *Server) handleGetInvitation(w http.ResponseWriter, r *http.Request) {
	user, err := s.store.AccountLink(r.URL.Query().Get("token"), store.LinkInvite)
	if errors.Is(err, store.ErrAccountLinkNotFound) {
		writeJSONError(w, http.StatusNotFound, "This invitation has expired, or was used already. Ask whoever manages users for a new one.")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the invitation")
		return
	}
	on, err := s.store.LocalPasswords()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	providers, err := s.publicProviders()
	if err != nil {
		writeServerError(w, err, "list identity providers")
		return
	}
	writeJSON(w, http.StatusOK, invitation{Email: user.Email, Name: user.Name, LocalPasswords: on, Providers: providers})
}

// AcceptInvitationRequest sets an invited person's password.
type AcceptInvitationRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// handleAcceptInvitation sets the password of someone invited, and signs
// them in: POST /api/v1/auth/invite.
func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req AcceptInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	on, err := s.store.LocalPasswords()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	if !on {
		writeJSONError(w, http.StatusConflict, "Passwords are off here: accept the invitation with one of the sign-in buttons.")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := s.store.TakeAccountLink(req.Token, store.LinkInvite)
	if errors.Is(err, store.ErrAccountLinkNotFound) {
		writeJSONError(w, http.StatusNotFound, "This invitation has expired, or was used already. Ask whoever manages users for a new one.")
		return
	}
	if err != nil {
		writeServerError(w, err, "use the invitation")
		return
	}
	if err := s.store.SetPassword(user.ID, req.Password, ""); err != nil {
		writeServerError(w, err, "set the password")
		return
	}
	sess, err := s.startSession(r, user.ID)
	if err != nil {
		writeServerError(w, err, "create session")
		return
	}
	SetSessionCookie(w, r, sess.ID, DefaultSessionTTL, s.cfg.Server.BaseURL)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "factor_setup_required": sess.FactorSetup})
}

// passwordResetOn reports whether passwords can be reset by email here:
// turned on, with passwords on (#184) and links that can be emailed.
func (s *Server) passwordResetOn() (bool, error) {
	on, err := s.store.On(store.PasswordResetSetting)
	if err != nil || !on || !s.canEmailLinks() {
		return false, err
	}
	return s.store.LocalPasswords()
}

// passwordResetSent is the answer to every request for a reset, whether or
// not the email has an account, so that it cannot be used to find one.
const passwordResetSent = "If that email has an account here, a link to set a new password is on its way. It works once, for 30 minutes."

// PasswordResetRequest asks for a link to set a new password.
type PasswordResetRequest struct {
	Email string `json:"email"`
}

// handleRequestPasswordReset emails a link to set a new password: POST
// /api/v1/auth/password-reset. It answers the same whether or not the email
// has an account, and is held back as failed sign-ins are, per address and
// per account.
func (s *Server) handleRequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		writeJSONError(w, http.StatusBadRequest, "Give the email of the account")
		return
	}
	on, err := s.passwordResetOn()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	if !on {
		writeJSONError(w, http.StatusConflict, "Passwords cannot be reset by email here. Ask whoever manages users.")
		return
	}
	address, now := ClientAddress(r), time.Now()
	if wait, err := s.store.SignInWait(req.Email, address, now); err != nil {
		writeServerError(w, err, "check the sign-in throttle")
		return
	} else if wait > 0 {
		writeThrottled(w, wait)
		return
	}
	// Each request counts as a failed sign-in would, so that the link
	// cannot be asked for again and again.
	if _, err := s.store.RecordSignInFailure(req.Email, address, now); err != nil {
		writeServerError(w, err, "count the request")
		return
	}
	user, err := s.store.GetUserByEmail(req.Email)
	if err == nil && user.IsActive && user.ArchivedAt == nil {
		token, err := s.store.CreateAccountLink(user.ID, store.LinkPasswordReset, store.PasswordResetTTL)
		if err != nil {
			writeServerError(w, err, "make the link")
			return
		}
		s.accountMail(user, "Set a new BAM! password", "#2563eb", []string{
			"Someone asked to set a new password for your BAM! account, " + user.Email + ". If it was you, follow the link. It works once, for 30 minutes.",
			"Setting a new password signs you out everywhere. If it was not you, ignore this email: your password stays as it is.",
		}, s.linkURL("/reset-password", token), "Set a new password")
	} else if err != nil && !errors.Is(err, store.ErrUserNotFound) {
		writeServerError(w, err, "find the account")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": passwordResetSent})
}

// handleGetPasswordReset says whose password a link sets: GET
// /api/v1/auth/password-reset?token=…
func (s *Server) handleGetPasswordReset(w http.ResponseWriter, r *http.Request) {
	user, ok := s.peekLink(w, r.URL.Query().Get("token"), store.LinkPasswordReset)
	if ok {
		writeJSON(w, http.StatusOK, map[string]string{"email": user.Email})
	}
}

// peekLink finds whom a link is for, answering when it does not work.
func (s *Server) peekLink(w http.ResponseWriter, token, purpose string) (*models.User, bool) {
	user, err := s.store.AccountLink(token, purpose)
	if errors.Is(err, store.ErrAccountLinkNotFound) {
		writeJSONError(w, http.StatusNotFound, "This link has expired, or was used already. Ask for a new one.")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the link")
		return nil, false
	}
	return user, true
}

// ResetPasswordRequest sets a new password with a link's token.
type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// handleResetPassword sets a new password with an emailed link: POST
// /api/v1/auth/password-reset/confirm. It signs them out everywhere, and
// tells them by email; they then sign in, with their second factor if
// they have one.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	on, err := s.passwordResetOn()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	if !on {
		writeJSONError(w, http.StatusConflict, "Passwords cannot be reset by email here. Ask whoever manages users.")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := s.store.TakeAccountLink(req.Token, store.LinkPasswordReset)
	if errors.Is(err, store.ErrAccountLinkNotFound) {
		writeJSONError(w, http.StatusNotFound, "This link has expired, or was used already. Ask for a new one.")
		return
	}
	if err != nil {
		writeServerError(w, err, "use the link")
		return
	}
	if err := s.store.SetPassword(user.ID, req.Password, ""); err != nil {
		writeServerError(w, err, "set the password")
		return
	}
	if err := s.clearSignInDelay(user.ID); err != nil {
		writeServerError(w, err, "clear the sign-in delay")
		return
	}
	s.tellAccount(user, noticePasswordChanged)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Your password is set, and you are signed out everywhere. Sign in with it."})
}

// factorRecoveryOpen reports whether user may have a lost second factor
// removed by email, saying why not when they may not.
func (s *Server) factorRecoveryOpen(user *models.User) (bool, string, error) {
	on, err := s.store.On(store.FactorRecoverySetting)
	if err != nil {
		return false, "", err
	}
	if !on || !s.canEmailLinks() {
		return false, "A second factor cannot be removed by email here. Use a recovery code, or ask whoever manages users.", nil
	}
	required, err := s.store.RequiresTwoFactor(user.ID)
	if err != nil {
		return false, "", err
	}
	if required {
		anyway, err := s.store.On(store.FactorRecoveryRequiredSetting)
		if err != nil {
			return false, "", err
		}
		if !anyway {
			return false, "A role you hold requires two-factor sign-in, so your second factor cannot be removed by email. Use a recovery code, or ask whoever manages users.", nil
		}
	}
	return true, "", nil
}

// handleRequestFactorRecovery emails a link that removes a lost second
// factor, to someone at the second step of signing in, so after their
// password: POST /api/v1/auth/login/factor/recover.
func (s *Server) handleRequestFactorRecovery(w http.ResponseWriter, r *http.Request) {
	token := ""
	if c, err := r.Cookie(ChallengeCookieName); err == nil {
		token = c.Value
	}
	user, err := s.store.SignInChallenge(token)
	if errors.Is(err, store.ErrChallengeNotFound) {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "read the sign-in")
		return
	}
	open, why, err := s.factorRecoveryOpen(user)
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	if !open {
		writeJSONError(w, http.StatusForbidden, why)
		return
	}
	link, err := s.store.CreateAccountLink(user.ID, store.LinkFactorRecovery, store.FactorRecoveryTTL)
	if err != nil {
		writeServerError(w, err, "make the link")
		return
	}
	s.accountMail(user, "Remove your BAM! second factor", "#dc2626", []string{
		"Someone signed in to your BAM! account, " + user.Email + ", with your password, and asked to remove your second factor because it is lost. If it was you, follow the link. It works once, for 30 minutes.",
		"If it was not you, someone knows your password: do not follow the link. Change your password, and tell whoever manages users.",
	}, s.linkURL("/recover-factor", link), "Remove my second factor")
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "A link to remove your second factor is on its way to " + user.Email + ". It works once, for 30 minutes."})
}

// handleGetFactorRecovery says whose second factor a link removes: GET
// /api/v1/auth/factor-recovery?token=…
func (s *Server) handleGetFactorRecovery(w http.ResponseWriter, r *http.Request) {
	user, ok := s.peekLink(w, r.URL.Query().Get("token"), store.LinkFactorRecovery)
	if ok {
		writeJSON(w, http.StatusOK, map[string]string{"email": user.Email})
	}
}

// FactorRecoveryRequest removes a lost second factor with a link's token.
type FactorRecoveryRequest struct {
	Token string `json:"token"`
}

// handleRecoverFactor removes someone's second factors with an emailed
// link, signing them out everywhere: POST /api/v1/auth/factor-recovery. It
// is recorded as auth.factors_reset, by themselves.
func (s *Server) handleRecoverFactor(w http.ResponseWriter, r *http.Request) {
	var req FactorRecoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	user, ok := s.peekLink(w, req.Token, store.LinkFactorRecovery)
	if !ok {
		return
	}
	// The settings, or their roles, may have changed since it was sent.
	open, why, err := s.factorRecoveryOpen(user)
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	if !open {
		writeJSONError(w, http.StatusForbidden, why)
		return
	}
	if _, err := s.store.TakeAccountLink(req.Token, store.LinkFactorRecovery); err != nil {
		if errors.Is(err, store.ErrAccountLinkNotFound) {
			writeJSONError(w, http.StatusNotFound, "This link has expired, or was used already. Ask for a new one.")
			return
		}
		writeServerError(w, err, "use the link")
		return
	}
	if _, err := s.store.As(user.ID).ResetFactors(user.ID); err != nil {
		writeServerError(w, err, "remove the second factors")
		return
	}
	s.tellAccount(user, noticeFactorRemoved)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Your second factor is removed, and you are signed out everywhere. Sign in with your password, and set up a new one."})
}

// accountEmailSettings are those of account emails, for Admin › Sign-in.
type accountEmailSettings struct {
	PasswordReset          bool `json:"password_reset"`
	FactorRecovery         bool `json:"factor_recovery_email"`
	FactorRecoveryRequired bool `json:"factor_recovery_email_required"`
	// CanEmailLinks says whether links can be emailed at all.
	CanEmailLinks bool `json:"can_email_links"`
}

func (s *Server) accountEmailSettings() (accountEmailSettings, error) {
	out := accountEmailSettings{CanEmailLinks: s.canEmailLinks()}
	for key, into := range map[string]*bool{store.PasswordResetSetting: &out.PasswordReset, store.FactorRecoverySetting: &out.FactorRecovery,
		store.FactorRecoveryRequiredSetting: &out.FactorRecoveryRequired} {
		on, err := s.store.On(key)
		if err != nil {
			return out, err
		}
		*into = on
	}
	return out, nil
}

// setAccountEmailSettings saves those given, refusing to turn one on that
// cannot email its link here.
func (s *Server) setAccountEmailSettings(w http.ResponseWriter, req SignInSettingsRequest) bool {
	for key, v := range map[string]*bool{store.PasswordResetSetting: req.PasswordReset, store.FactorRecoverySetting: req.FactorRecovery,
		store.FactorRecoveryRequiredSetting: req.FactorRecoveryRequired} {
		if v == nil {
			continue
		}
		if *v && key != store.FactorRecoveryRequiredSetting && !s.canEmailLinks() {
			noEmailForLinks(w)
			return false
		}
		value := "off"
		if *v {
			value = "on"
		}
		if err := s.store.SetSetting(key, value); err != nil {
			writeServerError(w, err, "save the sign-in settings")
			return false
		}
	}
	return true
}
