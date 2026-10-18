package mcpserver

// Uncovered lists the API routes no tool reaches yet, each with the issue
// that will cover it. The coverage test fails for any route neither a tool
// reaches nor this list names, so a route added later cannot be forgotten;
// each part of #33 takes its routes off.
var Uncovered = map[string]string{
	"POST /api/v1/auth/login":                   "#43",
	"POST /api/v1/auth/logout":                  "#43",
	"PUT /api/v1/auth/password":                 "#43",
	"GET /api/v1/users":                         "#41",
	"POST /api/v1/users":                        "#41",
	"PATCH /api/v1/users/{id}/status":           "#41",
	"DELETE /api/v1/users/{id}":                 "#41",
	"PUT /api/v1/users/{id}/name":               "#41",
	"PUT /api/v1/users/{id}/password":           "#41",
	"PUT /api/v1/users/{id}/roles":              "#41",
	"GET /api/v1/roles":                         "#41",
	"POST /api/v1/roles":                        "#41",
	"PUT /api/v1/roles/{id}":                    "#41",
	"DELETE /api/v1/roles/{id}":                 "#41",
	"GET /api/v1/scopes":                        "#41",
	"POST /api/v1/scopes":                       "#41",
	"PUT /api/v1/scopes/{id}":                   "#41",
	"DELETE /api/v1/scopes/{id}":                "#41",
	"PUT /api/v1/categories/{id}/scopes":        "#41",
	"GET /api/v1/tokens":                        "#43",
	"POST /api/v1/tokens":                       "#43",
	"DELETE /api/v1/tokens/{id}":                "#43",
	"GET /api/v1/auth/tokens":                   "#43",
	"POST /api/v1/auth/tokens":                  "#43",
	"DELETE /api/v1/auth/tokens/{id}":           "#43",
	"GET /api/v1/tokens/connections":            "#41",
	"GET /api/v1/events/live":                   "#42",
	"GET /api/v1/reports":                       "#40",
	"POST /api/v1/reports":                      "#40",
	"POST /api/v1/reports/preview":              "#40",
	"GET /api/v1/reports/{id}":                  "#40",
	"PUT /api/v1/reports/{id}":                  "#40",
	"DELETE /api/v1/reports/{id}":               "#40",
	"POST /api/v1/reports/{id}/default":         "#40",
	"GET /api/v1/reports/{id}/run":              "#40",
	"GET /api/v1/reports/{id}/sections/{n}/csv": "#40",
	"GET /api/v1/notifiers/drivers":             "#41",
	"GET /api/v1/notifiers/deliveries":          "#41",
	"GET /api/v1/notifiers":                     "#41",
	"POST /api/v1/notifiers":                    "#41",
	"GET /api/v1/notifiers/{id}":                "#41",
	"PUT /api/v1/notifiers/{id}":                "#41",
	"DELETE /api/v1/notifiers/{id}":             "#41",
	"POST /api/v1/notifiers/{id}/test":          "#41",
}

// Refused lists the API routes deliberately out of reach over MCP, and why.
var Refused = map[string]string{}
