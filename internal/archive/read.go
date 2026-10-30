package archive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// ErrInvalid is returned for an archive that does not check out: not an
// archive, corrupted, cut short, or changed since it was written.
var ErrInvalid = errors.New("archive: not a valid archive")

// ErrNewer is returned for an archive written by a newer bam, whose format
// or schema this one does not know.
var ErrNewer = errors.New("archive: written by a newer bam")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Checked is what Check found an archive holds.
type Checked struct {
	Header   Header
	Manifest Manifest
	// Rows counts each table's rows, in data/ or, for authentication, in
	// auth/.
	Rows map[string]int
	// Files counts the files in files/, and their bytes.
	Files     int
	FileBytes int64
}

// tableByName finds an archived table.
func tableByName(name string) *Table {
	for i := range Tables {
		if Tables[i].Name == name {
			return &Tables[i]
		}
	}
	return nil
}

// members reads an archive's members in turn: header.json, and every one
// after it, refusing what a writer never writes.
type members struct {
	src  io.Reader
	gz   *gzip.Reader
	tr   *tar.Reader
	seen map[string]bool
}

func openMembers(r io.Reader) (*members, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, invalid("not gzipped: %v", err)
	}
	gz.Multistream(false)
	return &members{src: r, gz: gz, tr: tar.NewReader(gz), seen: map[string]bool{}}, nil
}

// next returns the next member, or io.EOF after the last.
func (m *members) next() (*tar.Header, error) {
	h, err := m.tr.Next()
	if err == io.EOF {
		return nil, io.EOF
	}
	if err != nil {
		return nil, invalid("%v", err)
	}
	if h.Typeflag != tar.TypeReg {
		return nil, invalid("%s is not a file", h.Name)
	}
	if h.Name != path.Clean(h.Name) || path.IsAbs(h.Name) || strings.HasPrefix(h.Name, "../") {
		return nil, invalid("%s is not a member's name", h.Name)
	}
	if m.seen[h.Name] {
		return nil, invalid("%s is in it twice", h.Name)
	}
	m.seen[h.Name] = true
	return h, nil
}

// end checks nothing follows the tar's end but the zeros that pad it, and
// nothing follows the gzip stream.
func (m *members) end() error {
	var buf [32 << 10]byte
	for {
		n, err := m.gz.Read(buf[:])
		if bytes.ContainsFunc(buf[:n], func(r rune) bool { return r != 0 }) {
			return invalid("something follows its end")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return invalid("%v", err)
		}
	}
	// The stream after gzip's: with Multistream off, Read stops at the
	// first one's end; anything after is not of the archive.
	if err := m.gz.Reset(m.src); err != io.EOF {
		return invalid("something follows its end")
	}
	return nil
}

