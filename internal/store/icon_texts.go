package store

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// An asset's own text on its icon (docs/icons.md). A category's own icon may
// bind any of its text params to the asset's name, its first code, or a
// field; an asset drawn with it is then compiled with that text, one icon
// at a time (isolex's compile_one), from the files the current build was
// made from. Texts differ almost asset by asset, so they get no wrapper
// icons as numbers do: each read of assets compiles those it holds in one
// batch, and keeps what it drew by the build, the icon and the text.

// Where a text param's value comes from.
const (
	TextFromName  = "name"
	TextFromCode  = "code"
	TextFromField = "field"
)

// maxDrawnTexts bounds the icons drawn with texts kept, a few KiB each.
const maxDrawnTexts = 4096

// IconDrawer compiles icons with params set on them, many at once.
type IconDrawer interface {
	CompileMany(ctx context.Context, files []isolex.File, requests []isolex.Request) (*isolex.Many, error)
}

// textBinding is a text param of an icon and where its value comes from.
type textBinding struct {
	param, source, fieldKey string
}

// readTextBindings maps each published icon with text bindings to them, by
// param.
func readTextBindings(q querier) (map[string][]textBinding, error) {
	rows, err := q(`SELECT ci.icon_name, b.param, b.source, COALESCE(f.field_key, '') FROM icon_text_bindings b
		JOIN category_icons ci ON ci.category_id = b.category_id AND ci.rendition = b.rendition
		LEFT JOIN fields f ON f.id = b.field_id
		WHERE ci.source IS NOT NULL ORDER BY ci.icon_name, b.param`)
	if err != nil {
		return nil, fmt.Errorf("reading icon texts: %w", err)
	}
	defer rows.Close()
	out := map[string][]textBinding{}
	for rows.Next() {
		var icon string
		var b textBinding
		if err := rows.Scan(&icon, &b.param, &b.source, &b.fieldKey); err != nil {
			return nil, err
		}
		out[icon] = append(out[icon], b)
	}
	return out, rows.Err()
}

// contextTexts sets the texts each of an asset's icons is drawn with: its
// name, its first code or its value of a field, for each text param of
// its plain icon bound to one, those it has none for left to their
// defaults.
func (s *Store) contextTexts(ctx *assetContext, d *models.AssetWithDetails) error {
	for _, icon := range uniqueIcons(d) {
		if err := s.iconTexts(ctx, d, icon); err != nil {
			return err
		}
	}
	return nil
}

// uniqueIcons are an asset's resolved icons, each once, the screen's
// first.
func uniqueIcons(d *models.AssetWithDetails) []*models.ResolvedIcon {
	var out []*models.ResolvedIcon
	seen := map[*models.ResolvedIcon]bool{}
	for _, r := range models.Renditions {
		if icon := d.ResolvedIcons[r]; icon != nil && !seen[icon] {
			seen[icon] = true
			out = append(out, icon)
		}
	}
	return out
}

func (s *Store) iconTexts(ctx *assetContext, d *models.AssetWithDetails, icon *models.ResolvedIcon) error {
	if icon.Missing {
		return nil
	}
	if ctx.textBindings == nil {
		var err error
		if ctx.textBindings, err = readTextBindings(s.Query); err != nil {
			return err
		}
	}
	bindings := ctx.textBindings[icon.Plain()]
	if len(bindings) == 0 {
		return nil
	}
	texts := map[string]string{}
	for _, b := range bindings {
		var v string
		switch b.source {
		case TextFromName:
			v = d.Asset.Name
		case TextFromCode:
			v = d.FirstCode()
		case TextFromField:
			v = d.FieldValues[b.fieldKey]
		}
		if v = strings.TrimSpace(v); v != "" {
			texts[b.param] = v
		}
	}
	if len(texts) > 0 {
		icon.Texts = texts
	}
	return nil
}

// iconTexts keeps the icons drawn with assets' texts, and the files of the
// build they were drawn from; one for a store and every view of it.
type iconTexts struct {
	mu sync.Mutex
	// drawer compiles them; nil for the process's compiler.
	drawer IconDrawer
	// hash and files are the build's, read when its hash changes.
	hash  string
	files []isolex.File
	// drawn holds each icon drawn, by key (drawnKey), most recently used
	// first; nil for one that does not draw with its texts.
	drawn map[string]*list.Element
	order *list.List
}

type drawnEntry struct {
	key string
	ir  json.RawMessage
}

func newIconTexts() *iconTexts {
	return &iconTexts{drawn: map[string]*list.Element{}, order: list.New()}
}

// DrawIconTextsWith sets the compiler assets' texts are drawn on their
// icons with, in place of the process's.
func (s *Store) DrawIconTextsWith(d IconDrawer) {
	s.texts.mu.Lock()
	defer s.texts.mu.Unlock()
	s.texts.drawer = d
}

// drawnKey keys an icon drawn with texts: the build, the icon and the texts.
func drawnKey(hash, name string, texts map[string]string) string {
	params := make([]string, 0, len(texts))
	for p := range texts {
		params = append(params, p)
	}
	sort.Strings(params)
	var b strings.Builder
	b.WriteString(hash + "\x00" + name)
	for _, p := range params {
		b.WriteString("\x00" + p + "\x00" + texts[p])
	}
	return b.String()
}

