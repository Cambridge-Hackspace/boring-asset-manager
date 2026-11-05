package mcpserver

// Uncovered lists the API routes no tool reaches yet, each with the issue
// that will cover it. It is empty: MCP reaches every route but those
// Refused names (#33). The coverage test fails for any route neither a tool
// reaches nor this list or Refused names, so a route added later cannot be
// forgotten.
var Uncovered = map[string]string{}

// Refused lists the API routes deliberately out of reach over MCP, and why.
var Refused = map[string]string{
	"GET /api/v1/oauth/clients/{id}":               "the consent page's, where a person signed in approves an assistant; never an assistant's own",
	"POST /api/v1/oauth/authorize":                 "the consent page's, where a person signed in approves an assistant; never an assistant's own",
	"POST /api/v1/auth/login":                      "signing in with a password; an assistant signs in by OAuth (#44) or presents a token",
	"POST /api/v1/auth/login/local":                "the break-glass sign-in with a password, while passwords are off; an assistant signs in by OAuth (#44) or presents a token",
	"GET /api/v1/auth/providers/public":            "the sign-in page's buttons, for a browser signing in",
	"GET /api/v1/auth/providers/{key}/start":       "sends a browser to an identity provider to sign in; an assistant signs in by OAuth (#44) or presents a token",
	"GET /api/v1/auth/providers/{key}/callback":    "an identity provider's answer to a browser signing in",
	"POST /api/v1/auth/providers/{key}/callback":   "an identity provider's answer to a browser signing in",
	"GET /api/v1/auth/me/identities":               "a person's identities are theirs to see and link in a browser, signed in; list_user_identities shows someone's",
	"DELETE /api/v1/auth/me/identities/{identity}": "a person's identities are theirs to unlink, signed in; unlink_user_identity unlinks someone's",
	"POST /api/v1/auth/login/factor":               "the second step of signing in, with a code from a person's app; an assistant signs in by OAuth (#44) or presents a token",
	"GET /api/v1/auth/login/factor":                "the second step of signing in; an assistant signs in by OAuth (#44) or presents a token",
	"POST /api/v1/auth/login/factor/passkey":       "a passkey answers in its person's browser, at the second step of signing in",
	// A person's own account, by links emailed to them (#211): reaching
	// their inbox is what proves who they are, which an assistant cannot.
	"POST /api/v1/auth/login/factor/recover":         "asking, at the second step of signing in, for a lost second factor's removal by email; that is a person's own, from their inbox",
	"GET /api/v1/auth/factor-recovery":               "a link emailed to a person, to remove their lost second factor; that is a person's own, from their inbox",
	"POST /api/v1/auth/factor-recovery":              "a link emailed to a person, to remove their lost second factor; that is a person's own, from their inbox",
	"POST /api/v1/auth/password-reset":               "asking for a password reset by email; that is a person's own, from their inbox",
	"GET /api/v1/auth/password-reset":                "a link emailed to a person, to set a new password; that is a person's own, from their inbox",
	"POST /api/v1/auth/password-reset/confirm":       "a link emailed to a person, to set a new password; that is a person's own, from their inbox",
	"GET /api/v1/auth/invite":                        "an invitation emailed to a person, to set a password or sign in; that is a person's own, from their inbox",
	"POST /api/v1/auth/invite":                       "an invitation emailed to a person, to set a password or sign in; that is a person's own, from their inbox",
	"GET /api/v1/auth/me/factors":                    "a person's second factors are theirs to see and change, signed in; reset_user_factors removes someone's",
	"POST /api/v1/auth/me/factors/totp":              "a second factor is set up by its person, signed in, so a misled assistant cannot hold one over them (#189)",
	"POST /api/v1/auth/me/factors/totp/confirm":      "a second factor is set up by its person, signed in, so a misled assistant cannot hold one over them (#189)",
	"POST /api/v1/auth/me/factors/passkey":           "a passkey is registered by its person, in their browser, signed in (#191)",
	"POST /api/v1/auth/me/factors/passkey/confirm":   "a passkey is registered by its person, in their browser, signed in (#191)",
	"POST /api/v1/auth/me/factors/passkey/challenge": "a passkey answers in its person's browser, to confirm it is them (#191)",
	"DELETE /api/v1/auth/me/factors/{id}":            "removing a second factor takes its person's password and a current factor; reset_user_factors removes someone's",
	"POST /api/v1/auth/me/factors/recovery":          "recovery codes are shown once to their person, signed in, and new ones take a current factor (#190)",
	"POST /api/v1/auth/logout":                       "an assistant has no session to end; revoke_token on its own token signs it out",
	"PUT /api/v1/auth/password":                      "a person's password is theirs to change, signed in, so a misled assistant cannot lock them out (#43)",
	"PUT /api/v1/auth/theme":                         "whether the web interface is light or dark for a person, chosen in the browser; an assistant sees no interface (#168)",
	"POST /api/v1/tokens":                            "tokens are made from a signed-in session, so a misled assistant cannot leave itself a way back in (#43)",
	"POST /api/v1/auth/tokens":                       "tokens are made from a signed-in session, so a misled assistant cannot leave itself a way back in (#43)",
	"POST /api/v1/templates/preview-data":            "the data the editor's live renderer draws a label with in the browser; preview_template draws with the same data on the server",
	"GET /api/v1/events/live":                        "a WebSocket, for devices; over MCP, subscribe to bam://events, which the server keeps up to date from GET /api/v1/events",
	"GET /auth/saml/{key}/metadata":                  "BAM!'s SAML metadata, for an identity provider to read; Admin shows its address, and save_identity_provider sets the provider up",
	"GET /api/v1/relay/live":                         "a WebSocket, for relays, with a relay's own credential from pairing; never an assistant's",
	"POST /api/v1/relays/join":                       "a relay pairing with its code, sending its own key from its own machine; add_relay gives the code",
	"POST /api/v1/relays/renew":                      "a relay renewing its certificate with its own credential; never an assistant's",
	"POST /api/v1/relays/mesh/rotate":                "rotating the mesh root unpairs every relay at once, which stays with a person signed in",
	"GET /api/v1/icons/export":                       "a zip of the icons, for people to download; over MCP, list_icon_sources reads every source",
	"GET /api/v1/assets/export.xlsx":                 "a workbook, for people to open in a spreadsheet; over MCP, export_assets gives the same as CSV",
	"GET /api/v1/reports/{id}/sections/{n}/xlsx":     "a workbook, for people to open in a spreadsheet; over MCP, the section's CSV gives the same",
	"GET /api/v1/reports/{id}/xlsx":                  "a workbook, for people to open in a spreadsheet; over MCP, each section's CSV gives the same",
	"PUT /api/v1/roles/{id}/two-factor":              "how two-factor sign-in is turned off for the admin role, which stays with a person, so a misled assistant cannot weaken admins' sign-in; save_role sets it for other roles",
	"POST /api/v1/icon-library/import":               "a zip uploaded by people; over MCP, save_icon_library_file saves each source",
	"GET /api/v1/flows/{slug}/qr.svg":                "the picture on a flow's printed poster; over MCP, a flow is reached at /f/<slug>, and run with run_flow",
	"POST /api/v1/incoming/{id}":                     "an incoming hook's address, for a sender outside BAM! holding its secret; an assistant does what it does with the tools themselves",
	// Archives move between a person's own computer and the server;
	// replacing an instance stays with a signed-in person, as making tokens
	// does (#43, #117).
	"POST /api/v1/uploads":             "an archive uploaded in parts from a person's own computer, to replace an instance with; that stays with a signed-in person (#117)",
	"GET /api/v1/uploads/{id}":         "an archive uploaded in parts from a person's own computer, to replace an instance with; that stays with a signed-in person (#117)",
	"PUT /api/v1/uploads/{id}":         "an archive uploaded in parts from a person's own computer, to replace an instance with; that stays with a signed-in person (#117)",
	"DELETE /api/v1/uploads/{id}":      "an archive uploaded in parts from a person's own computer, to replace an instance with; that stays with a signed-in person (#117)",
	"GET /api/v1/uploads/{id}/content": "a backup taken before a replace, for a person to download to their own computer",
	"GET /api/v1/backup":               "a backup of the whole instance, for a person to download and keep safe; never an assistant's to carry off",
	"POST /api/v1/backup/check":        "checking an archive uploaded to replace the instance with; replacing stays with a signed-in person (#117)",
	"POST /api/v1/backup/replace":      "replacing the instance with a backup stays with a signed-in person, as making tokens does (#43, #117)",
	"GET /api/v1/backup/jobs/{id}":     "the progress of a check or replace a signed-in person began (#117)",
}
