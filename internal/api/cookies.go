package api

import (
	"net/http"
	"strings"
	"time"
)

// SessionCookieName is the standard cookie name for BAM authentication sessions.
const SessionCookieName = "bam_session"

// DefaultSessionTTL is 7 days.
const DefaultSessionTTL = 7 * 24 * time.Hour

// SetSessionCookie sets the HTTP-only, SameSite=Lax authentication cookie.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration, baseURL string) {
	secure := isSecureRequest(r, baseURL)

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// ClearSessionCookie invalidates the authentication cookie on the client.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request, baseURL string) {
	secure := isSecureRequest(r, baseURL)

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// GetSessionCookie extracts the session token from the request cookie header.
func GetSessionCookie(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return cookie.Value
}

func isSecureRequest(r *http.Request, baseURL string) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	if strings.HasPrefix(baseURL, "https://") {
		return true
	}
	return false
}
