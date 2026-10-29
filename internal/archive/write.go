package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// ErrSchema is returned for a database whose schema is not the one this
// bam's migrations make.
var ErrSchema = errors.New("archive: the schema is not current")

// Options are what Write writes.
type Options struct {
	// Type is the header's type: "backup", unless set.
	Type string
	// BamVersion is the version of bam writing it.
	BamVersion string
	// Kinds are the kinds of information to write; nil for every kind.
	// Authentication (KindAuth) is written only when named, or when Kinds
	// is nil and Secrets is set.
	Kinds []Kind
	// Secrets writes authentication when Kinds is nil.
	Secrets bool
	// Split, when more than 0, is the most bytes a part may hold: the rows
	// go in the first part, with the files that fit, and the rest of the
	// files in parts after it. A part holding a single file larger than
	// Split is the only one over it.
	Split int64
	// Now is when it is written; the current time, unless set.
	Now time.Time
	// setID, in tests, is the set's ID, so an archive can be written again
	// the same.
	setID string
}

// Summary is what Write wrote.
type Summary struct {
	Parts     int
	Set       string
	Tables    map[string]int
	Files     int
	FileBytes int64
	Missing   []string
}

// afterTable, in tests, is called once each table is read, to change the
// database while the rest are.
var afterTable func(table string)

// filesNamed are the columns whose values name a stored file, with the
// prefix of its key (filestore.Images).
var filesNamed = map[string][]struct{ column, prefix string }{
	"asset_photos": {{"id", "photos/"}, {"thumb_id", "photos/"}},
	"images":       {{"id", "images/"}},
}

