package api

import (
	"log"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// writeStoreError answers an error from a store method that checks what it
// is given, or a validator: 409 when another row already holds a unique
// value, 500 when the database failed, and otherwise 400 with the error's
// message, which is about the request. action says what was being done, such
// as "create the asset".
func writeStoreError(w http.ResponseWriter, err error, action string) {
	switch {
	case store.IsUniqueViolation(err):
		writeJSONError(w, http.StatusConflict, "Could not "+action+": something with the same name or code already exists")
	case store.IsDatabaseFailure(err):
		writeServerError(w, err, action)
	default:
		writeJSONError(w, http.StatusBadRequest, err.Error())
	}
}

// writeServerError answers a failure of the server itself with 500. The
// error goes to the log, not to the client, which has no use for SQL.
func writeServerError(w http.ResponseWriter, err error, action string) {
	log.Printf("failed to %s: %v", action, err)
	writeJSONError(w, http.StatusInternalServerError, "Failed to "+action)
}
