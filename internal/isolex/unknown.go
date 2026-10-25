package isolex

import (
	_ "embed"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
)

// UnknownName is the name of the icon drawn in place of one the lexicon
// does not have.
const UnknownName = "bam-unknown"

// unknownSource is the icon's source; unknownIR is that compiled, kept so
// that a binary without the compiler can still draw it.
//
//go:embed unknown.ilx
var unknownSource string

//go:embed unknown.ir.json
var unknownIR []byte

var unknownIcon = func() ir.Icon {
	doc, err := ir.Parse(unknownIR)
	if err != nil || len(doc.Icons) != 1 || doc.Icons[0].Name != UnknownName {
		panic("isolex: unknown.ir.json is not the bam-unknown icon")
	}
	return doc.Icons[0]
}()

// Unknown returns the icon drawn in place of one the lexicon does not
// have. It is bam's own, the only icon bam ships.
func Unknown() ir.Icon { return unknownIcon }
