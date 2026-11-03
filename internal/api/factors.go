package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/qr"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Two-factor sign-in (docs/access.md): a password accepted for someone with
// a second factor leads to a short second step, bound to the browser by a
// cookie, rather than a session; a code from their app finishes it.

// ChallengeCookieName holds the second step of a sign-in. It is sent only
// to the sign-in routes.
const ChallengeCookieName = "bam_sign_in"

const challengeCookiePath = "/api/v1/auth/login"

func (s *Server) setChallengeCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: ChallengeCookieName, Value: token, Path: challengeCookiePath,
		MaxAge: int(store.SignInChallengeTTL.Seconds()), Expires: time.Now().Add(store.SignInChallengeTTL),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isSecureRequest(r, s.cfg.Server.BaseURL),
	})
}

func (s *Server) clearChallengeCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: ChallengeCookieName, Value: "", Path: challengeCookiePath, MaxAge: -1, Expires: time.Unix(0, 0),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isSecureRequest(r, s.cfg.Server.BaseURL),
	})
}

// startSecondStep answers a password accepted for someone with a second
// factor: no session yet, but a challenge, and the kinds of factor that can
// finish it.
func (s *Server) startSecondStep(w http.ResponseWriter, r *http.Request, user *models.User, factors []store.SealedFactor) {
	token, err := s.store.CreateSignInChallenge(user.ID)
	if err != nil {
		writeServerError(w, err, "start the second step")
		return
	}
	s.setChallengeCookie(w, r, token)
	kinds := []string{}
	for _, f := range factors {
		if !contains(kinds, f.Kind) {
			kinds = append(kinds, f.Kind)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"factor_required": true, "factors": kinds})
}

// FactorLoginRequest is the second step of a sign-in.
type FactorLoginRequest struct {
	Code string `json:"code"`
}

// POST /api/v1/auth/login/factor: a code from the app finishes a sign-in
// whose password was accepted, as the challenge cookie says.
func (s *Server) handleLoginFactor(w http.ResponseWriter, r *http.Request) {
	var req FactorLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	token := ""
	if c, err := r.Cookie(ChallengeCookieName); err == nil {
		token = c.Value
	}
	user, err := s.store.SignInChallenge(token)
	if errors.Is(err, store.ErrChallengeNotFound) || errors.Is(err, store.ErrUserNotFound) {
		s.clearChallengeCookie(w, r)
		writeJSONError(w, http.StatusUnauthorized, "The sign-in has expired. Sign in again.")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the sign-in")
		return
	}

	// Wrong codes count with wrong passwords, against the account and the
	// address (#246).
	address, now := ClientAddress(r), time.Now()
	wait, err := s.store.SignInWait(user.Email, address, now)
	if err != nil {
		writeServerError(w, err, "check the sign-in throttle")
		return
	}
	if wait > 0 {
		writeThrottled(w, wait)
		return
	}

	factors, err := s.store.SealedFactors(user.ID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	matched, unreadable := false, 0
	for _, f := range factors {
		if f.Kind != store.FactorTOTP {
			continue
		}
		secret, err := s.sealer.Open(f.Secret, store.FactorPurpose(f.ID))
		if err != nil {
			unreadable++
			continue
		}
		step, ok := auth.CheckTOTP(secret, req.Code, now, f.LastStep)
		if !ok {
			continue
		}
		if err := s.store.UseTOTPStep(f.ID, step); err != nil {
			if errors.Is(err, store.ErrCodeUsed) {
				continue
			}
			writeServerError(w, err, "record the code")
			return
		}
		matched = true
		break
	}
	if !matched {
		if unreadable > 0 && unreadable == len(factors) {
			writeJSONError(w, http.StatusConflict, "Your second factor cannot be checked: this server's [auth] secret_key is not the one it was set up with. "+
				"Ask whoever manages users to remove it, then sign in and set it up again.")
			return
		}
		wait, err := s.store.RecordSignInFailure(user.Email, address, now)
		if err != nil {
			writeServerError(w, err, "count the failed sign-in")
			return
		}
		if wait > 0 {
			writeThrottled(w, wait)
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "That code is not right. Enter the code your app shows now.")
		return
	}

	if err := s.store.ClearSignInFailures(user.Email); err != nil {
		writeServerError(w, err, "clear the sign-in throttle")
		return
	}
	if err := s.store.EndSignInChallenge(token); err != nil {
		writeServerError(w, err, "end the sign-in")
		return
	}
	s.clearChallengeCookie(w, r)
	sess, err := s.store.CreateSession(user.ID, DefaultSessionTTL)
	if err != nil {
		writeServerError(w, err, "create session")
		return
	}
	SetSessionCookie(w, r, sess.ID, DefaultSessionTTL, s.cfg.Server.BaseURL)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

// GET /api/v1/auth/me/factors: the signed-in person's second factors.
func (s *Server) handleListOwnFactors(w http.ResponseWriter, r *http.Request) {
	factors, err := s.store.Factors(CurrentUser(r.Context()).ID)
	if err != nil {
		writeServerError(w, err, "list second factors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"factors": factors, "available": s.sealer.HasKey()})
}

// issuer is the name an app shows a code under: BAM!, and the instance's
// host when it has one.
func (s *Server) issuer() string {
	if u, err := url.Parse(s.cfg.Server.BaseURL); err == nil && u.Hostname() != "" {
		return "BAM! " + u.Hostname()
	}
	return "BAM!"
}

// POST /api/v1/auth/me/factors/totp: starts setting up an authenticator
// app, with a new secret, as a QR code and as text. It is set up once
// confirmed with a code.
func (s *Server) handleStartTOTP(w http.ResponseWriter, r *http.Request) {
	if !s.sealer.HasKey() {
		writeJSONError(w, http.StatusConflict, "Two-factor sign-in is not available: the configuration has no [auth] secret_key. Ask whoever runs BAM! to add one.")
		return
	}
	user := CurrentUser(r.Context())
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		writeServerError(w, err, "make a secret")
		return
	}
	factor, err := s.store.StartTOTP(user.ID, func(id string) (string, error) {
		return s.sealer.Seal(secret, store.FactorPurpose(id))
	})
	if err != nil {
		writeServerError(w, err, "start setting up the app")
		return
	}
	uri := auth.TOTPURI(s.issuer(), user.Email, secret)
	svg, err := qrSVG(uri)
	if err != nil {
		writeServerError(w, err, "draw the QR code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"factor": factor,
		"secret": auth.TOTPSecretText(secret),
		"uri":    uri,
		"qr_svg": svg,
	})
}

// qrSVG draws a QR code of text as SVG, with its quiet zone.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, size, size, size, size)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Dark(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}

// ConfirmFactorRequest finishes setting up an app, with a code from it.
type ConfirmFactorRequest struct {
	Code string `json:"code"`
}

// POST /api/v1/auth/me/factors/totp/confirm: a code from the app finishes
// setting it up; from then on, signing in asks for one.
func (s *Server) handleConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	var req ConfirmFactorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	user := CurrentUser(r.Context())
	pending, err := s.store.PendingTOTP(user.ID)
	if errors.Is(err, store.ErrFactorNotFound) {
		writeJSONError(w, http.StatusNotFound, "No app is being set up. Start again.")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the app being set up")
		return
	}
	secret, err := s.sealer.Open(pending.Secret, store.FactorPurpose(pending.ID))
	if err != nil {
		writeJSONError(w, http.StatusConflict, "The app being set up cannot be checked with this server's key. Start again.")
		return
	}
	step, ok := auth.CheckTOTP(secret, req.Code, time.Now(), 0)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "That code is not right. Enter the code the app shows now, and check that the phone's clock is right.")
		return
	}
	factor, err := s.store.ConfirmTOTP(user.ID, pending.ID, step)
	if errors.Is(err, store.ErrFactorNotFound) {
		writeJSONError(w, http.StatusNotFound, "No app is being set up. Start again.")
		return
	}
	if err != nil {
		writeServerError(w, err, "confirm the app")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"factor": factor})
}

