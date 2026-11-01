// Package archive writes and reads bam's archives (docs/packages.md, #117): a
// backup of a whole instance, or a package of part of one, as a gzipped tar
// readable with tar and any JSON parser. Rows are JSON Lines, one directory
// per table; uploaded files are kept as stored; a header comes first and a
// manifest, with the size and SHA-256 of every member, comes last. Archives
// are written and read as streams, so memory stays bounded however large an
// instance is.
package archive

// FormatVersion is the version of the archive format this package writes.
// It is the format's own, apart from bam's. Version 2 (#146) keeps icon
// sources as an isolex library folder, icons/, rather than in their rows;
// version 1 archives are read as they are.
const FormatVersion = 2

// Format names the format in every header and manifest.
const Format = "bam-archive"

// Kind is a kind of information (#117, "Kinds of information"): what a
// restore or a replace chooses, each on its own.
type Kind string

// The kinds of information.
const (
	KindAssets   Kind = "assets"
	KindCatalog  Kind = "catalog"
	KindLabels   Kind = "labels"
	KindReports  Kind = "reports"
	KindIcons    Kind = "icons"
	KindFlows    Kind = "flows"
	KindPeople   Kind = "people"
	KindSettings Kind = "settings"
	// KindAuth is authentication: password hashes, sessions, API tokens,
	// OAuth, and notification channels' secrets. Only the CLI writes or
	// reads it (#117, "Authentication secrets").
	KindAuth Kind = "auth"
)

// Kinds lists the kinds of information, in the order they are described.
var Kinds = []Kind{KindAssets, KindCatalog, KindLabels, KindReports, KindIcons, KindFlows, KindPeople, KindSettings, KindAuth}

// ColType is how a column's values are written.
type ColType int

// The column types. Values are written one way, whatever the dialect: text
// as strings, numbers as numbers, booleans as booleans, times as RFC 3339
// in UTC, and JSON columns as JSON, not as strings of it. NULL is null.
const (
	Text ColType = iota
	Int
	Float
	Bool
	Time
	JSON
)

func (c ColType) String() string {
	return [...]string{"text", "int", "float", "bool", "time", "json"}[c]
}

// Column is a column of an archived table.
type Column struct {
	Name string
	Type ColType
}

// Table is an archived table.
type Table struct {
	Name string
	Kind Kind
	// Key is its primary key, which rows are written in the order of.
	Key     []string
	Columns []Column
	// Secret columns are written apart, in auth/<table>/, with the key, and
	// only when authentication is written.
	Secret []string
	// ByPermissionKey tables name a permission by its key, which is the
	// same on every instance, where the database has its ID, which is not.
	ByPermissionKey bool
}

// secret reports whether a column is kept in auth/.
func (t *Table) secret(col string) bool {
	for _, s := range t.Secret {
		if s == col {
			return true
		}
	}
	return false
}

// Excluded are the tables an archive leaves out, and why. Every table the
// migrations make is in Tables or here.
var Excluded = map[string]string{
	"schema_migrations":       "the schema's version is in the header; a restore migrates",
	"app_locks":               "rows that serialize changes, not information",
	"permissions":             "the same on every instance by key; grants are archived by permission key",
	"icon_build":              "made from the icon sources, again, when the server starts",
	"live_connections":        "connections open now, gone when the server stops",
	"flow_runs":               "runs under way, dropped when left 12 hours",
	"action_replies":          "devices' answers to a hook, for the moment it is triggered",
	"notification_deliveries": "the queue of notices to send, which a restore must not send again",
	"uploads":                 "archives being uploaded, staged apart from the files and gone after a day",
	"upload_parts":            "the parts of uploads under way",
}