// Write writes an archive of st and the files in blobs that its rows name,
// opening each part with open, numbered from 1, and closing it once
// written. The rows are read in one transaction, so they are as they were
// at one moment, however much changes while they are read; the files are
// copied after, and one removed by then is listed in the manifest as
// missing.
func Write(ctx context.Context, st *store.Store, blobs filestore.Blobs, open func(part int) (io.WriteCloser, error), opts Options) (Summary, error) {
	kinds := chosenKinds(opts)
	if opts.Type == "" {
		opts.Type = "backup"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	hdr := Header{
		Format:        Format,
		FormatVersion: FormatVersion,
		Type:          opts.Type,
		BamVersion:    opts.BamVersion,
		CreatedAt:     opts.Now.UTC().Format(time.RFC3339),
		Dialect:       st.Dialect,
		Kinds:         kinds,
		Secrets:       hasKind(kinds, KindAuth),
		Set:           Set{ID: opts.setID, Part: 1},
	}
	if hdr.Set.ID == "" {
		hdr.Set.ID = uuid.NewString()
	}
	sum := Summary{Set: hdr.Set.ID, Tables: map[string]int{}}

	txOpts := &sql.TxOptions{ReadOnly: true}
	if st.Dialect != "sqlite" {
		// SQLite's transactions are snapshots already, and its one
		// connection takes no options.
		txOpts.Isolation = sql.LevelRepeatableRead
	}
	tx, err := st.DB.BeginTx(ctx, txOpts)
	if err != nil {
		return sum, fmt.Errorf("starting to read: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read only
	if err := st.TxQueryRow(tx, "SELECT MAX(version) FROM schema_migrations").Scan(&hdr.SchemaVersion); err != nil {
		return sum, fmt.Errorf("reading the schema's version: %w", err)
	}
	if hdr.SchemaVersion != migrations.Latest() {
		// Tables describes the schema as this bam's migrations leave it.
		return sum, fmt.Errorf("%w: the database's schema is at version %d and this bam's at %d; run bam migrate with the bam that matches it", ErrSchema, hdr.SchemaVersion, migrations.Latest())
	}

	p, err := startPart(open, hdr)
	if err != nil {
		return sum, err
	}
	defer func() { p.abort() }()
	named := map[string]bool{}
	for i := range Tables {
		t := &Tables[i]
		if !hasKind(kinds, t.Kind) {
			continue
		}
		n, err := writeTable(ctx, st, tx, p, t, hdr.Secrets, named)
		if err != nil {
			return sum, fmt.Errorf("writing %s: %w", t.Name, err)
		}
		sum.Tables[t.Name] = n
		if afterTable != nil {
			afterTable(t.Name)
		}
	}
	if err := tx.Commit(); err != nil {
		return sum, fmt.Errorf("finishing reading: %w", err)
	}
	p.man.Tables = sum.Tables

	files, missing, err := sizeFiles(ctx, blobs, named)
	if err != nil {
		return sum, err
	}
	sum.Missing = missing
	p.man.Missing = missing
	parts, err := planParts(p, files, opts.Split)
	if err != nil {
		return sum, err
	}
	sum.Parts = len(parts)
	for i, keys := range parts {
		if i > 0 {
			if err := p.finish(len(parts), false); err != nil {
				return sum, err
			}
			next := hdr
			next.Set.Part = i + 1
			next.Set.Parts = len(parts)
			if p, err = startPart(open, next); err != nil {
				return sum, err
			}
			p.man.Missing = []string{}
		}
		for _, f := range keys {
			n, err := p.addFile(ctx, blobs, f)
			if errors.Is(err, filestore.ErrNotFound) {
				sum.Missing = append(sum.Missing, f.Key)
				p.man.Missing = append(p.man.Missing, f.Key)
				continue
			}
			if err != nil {
				return sum, fmt.Errorf("writing %s: %w", f.Key, err)
			}
			sum.Files++
			sum.FileBytes += n
		}
	}
	return sum, p.finish(len(parts), true)
}

// chosenKinds are the kinds Write writes, in the order of Kinds.
func chosenKinds(opts Options) []Kind {
	var out []Kind
	for _, k := range Kinds {
		if opts.Kinds == nil && (k != KindAuth || opts.Secrets) || hasKind(opts.Kinds, k) {
			out = append(out, k)
		}
	}
	return out
}

func hasKind(kinds []Kind, k Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// selectRows is the query reading a table's rows in the order of its key.
func selectRows(t *Table) string {
	cols := make([]string, len(t.Columns))
	from := t.Name + " t"
	for i, c := range t.Columns {
		cols[i] = "t." + c.Name
		if t.ByPermissionKey && c.Name == "permission_key" {
			cols[i] = "p.permission_key"
			from += " JOIN permissions p ON p.id = t.permission_id"
		}
	}
	order := make([]string, len(t.Key))
	for i, k := range t.Key {
		order[i] = "t." + k
		if t.ByPermissionKey && k == "permission_key" {
			order[i] = "p.permission_key"
		}
	}
	return "SELECT " + strings.Join(cols, ", ") + " FROM " + from + " ORDER BY " + strings.Join(order, ", ")
}

// writeTable writes a table's rows, in data/, and in auth/ its secret
// columns, with its key, or the whole of it when it is authentication.
// It notes the files its rows name in named.
func writeTable(ctx context.Context, st *store.Store, tx *sql.Tx, p *part, t *Table, secrets bool, named map[string]bool) (int, error) {
	rows, err := tx.QueryContext(ctx, st.Rebind(selectRows(t)))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	data := chunks{p: p, dir: DataDir + t.Name + "/"}
	auth := chunks{p: p, dir: AuthDir + t.Name + "/"}
	if t.Kind == KindAuth {
		data.dir = auth.dir
	}
	vals := make([]any, len(t.Columns))
	ptrs := make([]any, len(vals))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	n := 0
	var line, secret []byte
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		line, secret = append(line[:0], '{'), append(secret[:0], '{')
		for i, c := range t.Columns {
			isKey := hasString(t.Key, c.Name)
			isSecret := t.secret(c.Name)
			if !isSecret || isKey {
				if line, err = appendField(line, c, vals[i]); err != nil {
					return n, err
				}
			}
			if secrets && (isSecret || isKey) && len(t.Secret) > 0 {
				if secret, err = appendField(secret, c, vals[i]); err != nil {
					return n, err
				}
			}
		}
		for _, f := range filesNamed[t.Name] {
			if s, ok := text(vals[colIndex(t, f.column)]); ok && s != "" {
				named[f.prefix+s] = true
			}
		}
		if err := data.add(append(line, '}', '\n')); err != nil {
			return n, err
		}
		if secrets && len(t.Secret) > 0 {
			if err := auth.add(append(secret, '}', '\n')); err != nil {
				return n, err
			}
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	if err := data.flush(); err != nil {
		return n, err
	}
	return n, auth.flush()
}

func hasString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func colIndex(t *Table, name string) int {
	for i, c := range t.Columns {
		if c.Name == name {
			return i
		}
	}
	panic("archive: no column " + t.Name + "." + name)
}

// appendField appends "name":value, and a comma before it when another
// field comes first.
func appendField(b []byte, c Column, v any) ([]byte, error) {
	if b[len(b)-1] != '{' {
		b = append(b, ',')
	}
	name, _ := json.Marshal(c.Name)
	b = append(append(b, name...), ':')
	val, err := encodeValue(c, v)
	if err != nil {
		return b, fmt.Errorf("column %s: %w", c.Name, err)
	}
	return append(b, val...), nil
}

// encodeValue writes a value as JSON, the same way whatever the dialect
// gave it as.
func encodeValue(c Column, v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	switch c.Type {
	case Text:
		s, ok := text(v)
		if !ok {
			return nil, fmt.Errorf("text as %T", v)
		}
		return json.Marshal(s)
	case Int:
		switch x := v.(type) {
		case int64:
			return strconv.AppendInt(nil, x, 10), nil
		case []byte, string:
			s, _ := text(x)
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return nil, err
			}
			return strconv.AppendInt(nil, n, 10), nil
		}
	case Float:
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case float32:
			f = float64(x)
		case int64:
			f = float64(x)
		case []byte, string:
			s, _ := text(x)
			var err error
			if f, err = strconv.ParseFloat(s, 64); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("float as %T", v)
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%v is not a number JSON holds", f)
		}
		return json.Marshal(f)
	case Bool:
		switch x := v.(type) {
		case bool:
			return json.Marshal(x)
		case int64:
			return json.Marshal(x != 0)
		case []byte, string:
			s, _ := text(x)
			switch strings.ToLower(s) {
			case "1", "t", "true":
				return []byte("true"), nil
			case "0", "f", "false":
				return []byte("false"), nil
			}
		}
	case Time:
		t, err := asTime(v)
		if err != nil {
			return nil, err
		}
		return json.Marshal(FormatTime(t))
	case JSON:
		s, ok := text(v)
		if !ok {
			return nil, fmt.Errorf("JSON as %T", v)
		}
		if !json.Valid([]byte(s)) {
			return nil, errors.New("holds what is not JSON")
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(s)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return nil, fmt.Errorf("%s as %T", c.Type, v)
}

func text(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}

// FormatTime is how an archive writes a time: RFC 3339 in UTC, to the
// microsecond, which every dialect keeps.
func FormatTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.999999Z07:00")
}

// timeLayouts are the ways a time may come back as text.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func asTime(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	s, ok := text(v)
	if !ok {
		return time.Time{}, fmt.Errorf("time as %T", v)
	}
	s = strings.TrimSuffix(s, " +0000 UTC")
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("time %q", s)
}

// chunks gathers a table's rows into members of at most RowChunk bytes.
type chunks struct {
	p   *part
	dir string
	buf bytes.Buffer
	n   int
}

func (c *chunks) add(line []byte) error {
	if c.buf.Len() > 0 && c.buf.Len()+len(line) > RowChunk {
		if err := c.flush(); err != nil {
			return err
		}
	}
	c.buf.Write(line)
	return nil
}

func (c *chunks) flush() error {
	if c.buf.Len() == 0 {
		return nil
	}
	c.n++
	err := c.p.addBytes(fmt.Sprintf("%s%04d.jsonl", c.dir, c.n), c.buf.Bytes())
	c.buf.Reset()
	return err
}

// stored is a file to copy: its key and its size.
type stored struct {
	Key  string
	Size int64
}

// sizeFiles finds the size of each file named, in the order of key, and
// those the store does not have.
func sizeFiles(ctx context.Context, blobs filestore.Blobs, named map[string]bool) ([]stored, []string, error) {
	var files []stored
	found := map[string]bool{}
	prefixes := map[string]bool{}
	for _, fs := range filesNamed {
		for _, f := range fs {
			prefixes[f.prefix] = true
		}
	}
	for prefix := range prefixes {
		err := blobs.List(ctx, prefix, func(o filestore.Object) error {
			if named[o.Key] {
				files = append(files, stored{o.Key, o.Size})
				found[o.Key] = true
			}
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("listing stored files: %w", err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Key < files[j].Key })
	missing := []string{}
	for key := range named {
		if !found[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return files, missing, nil
}

// partSlack is room kept in each part for what is not yet written when
// parts are planned: the gzip stream's buffer and end, the tar's end, and
// the manifest's fixed part.
const partSlack = 256 << 10

// fileCost estimates the bytes a file adds to a part: its own, a little
// more once gzip has failed to make them smaller, its tar header and
// padding, and its line in the manifest.
func fileCost(f stored) int64 {
	return f.Size + f.Size/500 + 1024 + int64(len(f.Key)) + 128
}

// planParts shares the files out among parts: as many as fit in the first,
// after the rows, then in each after it in turn. Without split, every file
// goes in the first.
func planParts(p *part, files []stored, split int64) ([][]stored, error) {
	if split <= 0 {
		return [][]stored{files}, nil
	}
	used, err := p.size()
	if err != nil {
		return nil, err
	}
	used += partSlack
	parts := [][]stored{{}}
	for _, f := range files {
		cur := &parts[len(parts)-1]
		if used+fileCost(f) > split && (len(*cur) > 0 || len(parts) == 1) {
			parts = append(parts, []stored{})
			cur = &parts[len(parts)-1]
			used = partSlack
		}
		*cur = append(*cur, f)
		used += fileCost(f)
	}
	return parts, nil
}

// counter counts the bytes written through it.
type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// part is an archive being written.
type part struct {
	out  io.WriteCloser
	cnt  *counter
	gz   *gzip.Writer
	tw   *tar.Writer
	man  Manifest
	done bool
}

func startPart(open func(int) (io.WriteCloser, error), hdr Header) (*part, error) {
	out, err := open(hdr.Set.Part)
	if err != nil {
		return nil, fmt.Errorf("opening part %d: %w", hdr.Set.Part, err)
	}
	cnt := &counter{w: out}
	gz := gzip.NewWriter(cnt)
	p := &part{out: out, cnt: cnt, gz: gz, tw: tar.NewWriter(gz), man: Manifest{Header: hdr, Members: map[string]Member{}}}
	b, err := json.MarshalIndent(hdr, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := p.addBytes(HeaderPath, append(b, '\n')); err != nil {
		p.abort()
		return nil, err
	}
	if err := p.addBytes(ReadmePath, []byte(readme)); err != nil {
		p.abort()
		return nil, err
	}
	return p, nil
}

func (p *part) header(path string, size int64) error {
	return p.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     path,
		Size:     size,
		Mode:     0o600,
		ModTime:  p.modTime(),
		Format:   tar.FormatPAX,
	})
}

func (p *part) modTime() time.Time {
	t, _ := time.Parse(time.RFC3339, p.man.CreatedAt)
	return t
}

func (p *part) addBytes(path string, b []byte) error {
	if err := p.header(path, int64(len(b))); err != nil {
		return err
	}
	if _, err := p.tw.Write(b); err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	p.man.Members[path] = Member{Size: int64(len(b)), SHA256: hex.EncodeToString(sum[:])}
	return nil
}

// addFile copies a stored file in, as files/<key>, and returns its size.
func (p *part) addFile(ctx context.Context, blobs filestore.Blobs, f stored) (int64, error) {
	rc, err := blobs.Get(ctx, f.Key)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	path := FilesDir + f.Key
	if err := p.header(path, f.Size); err != nil {
		return 0, err
	}
	h := sha256.New()
	n, err := io.Copy(p.tw, io.TeeReader(io.LimitReader(rc, f.Size+1), h))
	if err != nil && !errors.Is(err, tar.ErrWriteTooLong) {
		return n, err
	}
	if n != f.Size || err != nil {
		return n, fmt.Errorf("the store listed %d bytes and held another number", f.Size)
	}
	p.man.Members[path] = Member{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}
	p.man.Files++
	p.man.FileBytes += n
	return n, nil
}

// size is how many bytes the part holds so far, its compressed stream
// flushed to tell.
func (p *part) size() (int64, error) {
	if err := p.tw.Flush(); err != nil {
		return 0, err
	}
	if err := p.gz.Flush(); err != nil {
		return 0, err
	}
	return p.cnt.n, nil
}

// finish writes the manifest and closes the part.
func (p *part) finish(parts int, last bool) error {
	p.man.Set.Parts = parts
	p.man.Last = last
	if p.man.Missing == nil {
		p.man.Missing = []string{}
	}
	b, err := json.MarshalIndent(p.man, "", "  ")
	if err != nil {
		return err
	}
	if err := p.header(ManifestPath, int64(len(b)+1)); err != nil {
		return err
	}
	if _, err := p.tw.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := p.tw.Close(); err != nil {
		return err
	}
	if err := p.gz.Close(); err != nil {
		return err
	}
	p.done = true
	return p.out.Close()
}

// abort closes a part left unfinished by an error.
func (p *part) abort() {
	if !p.done {
		p.done = true
		_ = p.out.Close()
	}
}
