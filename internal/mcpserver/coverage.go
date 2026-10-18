package mcpserver

// Uncovered lists the API routes no tool reaches yet, each with the issue
// that will cover it. The coverage test fails for any route neither a tool
// reaches nor this list names, so a route added later cannot be forgotten;
// each part of #33 takes its routes off.
var Uncovered = map[string]string{
	"POST /api/v1/auth/login":         "#43",
	"POST /api/v1/auth/logout":        "#43",
	"PUT /api/v1/auth/password":       "#43",
	"GET /api/v1/tokens":              "#43",
	"POST /api/v1/tokens":             "#43",
	"DELETE /api/v1/tokens/{id}":      "#43",
	"GET /api/v1/auth/tokens":         "#43",
	"POST /api/v1/auth/tokens":        "#43",
	"DELETE /api/v1/auth/tokens/{id}": "#43",
	"GET /api/v1/events/live":         "#42",
}

// Refused lists the API routes deliberately out of reach over MCP, and why.
var Refused = map[string]string{}