// RemoveFactorRequest proves it is the person removing their own factor.
type RemoveFactorRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// DELETE /api/v1/auth/me/factors/{id}: turns a factor off, given the
// password and a current code from it.
func (s *Server) handleDeleteOwnFactor(w http.ResponseWriter, r *http.Request) {
	var req RemoveFactorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	user, id := CurrentUser(r.Context()), r.PathValue("id")
	factors, err := s.store.SealedFactors(user.ID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	var factor *store.SealedFactor
	for i := range factors {
		if factors[i].ID == id {
			factor = &factors[i]
		}
	}
	if factor == nil {
		writeJSONError(w, http.StatusNotFound, "Second factor not found")
		return
	}
	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		writeJSONError(w, http.StatusBadRequest, "That password is not right.")
		return
	}
	secret, err := s.sealer.Open(factor.Secret, store.FactorPurpose(factor.ID))
	if err != nil {
		writeJSONError(w, http.StatusConflict, "This factor cannot be checked with this server's key. Ask whoever manages users to remove it.")
		return
	}
	step, ok := auth.CheckTOTP(secret, req.Code, time.Now(), factor.LastStep)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "That code is not right. Enter the code your app shows now.")
		return
	}
	if err := s.store.UseTOTPStep(factor.ID, step); err != nil {
		if errors.Is(err, store.ErrCodeUsed) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeServerError(w, err, "record the code")
		return
	}
	if err := s.store.DeleteFactor(user.ID, id); err != nil {
		writeServerError(w, err, "remove the factor")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/users/{id}/factors: removes someone's second factors, for
// one who has lost theirs, and signs them out everywhere (users.manage).
func (s *Server) handleResetUserFactors(w http.ResponseWriter, r *http.Request) {
	n, err := s.actingStore(r).ResetFactors(r.PathValue("id"))
	if errors.Is(err, store.ErrUserNotFound) {
		writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "remove the second factors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": n})
}
