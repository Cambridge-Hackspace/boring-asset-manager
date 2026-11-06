package archive

// Retired is a table an older schema had, which an archive written then
// may hold. A restore puts its rows back as they were, into a database at
// the archive's schema, and the migrations that retired it move them; an
// import, which merges into this schema, reads each row as what it became.
type Retired struct {
	Table
	// Until is the schema version that retired it: an archive at that
	// version or later has none of its rows.
	Until int
	// As is the table its rows became, and Convert makes one of them.
	As      string
	Convert func(row map[string]any) map[string]any
}

// AttachmentsVersion is the migration that made assets' photos their image
// attachments (#220).
const AttachmentsVersion = 67

// VendorsVersion is the migration that made vendors, and categories'
// preferred vendors (#217).
const VendorsVersion = 68

// RetiredTables are the tables older schemas had.
var RetiredTables = []Retired{
	{
		Table: Table{Name: "asset_photos", Kind: KindAssets, Since: 18, Key: []string{"id"},
			Columns: []Column{{"id", Text}, {"asset_id", Text}, {"thumb_id", Text}, {"position", Int}, {"filename", Text}, {"content_type", Text}, {"width_px", Int}, {"height_px", Int}, {"byte_size", Int}, {"created_at", Time}},
		},
		Until: AttachmentsVersion,
		As:    "asset_attachments",
		// A photo is an image attachment named by its file, as migration
		// 067 makes it.
		Convert: func(r map[string]any) map[string]any {
			return map[string]any{
				"id": r["id"], "asset_id": r["asset_id"], "kind": "image", "position": r["position"],
				"name": r["filename"], "description": "", "filename": r["filename"], "content_type": r["content_type"],
				"byte_size": r["byte_size"], "thumb_id": r["thumb_id"], "width_px": r["width_px"], "height_px": r["height_px"],
				"added_by": nil, "created_at": r["created_at"],
			}
		},
	},
}

// tablesAt are the tables an archive at schema version v may hold: every
// table in Tables, and those retired since v. A table whose Since is later
// than v may be missing from it.
func tablesAt(v int) []Table {
	tables := append([]Table(nil), Tables...)
	for _, r := range RetiredTables {
		if v < r.Until {
			tables = append(tables, r.Table)
		}
	}
	return tables
}

// retiredTable finds a retired table by name, or nil.
func retiredTable(name string) *Retired {
	for i := range RetiredTables {
		if RetiredTables[i].Name == name {
			return &RetiredTables[i]
		}
	}
	return nil
}
