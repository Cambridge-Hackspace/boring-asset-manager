package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Icons as an archive (README §3.7): the whole lexicon out, as a folder
// the isolex compiler checks as it is, and library files in.

const (
	// archiveBodyLimit bounds an uploaded archive.
	archiveBodyLimit = 16 << 20
	// archiveExpandLimit bounds what an archive may expand to, whatever
	// its entries claim, against archives built to blow up.
	archiveExpandLimit = 64 << 20
	// archiveMaxFiles bounds the sources one archive brings.
	archiveMaxFiles = 4096
	// archiveEntryLimit is the most bytes one source can be: MaxIconSource
	// characters, four bytes each at most.
	archiveEntryLimit = store.MaxIconSource * 4
)

// archiveEpoch dates every entry of an export, so the same lexicon makes
// the same bytes.
var archiveEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// handleExportIcons sends what is published as a zip: _lexicon.ilx, the
// block bam writes, then library/ and categories/, as a build compiles
// them: GET /api/v1/icons/export
func (s *Server) handleExportIcons(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.IconLexiconFiles()
	if err != nil {
		writeServerError(w, err, "export the icons")
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: archiveEpoch})
		if err == nil {
			_, err = io.WriteString(fw, f.Text)
		}
		if err != nil {
			writeServerError(w, err, "export the icons")
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeServerError(w, err, "export the icons")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="icons.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// skippedEntry is an archive entry not imported, and why.
type skippedEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// handleImportIcons takes a zip of .ilx files into the library at their
// paths, as one change, refused whole if the icons would not compile:
// POST /api/v1/icon-library/import, multipart with the zip as file.
func (s *Server) handleImportIcons(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "A zip of .ilx files, as file, is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Archives are limited to %d bytes", archiveBodyLimit))
		return
	}
	sources, skipped, err := readIconArchive(data)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(sources) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "The archive holds no .ilx files to import", Details: skipped})
		return
	}
	result, err := s.actingStore(r).ImportIconLibrary(r.Context(), s.icons, sources)
	if err != nil {
		writeIconError(w, err, "import the icons")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"save": result.Save, "created": result.Created, "updated": result.Updated, "skipped": skipped})
}

// readIconArchive reads the library sources a zip holds. A folder the
// whole archive is in is not part of the paths, nor is library/, as an
// export writes them; categories' icons and the lexicon block an export
// writes are skipped, as is anything that is not .ilx.
func readIconArchive(data []byte) ([]store.IconSourceFile, []skippedEntry, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("the file is not a zip archive")
	}
	type entry struct {
		name string
		f    *zip.File
	}
	var entries []entry
	for _, f := range zr.File {
		name := f.Name
		base := path.Base(name)
		if strings.HasSuffix(name, "/") || strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(base, ".") {
			continue
		}
		entries = append(entries, entry{name, f})
	}
	// one folder holding everything is the archive's, not the paths'
	first := ""
	if len(entries) > 0 {
		first = entries[0].name
	}
	if top, _, ok := strings.Cut(first, "/"); ok && top != "library" && top != "categories" {
		all := true
		for _, e := range entries {
			if !strings.HasPrefix(e.name, top+"/") {
				all = false
				break
			}
		}
		if all {
			for i := range entries {
				entries[i].name = strings.TrimPrefix(entries[i].name, top+"/")
			}
		}
	}

	var sources []store.IconSourceFile
	var skipped []skippedEntry
	var total int64
	for _, e := range entries {
		name := strings.TrimPrefix(e.name, "library/")
		switch {
		case e.name == "_lexicon.ilx":
			skipped = append(skipped, skippedEntry{e.name, "bam writes the lexicon block itself, from the palette and settings"})
			continue
		case strings.HasPrefix(e.name, "categories/"):
			skipped = append(skipped, skippedEntry{e.name, "a category's icon: it is saved on its category's page"})
			continue
		case !strings.HasSuffix(name, ".ilx"):
			skipped = append(skipped, skippedEntry{e.name, "not an .ilx file"})
			continue
		}
		if err := store.ValidIconLibraryPath(name); err != nil {
			return nil, nil, fmt.Errorf("%s: %v", e.name, err)
		}
		if len(sources) == archiveMaxFiles {
			return nil, nil, fmt.Errorf("an archive may hold at most %d .ilx files", archiveMaxFiles)
		}
		rc, err := e.f.Open()
		if err != nil {
			return nil, nil, fmt.Errorf("%s cannot be read: %v", e.name, err)
		}
		text, err := io.ReadAll(io.LimitReader(rc, archiveEntryLimit+1))
		rc.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("%s cannot be read: %v", e.name, err)
		}
		if len(text) > archiveEntryLimit {
			return nil, nil, fmt.Errorf("%s is longer than a source may be", e.name)
		}
		if total += int64(len(text)); total > archiveExpandLimit {
			return nil, nil, fmt.Errorf("the archive expands to more than %d bytes", archiveExpandLimit)
		}
		sources = append(sources, store.IconSourceFile{Path: name, Source: string(text)})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, skipped, nil
}