// buildFiles returns the current build's hash and the files it was made
// from; none when there is no build, or it was made before builds kept
// them.
func (s *Store) buildFiles() (string, []isolex.File, error) {
	var hash string
	var inputs sql.NullString
	err := s.QueryRow(`SELECT source_hash FROM icon_build WHERE id = ?`, iconBuildID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	t := s.texts
	t.mu.Lock()
	if t.hash == hash && t.files != nil {
		files := t.files
		t.mu.Unlock()
		return hash, files, nil
	}
	t.mu.Unlock()
	if err := s.QueryRow(`SELECT source_hash, inputs FROM icon_build WHERE id = ?`, iconBuildID).Scan(&hash, &inputs); err != nil {
		return "", nil, err
	}
	// a build made before builds kept their files keeps none, until it is
	// made again, perhaps with the same hash: nothing is remembered of it
	if !inputs.Valid {
		return hash, nil, nil
	}
	var files []isolex.File
	if err := json.Unmarshal([]byte(inputs.String), &files); err != nil {
		return "", nil, fmt.Errorf("reading the icon build's files: %w", err)
	}
	if files == nil {
		files = []isolex.File{}
	}
	t.mu.Lock()
	t.hash, t.files = hash, files
	t.mu.Unlock()
	return hash, files, nil
}

// drawTexts draws, on each of the assets' icons with texts, those texts:
// as each is drawn and, when it shows a state, without it too, for a label
// that leaves states out. Icons already drawn are taken as they are kept;
// the rest are compiled together. An icon that does not draw with its
// texts, or that cannot be drawn now, is drawn as the build draws it.
func (s *Store) drawTexts(assets []*models.AssetWithDetails) {
	type want struct {
		icon *models.ResolvedIcon
		name string
		key  string
	}
	var wants []want
	for _, a := range assets {
		for _, icon := range uniqueIcons(a) {
			if len(icon.Texts) == 0 {
				continue
			}
			names := []string{icon.Name}
			if shape := icon.Shape(); shape != icon.Name {
				names = append(names, shape)
			}
			for _, name := range names {
				wants = append(wants, want{icon: icon, name: name})
			}
		}
	}
	if len(wants) == 0 {
		return
	}
	hash, files, err := s.buildFiles()
	if err != nil {
		log.Printf("icons: drawing assets' texts: %v", err)
		return
	}
	if files == nil {
		return
	}
	t := s.texts
	var requests []isolex.Request
	asked := map[string]int{}
	t.mu.Lock()
	drawer := t.drawer
	for i := range wants {
		w := &wants[i]
		w.key = drawnKey(hash, w.name, w.icon.Texts)
		if el, ok := t.drawn[w.key]; ok {
			t.order.MoveToFront(el)
			continue
		}
		if _, ok := asked[w.key]; !ok {
			params := make(map[string]any, len(w.icon.Texts))
			for p, v := range w.icon.Texts {
				params[p] = v
			}
			asked[w.key] = len(requests)
			requests = append(requests, isolex.Request{Icon: w.name, Params: params})
		}
	}
	t.mu.Unlock()

	if len(requests) > 0 {
		if drawer == nil {
			drawer = isolex.Default()
		}
		many, err := drawer.CompileMany(context.Background(), files, requests)
		if err != nil {
			if !errors.Is(err, isolex.ErrUnavailable) {
				log.Printf("icons: drawing assets' texts: %v", err)
			}
			return
		}
		t.mu.Lock()
		for key, i := range asked {
			var drawn json.RawMessage
			if many.OK && i < len(many.Results) {
				drawn = iconOf(many.Results[i], requests[i].Icon)
			}
			t.keep(key, drawn)
		}
		t.mu.Unlock()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, w := range wants {
		el, ok := t.drawn[w.key]
		if !ok || el.Value.(*drawnEntry).ir == nil {
			continue
		}
		if w.icon.Drawn == nil {
			w.icon.Drawn = map[string]json.RawMessage{}
		}
		w.icon.Drawn[w.name] = el.Value.(*drawnEntry).ir
	}
}

// iconOf is the icon named in a result that compiled, as the IR has it;
// nil otherwise.
func iconOf(one isolex.One, name string) json.RawMessage {
	if one.Result == nil || !one.OK {
		return nil
	}
	var doc struct {
		Icons []json.RawMessage `json:"icons"`
	}
	if err := json.Unmarshal(one.IR, &doc); err != nil {
		return nil
	}
	for _, raw := range doc.Icons {
		var head struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &head) == nil && head.Name == name {
			return raw
		}
	}
	return nil
}

// keep keeps an icon drawn, or nil for one that does not draw, dropping
// the least recently used past maxDrawnTexts. t.mu is held.
func (t *iconTexts) keep(key string, ir json.RawMessage) {
	if el, ok := t.drawn[key]; ok {
		el.Value.(*drawnEntry).ir = ir
		t.order.MoveToFront(el)
		return
	}
	t.drawn[key] = t.order.PushFront(&drawnEntry{key: key, ir: ir})
	for t.order.Len() > maxDrawnTexts {
		last := t.order.Back()
		t.order.Remove(last)
		delete(t.drawn, last.Value.(*drawnEntry).key)
	}
}