// Check reads a whole archive, or one part of a set, and checks every
// member against the manifest: its size and SHA-256, and the rows each
// table has. It refuses an archive corrupted, cut short, padded, with
// members in another order or that the manifest does not list, and one
// from a newer bam (ErrNewer). It changes nothing.
func Check(r io.Reader) (*Checked, error) {
	src := bufio.NewReader(r)
	m, err := openMembers(src)
	if err != nil {
		return nil, err
	}
	c := &Checked{Rows: map[string]int{}}
	got := map[string]Member{}
	authRows := map[string]int{}
	first, gotManifest := true, false
	for {
		h, err := m.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if gotManifest {
			return nil, invalid("%s follows the manifest", h.Name)
		}
		if first != (h.Name == HeaderPath) {
			return nil, invalid("%s is not first, or header.json is", HeaderPath)
		}
		first = false
		hash := sha256.New()
		body := io.TeeReader(m.tr, hash)
		var size int64
		switch dir, rest, _ := strings.Cut(h.Name, "/"); {
		case h.Name == HeaderPath:
			b, err := readSmall(body, h.Size)
			if err != nil {
				return nil, err
			}
			if c.Header, err = readHeader(b); err != nil {
				return nil, err
			}
			size = int64(len(b))
		case h.Name == ManifestPath:
			b, err := readSmall(body, h.Size)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(b, &c.Manifest); err != nil {
				return nil, invalid("the manifest: %v", err)
			}
			gotManifest = true
			continue
		case h.Name == ReadmePath || c.Header.Package != nil && h.Name == c.Header.Package.Preview && h.Name != "":
			n, err := io.Copy(io.Discard, body)
			if err != nil {
				return nil, invalid("%s: %v", h.Name, err)
			}
			size = n
		case dir+"/" == DataDir || dir+"/" == AuthDir:
			table, _, ok := strings.Cut(rest, "/")
			t := tableByName(table)
			if !ok || t == nil {
				return nil, invalid("%s is of no table this bam knows", h.Name)
			}
			if dir+"/" == DataDir && t.Kind == KindAuth || dir+"/" == AuthDir && t.Kind != KindAuth && len(t.Secret) == 0 {
				return nil, invalid("%s is in the wrong directory", h.Name)
			}
			if c.Header.Set.Part > 1 {
				return nil, invalid("%s is in part %d; rows are in the first", h.Name, c.Header.Set.Part)
			}
			n, rows, err := countRows(body, h.Name)
			if err != nil {
				return nil, err
			}
			size = n
			if dir+"/" == AuthDir && t.Kind != KindAuth {
				authRows[table] += rows
			} else {
				c.Rows[table] += rows
			}
		case h.Name == IconsIndex || dir+"/" == IconsDir:
			if c.Header.FormatVersion < 2 || c.Header.Set.Part > 1 {
				return nil, invalid("%s is in an archive that keeps its icons in its rows, or in a later part", h.Name)
			}
			if h.Name != IconsIndex && !iconMember(rest) {
				return nil, invalid("%s is not in an isolex library folder as bam writes one", h.Name)
			}
			if h.Name == IconsIndex {
				b, err := readSmall(body, h.Size)
				if err != nil {
					return nil, err
				}
				var index IconsIndexFile
				if err := json.Unmarshal(b, &index); err != nil {
					return nil, invalid("%s: %v", IconsIndex, err)
				}
				size = int64(len(b))
			} else {
				src, err := readIconFile(h.Name, body)
				if err != nil {
					return nil, err
				}
				size = int64(len(src))
			}
		case dir+"/" == FilesDir:
			if !filestore.ValidKey(rest) || !strings.HasPrefix(rest, "photos/") && !strings.HasPrefix(rest, "images/") {
				return nil, invalid("%s is no file bam keeps", h.Name)
			}
			n, err := io.Copy(io.Discard, body)
			if err != nil {
				return nil, invalid("%s: %v", h.Name, err)
			}
			size = n
			c.Files++
			c.FileBytes += n
		default:
			return nil, invalid("%s is no member of an archive", h.Name)
		}
		got[h.Name] = Member{Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	if first {
		return nil, invalid("it is empty")
	}
	if !gotManifest {
		return nil, invalid("it has no manifest: cut short, or still being written")
	}
	if err := m.end(); err != nil {
		return nil, err
	}
	return c, c.matchManifest(got, authRows)
}

func readSmall(r io.Reader, size int64) ([]byte, error) {
	if size > 64<<20 {
		return nil, invalid("a header or manifest of %d bytes", size)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, invalid("%v", err)
	}
	return b, nil
}

func readHeader(b []byte) (Header, error) {
	var h Header
	if err := json.Unmarshal(b, &h); err != nil {
		return h, invalid("the header: %v", err)
	}
	if h.Format != Format {
		return h, invalid("the header names the format %q", h.Format)
	}
	if h.FormatVersion > FormatVersion {
		return h, fmt.Errorf("%w: its format is version %d, and this bam reads %d", ErrNewer, h.FormatVersion, FormatVersion)
	}
	if h.FormatVersion < 1 || h.Set.ID == "" || h.Set.Part < 1 || h.SchemaVersion < 1 {
		return h, invalid("the header is incomplete")
	}
	if h.SchemaVersion > migrations.Latest() {
		return h, fmt.Errorf("%w: its schema is at version %d, and this bam's at %d", ErrNewer, h.SchemaVersion, migrations.Latest())
	}
	for _, k := range h.Kinds {
		if !hasKind(Kinds, k) {
			return h, fmt.Errorf("%w: it holds information of a kind this bam does not know, %q", ErrNewer, k)
		}
	}
	return h, nil
}

// countRows counts a member's rows, each a JSON object on a line.
func countRows(r io.Reader, name string) (int64, int, error) {
	cr := &countingReader{r: r}
	sc := bufio.NewScanner(cr)
	sc.Buffer(make([]byte, 64<<10), RowChunk+1)
	rows := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' || !json.Valid(line) {
			return 0, 0, invalid("%s: row %d is not a JSON object", name, rows+1)
		}
		rows++
	}
	if err := sc.Err(); err != nil {
		return 0, 0, invalid("%s: %v", name, err)
	}
	return cr.n, rows, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)
	return n, err
}

// matchManifest checks what was read against what the manifest says.
func (c *Checked) matchManifest(got map[string]Member, authRows map[string]int) error {
	man := &c.Manifest
	hdr := man.Header
	hdr.Set.Parts = c.Header.Set.Parts
	a, _ := json.Marshal(hdr)
	b, _ := json.Marshal(c.Header)
	if !bytes.Equal(a, b) {
		return invalid("the manifest's header is not the header")
	}
	if man.Set.Parts < man.Set.Part || c.Header.Set.Parts != 0 && c.Header.Set.Parts != man.Set.Parts {
		return invalid("the manifest's set is not whole")
	}
	if man.Last != (man.Set.Part == man.Set.Parts) {
		return invalid("the manifest's last part is not its last")
	}
	var paths []string
	for p := range got {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		want, ok := man.Members[p]
		if !ok {
			return invalid("%s is not in the manifest", p)
		}
		if want != got[p] {
			return invalid("%s has changed since it was written: corrupted, or altered", p)
		}
	}
	if len(man.Members) != len(got) {
		for p := range man.Members {
			if _, ok := got[p]; !ok {
				return invalid("%s is in the manifest and not in the archive", p)
			}
		}
	}
	if c.Files != man.Files || c.FileBytes != man.FileBytes {
		return invalid("it holds %d files, and the manifest says %d", c.Files, man.Files)
	}
	if man.Set.Part > 1 {
		if len(man.Tables) > 0 {
			return invalid("part %d counts rows; rows are in the first", man.Set.Part)
		}
		return nil
	}
	for _, t := range Tables {
		want, listed := man.Tables[t.Name]
		if listed != hasKind(c.Header.Kinds, t.Kind) {
			return invalid("the manifest's tables are not those of its kinds")
		}
		if c.Rows[t.Name] != want {
			return invalid("%s has %d rows, and the manifest says %d", t.Name, c.Rows[t.Name], want)
		}
		if len(t.Secret) > 0 && t.Kind != KindAuth {
			if n := authRows[t.Name]; c.Header.Secrets && n != want || !c.Header.Secrets && n != 0 {
				return invalid("%s's secrets are for %d rows of %d", t.Name, n, want)
			}
		}
	}
	for name := range c.Rows {
		if _, ok := man.Tables[name]; !ok {
			return invalid("%s has rows, and is not in the manifest", name)
		}
	}
	if c.Header.Secrets != hasKind(c.Header.Kinds, KindAuth) {
		return invalid("the header's secrets and its kinds disagree")
	}
	return nil
}
