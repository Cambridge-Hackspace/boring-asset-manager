package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/epc"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Long-range RFID tags (docs/rfid.md): printers that write a label's tag
// as they print it, and the scheme that turns codes into EPCs and back.

// handleRFIDSettings: GET /api/v1/settings/rfid (settings.manage).
func (s *Server) handleRFIDSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.RFIDSettings()
	if err != nil {
		writeServerError(w, err, "read the RFID settings")
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// handleSetRFIDSettings: PUT /api/v1/settings/rfid (settings.manage).
func (s *Server) handleSetRFIDSettings(w http.ResponseWriter, r *http.Request) {
	var req store.RFIDSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetRFIDSettings(req); errors.Is(err, store.ErrInvalidSetting) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeServerError(w, err, "save the RFID settings")
		return
	}
	s.handleRFIDSettings(w, r)
}

// tagWriter gives the EPC each label's tag is written with, on a printer
// set to write tags. A nil one writes none.
type tagWriter struct {
	s   *Server
	set store.RFIDSettings
}

// tagWriterFor is the tagWriter for a plan: nil unless its printer writes
// tags.
func (s *Server) tagWriterFor(plan printers.Plan) (*tagWriter, error) {
	tp, ok := plan.(printers.TagPlan)
	if !ok || !tp.EncodesTags() {
		return nil, nil
	}
	set, err := s.store.RFIDSettings()
	if err != nil {
		return nil, err
	}
	return &tagWriter{s: s, set: set}, nil
}

// tagError says why an asset's tag cannot be written, for the person
// printing.
type tagError string

func (e tagError) Error() string { return string(e) }

// isTagError reports whether err says why a tag cannot be written.
func isTagError(err error) bool {
	var te tagError
	return errors.As(err, &te)
}

// epc is the EPC for an asset's tag, or "" from a nil tagWriter. An asset
// with no code of the type tags hold, or one the scheme cannot write, is
// a tagError.
func (t *tagWriter) epc(assetID string) (string, error) {
	if t == nil || assetID == "" {
		return "", nil
	}
	a, err := t.s.store.GetAssetByID(assetID)
	if err != nil {
		return "", err
	}
	code := a.FirstCode()
	if t.set.CodeType != "" {
		code = ""
		for _, k := range a.GeneratorKeys {
			if k.GeneratorType == t.set.CodeType {
				code = k.AssignedCode
			}
		}
	}
	if code == "" {
		return "", tagError(a.Asset.Name + " has no code to write to its RFID tag")
	}
	out, err := t.set.EPC().Encode(code)
	if err != nil {
		return "", tagError(fmt.Sprintf("%s's RFID tag: %s", a.Asset.Name, strings.TrimPrefix(err.Error(), epc.ErrInvalid.Error()+": ")))
	}
	return out, nil
}

// codeFromEPC is the code an EPC a reader typed holds, under the
// instance's scheme, and whether it holds one.
func (s *Server) codeFromEPC(text string) (string, bool) {
	set, err := s.store.RFIDSettings()
	if err != nil {
		return "", false
	}
	code, ok := set.EPC().Decode(text)
	if !ok || code == text {
		return "", false
	}
	return code, true
}