// Header is header.json, the first member: what the archive is, before
// anything else is read.
type Header struct {
	Format        string `json:"format"`
	FormatVersion int    `json:"format_version"`
	// Type is "backup" or "package".
	Type          string `json:"type"`
	BamVersion    string `json:"bam_version"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     string `json:"created_at"`
	Dialect       string `json:"dialect"`
	// Kinds are the kinds of information it holds.
	Kinds []Kind `json:"kinds"`
	// Secrets is set when it holds authentication (KindAuth).
	Secrets bool `json:"secrets"`
	Set     Set  `json:"set"`
	// Package says what a package is: set on packages alone.
	Package *PackageInfo `json:"package,omitempty"`
}

// Set names the set of archives a backup split into parts makes, and this
// part's place in it. An archive in one part is part 1 of a set of its own.
type Set struct {
	ID   string `json:"id"`
	Part int    `json:"part"`
	// Parts is how many parts the set has: in every manifest, and in the
	// header of every part after the first, which is begun before it is
	// known.
	Parts int `json:"parts,omitempty"`
}

// Member is one member of an archive, as the manifest lists it.
type Member struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is manifest.json, the last member, written once everything
// else is: the header again, and what the archive holds.
type Manifest struct {
	Header
	// Tables counts the rows of each table, in data/ and in auth/, in the
	// part holding the rows (the first).
	Tables map[string]int `json:"tables,omitempty"`
	// Files counts the uploaded files in this part, and their bytes.
	Files     int   `json:"files"`
	FileBytes int64 `json:"file_bytes"`
	// Missing are files rows name that the store did not have.
	Missing []string `json:"missing_files"`
	// Members are every other member, by path.
	Members map[string]Member `json:"members"`
	// Last is set on the last part of a set.
	Last bool `json:"last"`
}

// The members' paths.
const (
	HeaderPath   = "header.json"
	ManifestPath = "manifest.json"
	ReadmePath   = "README.txt"
	DataDir      = "data/"
	AuthDir      = "auth/"
	FilesDir     = "files/"
	// IconsDir is the icons as an isolex library folder (#146): its
	// lexicon.ilx, library/<path> and categories/<icon>.ilx; IconsIndex
	// says whose each category's icon is.
	IconsDir   = "icons/"
	IconsIndex = "icons.json"
	// LexiconFile is the folder's lexicon block and palette, as bam writes
	// them from its palette and icon settings.
	LexiconFile = "lexicon.ilx"
)

// RowChunk is the most bytes of rows a member holds before the next
// begins: a tar member's size is written before it, so each is made in
// memory first.
const RowChunk = 8 << 20

// readme is README.txt, for whoever opens an archive without bam.
const readme = `This is an archive of a BAM! (Boring Asset Manager) instance, written by
bam backup, or downloaded from its Backups page. bam restore puts it back,
into a database of any kind. Or it is a package of part of one, written by
bam export or its Import & export page, which header.json says. Its format
is documented in bam's docs/backups.md.

header.json    what this archive is: bam's version, the schema's, when it
               was written, the kinds of information it holds, and its
               place in a set of parts, if it is one.
data/<table>/  the rows of each table, as JSON Lines: one object a row,
               in files of at most 8 MiB, in order.
auth/<table>/  authentication: password hashes, sessions, API tokens,
               OAuth, and notification channels' secrets. Only in archives
               the command line writes. Keep such an archive as carefully
               as the database itself.
files/         uploaded files, as stored: photos/<id> and images/<id>.
icons/         the icons' sources, as an isolex library folder that
               isolex check and isolex studio open as it is: lexicon.ilx
               (the lexicon block and the palette), library/ and each
               category's own icon in categories/<icon>.ilx.
icons.json     which category each icon in icons/categories/ belongs to,
               and the assets drawn with an icon of their own.
preview.*      a package's preview image, if it has one.
manifest.json  last: the size and SHA-256 of every other member, the rows
               of each table, and files rows name that were not found.
`

// IconsIndexFile is icons.json: whose each category's icon is, and the
// assets drawn with an icon of their own, by name.
type IconsIndexFile struct {
	Categories []IndexedIcon  `json:"categories"`
	Assets     []IndexedAsset `json:"assets"`
}

// IndexedIcon is a category's own icon for a rendition, by its name.
type IndexedIcon struct {
	Icon       string `json:"icon"`
	CategoryID string `json:"category_id"`
	Category   string `json:"category"`
	// Rendition is where it is drawn: screen or label. An index written
	// before renditions has none, and means screen.
	Rendition string `json:"rendition,omitempty"`
}

// IndexedAsset is an asset drawn with an icon of its own, for a rendition.
type IndexedAsset struct {
	AssetID   string `json:"asset_id"`
	Asset     string `json:"asset"`
	Icon      string `json:"icon"`
	Rendition string `json:"rendition,omitempty"`
}
