package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/webauthn"
)

// Passkeys (docs/access.md#passkeys): registered under Your account, and
// offered at the second step of signing in. The browser is sent options
// for navigator.credentials.create or .get, binary values base64url, and
// sends back what it answered, the same way.

// passkeyTimeout is how long the browser waits for a passkey, in
// milliseconds: the challenge's own life.
const passkeyTimeout = int(store.PasskeyChallengeTTL / 1e6)

// maxPasskeyName is the longest name a passkey is given.
const maxPasskeyName = 100

const noPasskeys = "Passkeys are not available here yet: whoever runs BAM! needs to set [server] base_url to the address people use."

// relyingParty is this server as passkeys know it, from base_url.
func (s *Server) relyingParty() (webauthn.RelyingParty, bool) {
	rp, err := webauthn.NewRelyingParty(s.cfg.Server.BaseURL)
	return rp, err == nil
}

// passkeysHere reports whether a passkey can be registered from the
// browser making r: only at base_url's host, which they are bound to. The
// default base_url, localhost, is no help to a browser elsewhere.
func (s *Server) passkeysHere(r *http.Request) bool {
	rp, ok := s.relyingParty()
	if !ok {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.EqualFold(host, rp.ID)
}

// usablePasskeys are the factors that are passkeys for this host.
func usablePasskeys(factors []store.SealedFactor, rp webauthn.RelyingParty) []store.SealedFactor {
	var out []store.SealedFactor
	for _, f := range factors {
		if f.Kind == store.FactorPasskey && f.RPID == rp.ID {
			out = append(out, f)
		}
	}
	return out
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func unb64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func credentialList(factors []store.SealedFactor) []map[string]any {
	out := []map[string]any{}
	for _, f := range factors {
		out = append(out, map[string]any{"type": "public-key", "id": f.CredentialID})
	}
	return out
}

// startPasskeyAssertion makes a challenge for userID and purpose, and
// answers the options for navigator.credentials.get with their passkeys.
func (s *Server) startPasskeyAssertion(w http.ResponseWriter, userID, purpose string) {
	rp, ok := s.relyingParty()
	if !ok {
		writeJSONError(w, http.StatusConflict, noPasskeys)
		return
	}
	factors, err := s.store.SealedFactors(userID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	passkeys := usablePasskeys(factors, rp)
	if len(passkeys) == 0 {
		writeJSONError(w, http.StatusNotFound, "You have no passkey for this address.")
		return
	}
	challenge, err := webauthn.NewChallenge()
	if err != nil {
		writeServerError(w, err, "make a challenge")
		return
	}
	if err := s.store.CreatePasskeyChallenge(userID, purpose, challenge); err != nil {
		writeServerError(w, err, "record the challenge")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"public_key": map[string]any{
		"challenge":        b64(challenge),
		"rpId":             rp.ID,
		"timeout":          passkeyTimeout,
		"userVerification": "preferred",
		"allowCredentials": credentialList(passkeys),
	}})
}

// PasskeyAssertion is what navigator.credentials.get answered, base64url.
type PasskeyAssertion struct {
	ID                string `json:"id"`
	ClientDataJSON    string `json:"client_data_json"`
	AuthenticatorData string `json:"authenticator_data"`
	Signature         string `json:"signature"`
}

// challengeOf reads the challenge a browser says it answered.
func challengeOf(clientDataJSON []byte) []byte {
	var cd struct {
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(clientDataJSON, &cd) != nil {
		return nil
	}
	b, _ := unb64(cd.Challenge)
	return b
}

// checkPasskey accepts an answer from one of the user's passkeys to a
// challenge sent them for purpose, once. It answers why one is refused,
// or "" when it is accepted.
func (s *Server) checkPasskey(user *models.User, factors []store.SealedFactor, purpose string, a *PasskeyAssertion) (string, error) {
	const notAccepted = "That passkey was not accepted. Try again, or use another way."
	rp, ok := s.relyingParty()
	if !ok {
		return noPasskeys, nil
	}
	var factor *store.SealedFactor
	for _, f := range usablePasskeys(factors, rp) {
		if f.CredentialID == strings.TrimRight(a.ID, "=") {
			factor = &f
		}
	}
	if factor == nil {
		return "That passkey is not one of yours here.", nil
	}
	clientData, err1 := unb64(a.ClientDataJSON)
	authData, err2 := unb64(a.AuthenticatorData)
	sig, err3 := unb64(a.Signature)
	publicKey, err4 := unb64(factor.PublicKey)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return notAccepted, nil
	}
	challenge := challengeOf(clientData)
	switch err := s.store.TakePasskeyChallenge(user.ID, purpose, challenge); {
	case errors.Is(err, store.ErrPasskeyChallengeNotFound):
		return "That passkey answered too late, or answered something else. Try again.", nil
	case err != nil:
		return "", err
	}
	was := uint32(factor.LastStep)
	count, err := rp.VerifyAssertion(webauthn.Credential{PublicKey: publicKey, SignCount: was}, challenge, clientData, authData, sig)
	if errors.Is(err, webauthn.ErrCounterBackward) {
		log.Printf("passkey %s of %s: %v", factor.ID, user.Email, err)
		return "That passkey was not accepted: it looks like a copy. Use another way, and tell whoever manages users.", nil
	}
	if err != nil {
		return notAccepted, nil
	}
	if err := s.store.UsePasskey(factor.ID, was, count); err != nil {
		if errors.Is(err, store.ErrPasskeyUsed) {
			return notAccepted, nil
		}
		return "", err
	}
	return "", nil
}

// GET /api/v1/auth/login/factor: the kinds of second factor that can
// finish a sign-in, as the challenge cookie says.
func (s *Server) handleLoginFactorKinds(w http.ResponseWriter, r *http.Request) {
	user, ok := s.signInChallengeUser(w, r)
	if !ok {
		return
	}
	factors, err := s.store.SealedFactors(user.ID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"factors": s.factorKinds(factors)})
}

// POST /api/v1/auth/login/factor/passkey: starts the second step of a
// sign-in with a passkey.
func (s *Server) handleLoginPasskeyOptions(w http.ResponseWriter, r *http.Request) {
	user, ok := s.signInChallengeUser(w, r)
	if !ok {
		return
	}
	s.startPasskeyAssertion(w, user.ID, store.PasskeySignIn)
}

// POST /api/v1/auth/me/factors/passkey/challenge: asks for a passkey to
// confirm it is them, to remove a factor or make recovery codes.
func (s *Server) handlePasskeyConfirmOptions(w http.ResponseWriter, r *http.Request) {
	s.startPasskeyAssertion(w, CurrentUser(r.Context()).ID, store.PasskeyConfirm)
}

// POST /api/v1/auth/me/factors/passkey: starts registering a passkey,
// answering the options for navigator.credentials.create.
func (s *Server) handleStartPasskey(w http.ResponseWriter, r *http.Request) {
	rp, ok := s.relyingParty()
	if !ok {
		writeJSONError(w, http.StatusConflict, noPasskeys)
		return
	}
	user := CurrentUser(r.Context())
	factors, err := s.store.SealedFactors(user.ID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	challenge, err := webauthn.NewChallenge()
	if err != nil {
		writeServerError(w, err, "make a challenge")
		return
	}
	if err := s.store.CreatePasskeyChallenge(user.ID, store.PasskeyRegister, challenge); err != nil {
		writeServerError(w, err, "record the challenge")
		return
	}
	params := []map[string]any{}
	for _, alg := range webauthn.Algorithms {
		params = append(params, map[string]any{"type": "public-key", "alg": alg})
	}
	display := user.Name
	if display == "" {
		display = user.Email
	}
	writeJSON(w, http.StatusOK, map[string]any{"public_key": map[string]any{
		"rp":                 map[string]any{"id": rp.ID, "name": "BAM!"},
		"user":               map[string]any{"id": b64([]byte(user.ID)), "name": user.Email, "displayName": display},
		"challenge":          b64(challenge),
		"pubKeyCredParams":   params,
		"timeout":            passkeyTimeout,
		"excludeCredentials": credentialList(usablePasskeys(factors, rp)),
		"authenticatorSelection": map[string]any{
			"residentKey":      "discouraged",
			"userVerification": "preferred",
		},
		"attestation": "none",
	}})
}

// ConfirmPasskeyRequest is what navigator.credentials.create answered,
// base64url, with the name its owner gives it.
type ConfirmPasskeyRequest struct {
	Name              string `json:"name"`
	ClientDataJSON    string `json:"client_data_json"`
	AttestationObject string `json:"attestation_object"`
}

// POST /api/v1/auth/me/factors/passkey/confirm: registers the passkey the
// browser made; from then on, signing in can ask for it.
func (s *Server) handleConfirmPasskey(w http.ResponseWriter, r *http.Request) {
	var req ConfirmPasskeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > maxPasskeyName {
		writeJSONError(w, http.StatusBadRequest, "Give the passkey a name, of 100 characters at most, such as YubiKey or Phone.")
		return
	}
	rp, ok := s.relyingParty()
	if !ok {
		writeJSONError(w, http.StatusConflict, noPasskeys)
		return
	}
	user := CurrentUser(r.Context())
	clientData, err1 := unb64(req.ClientDataJSON)
	attestation, err2 := unb64(req.AttestationObject)
	if errors.Join(err1, err2) != nil {
		writeJSONError(w, http.StatusBadRequest, "That is not a passkey's answer.")
		return
	}
	existing, err := s.store.SealedFactors(user.ID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	challenge := challengeOf(clientData)
	switch err := s.store.TakePasskeyChallenge(user.ID, store.PasskeyRegister, challenge); {
	case errors.Is(err, store.ErrPasskeyChallengeNotFound):
		writeJSONError(w, http.StatusBadRequest, "The passkey answered too late, or answered something else. Start again.")
		return
	case err != nil:
		writeServerError(w, err, "take the challenge")
		return
	}
	cred, err := rp.VerifyRegistration(challenge, clientData, attestation)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "The passkey was not accepted: "+err.Error()+".")
		return
	}
	factor, err := s.store.AddPasskey(user.ID, req.Name, rp.ID, cred.ID, cred.PublicKey, cred.SignCount)
	if errors.Is(err, store.ErrPasskeyRegistered) {
		writeJSONError(w, http.StatusConflict, "That passkey is registered already.")
		return
	}
	if err != nil {
		writeServerError(w, err, "add the passkey")
		return
	}
	s.tellAccount(user, noticePasskeyAdded)
	// The first factor comes with recovery codes, shown this once.
	resp := map[string]any{"factor": factor}
	if len(existing) == 0 {
		codes, err := s.newRecoveryCodes(user.ID)
		if err != nil {
			writeServerError(w, err, "make recovery codes")
			return
		}
		resp["recovery_codes"] = codes
	}
	writeJSON(w, http.StatusOK, resp)
}
