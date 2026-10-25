package mcpserver

// Uncovered lists the API routes no tool reaches yet, each with the issue
// that will cover it. It is empty: MCP reaches every route but those
// Refused names (#33). The coverage test fails for any route neither a tool
// reaches nor this list or Refused names, so a route added later cannot be
// forgotten.
var Uncovered = map[string]string{}

// Refused lists the API routes deliberately out of reach over MCP, and why.
var Refused = map[string]string{
	"GET /api/v1/oauth/clients/{id}":      "the consent page's, where a person signed in approves an assistant; never an assistant's own",
	"POST /api/v1/oauth/authorize":        "the consent page's, where a person signed in approves an assistant; never an assistant's own",
	"POST /api/v1/auth/login":             "signing in with a password; an assistant signs in by OAuth (#44) or presents a token",
	"POST /api/v1/auth/logout":            "an assistant has no session to end; revoke_token on its own token signs it out",
	"PUT /api/v1/auth/password":           "a person's password is theirs to change, signed in, so a misled assistant cannot lock them out (#43)",
	"POST /api/v1/tokens":                 "tokens are made from a signed-in session, so a misled assistant cannot leave itself a way back in (#43)",
	"POST /api/v1/auth/tokens":            "tokens are made from a signed-in session, so a misled assistant cannot leave itself a way back in (#43)",
	"POST /api/v1/templates/preview-data": "the data the editor's live renderer draws a label with in the browser; preview_template draws with the same data on the server",
	"GET /api/v1/events/live":             "a WebSocket, for devices; over MCP, subscribe to bam://events, which the server keeps up to date from GET /api/v1/events",
	"GET /api/v1/icons/export":            "a zip of the icons, for people to download; over MCP, list_icon_sources reads every source",
	"POST /api/v1/icon-library/import":    "a zip uploaded by people; over MCP, save_icon_library_file saves each source",
}
