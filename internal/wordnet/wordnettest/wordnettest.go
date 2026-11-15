// Package wordnettest builds a tiny WordNet database in the published
// format (wndb(5WN)), so tests seed terms without downloading the real
// one.
package wordnettest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/wordnet"
)

// synset is one line of data.noun before its offsets are known: its key,
// its words, the keys of its hypernyms, and its gloss.
type synset struct {
	key     string
	words   []string
	broader []string
	gloss   string
}

// The fixture's nouns: drill as a tool and as practice, a flashlight that
// is also a torch, a torch that burns, a soldering iron (a collocation)
// beside iron, and a die (plural dice, an exception).
var synsets = []synset{
	{"entity", []string{"entity"}, nil, "that which is perceived or known or inferred to have its own distinct existence"},
	{"tool", []string{"tool"}, []string{"entity"}, `an implement used in the practice of a vocation; "a craftsman's tools"`},
	{"activity", []string{"activity"}, []string{"entity"}, "any specific behavior"},
	{"lamp", []string{"lamp"}, []string{"entity"}, "an artificial source of visible illumination"},
	{"drill", []string{"drill"}, []string{"tool"}, "a tool with a sharp point and cutting edges for making holes in hard materials"},
	{"drill2", []string{"drill"}, []string{"activity"}, "systematic training by multiple repetitions"},
	{"flashlight", []string{"flashlight", "torch"}, []string{"lamp"}, "a small portable battery-powered electric lamp"},
	{"torch2", []string{"torch", "flambeau"}, []string{"lamp"}, "a light usually carried in the hand; consists of some flammable substance"},
	{"cordless", []string{"cordless_drill", "battery_drill"}, []string{"drill"}, "a drill that runs on a battery"},
	{"soldering", []string{"soldering_iron"}, []string{"tool"}, "a hand tool with a rod that is heated electrically and used to melt solder"},
	{"iron", []string{"iron", "Fe"}, []string{"entity"}, "a heavy ductile magnetic metallic element"},
	{"die", []string{"die"}, []string{"tool"}, "a device used for shaping metal"},
}

// The fixture's index: each lemma's synsets, most used first.
var lemmas = map[string][]string{
	"entity": {"entity"}, "tool": {"tool"}, "activity": {"activity"}, "lamp": {"lamp"},
	"drill": {"drill", "drill2"}, "flashlight": {"flashlight"}, "torch": {"flashlight", "torch2"}, "flambeau": {"torch2"},
	"cordless_drill": {"cordless"}, "battery_drill": {"cordless"},
	"soldering_iron": {"soldering"}, "iron": {"iron"}, "fe": {"iron"}, "die": {"die"},
}

const header = "  1 This software and database is being provided to you, the LICENSEE, by  \n" +
	"  2 Princeton University under the following license.  \n"

// Files is the fixture's index.noun, data.noun and noun.exc.
func Files() (index, data, exc []byte) {
	// Offsets are eight digits, so every line's length is known before
	// its offsets are: lay out with zeros, then write them in.
	format := func(s synset, offsets map[string]int64) string {
		var b strings.Builder
		fmt.Fprintf(&b, "%08d 06 n %02x", offsets[s.key], len(s.words))
		for _, w := range s.words {
			fmt.Fprintf(&b, " %s 0", w)
		}
		fmt.Fprintf(&b, " %03d", len(s.broader))
		for _, k := range s.broader {
			fmt.Fprintf(&b, " @ %08d n 0000", offsets[k])
		}
		fmt.Fprintf(&b, " | %s  \n", s.gloss)
		return b.String()
	}
	offsets := map[string]int64{}
	at := int64(len(header))
	for _, s := range synsets {
		offsets[s.key] = at
		at += int64(len(format(s, map[string]int64{})))
	}
	var d strings.Builder
	d.WriteString(header)
	for _, s := range synsets {
		d.WriteString(format(s, offsets))
	}
	var i strings.Builder
	i.WriteString(header)
	for _, lemma := range []string{"activity", "battery_drill", "cordless_drill", "die", "drill", "entity", "fe", "flambeau", "flashlight", "iron", "lamp", "soldering_iron", "tool", "torch"} {
		keys := lemmas[lemma]
		fmt.Fprintf(&i, "%s n %d 1 @ %d 0", lemma, len(keys), len(keys))
		for _, k := range keys {
			fmt.Fprintf(&i, " %08d", offsets[k])
		}
		i.WriteString("  \n")
	}
	return []byte(i.String()), []byte(d.String()), []byte("dice die\n")
}

// Archive is the fixture packed as Princeton packs the database, a
// .tar.gz of dict/, and a Source that names it by its SHA-256.
func Archive() ([]byte, wordnet.Source) {
	idx, data, exc := Files()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"dict/", nil}, {"dict/index.noun", idx}, {"dict/data.noun", data}, {"dict/noun.exc", exc}} {
		h := &tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(f.name, "/") {
			h.Typeflag, h.Mode = tar.TypeDir, 0o755
		}
		_ = tw.WriteHeader(h)
		_, _ = tw.Write(f.body)
	}
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), wordnet.Source{Name: "WordNet", Version: "test", URL: "https://wordnet.test/dict.tar.gz", SHA256: hex.EncodeToString(sum[:]), Size: int64(buf.Len())}
}
