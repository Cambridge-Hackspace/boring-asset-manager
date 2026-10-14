package generators

import "fmt"

// Info describes a generator type for clients choosing one.
type Info struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// ErrorCorrectionLevels lists the levels templates may ask for, default
	// first; empty when the symbology has none.
	ErrorCorrectionLevels []string `json:"error_correction_levels"`
	// CodeRules says which codes an asset may be assigned, in a sentence.
	CodeRules string `json:"code_rules"`
}

// Describe lists every generator type, in AllowedGeneratorTypes order.
func Describe() []Info {
	names := map[string]string{TypeQR: "QR Code", TypeMicroQR: "Micro QR Code", TypeCode128: "Code 128"}
	rules := map[string]string{
		TypeQR: fmt.Sprintf("Any text up to %d bytes.", QRMaxBytes),
		TypeMicroQR: fmt.Sprintf("Up to %d digits, or up to %d characters from 0-9, A-Z, space and $%%*+-./:.",
			MicroQRMaxNumeric, MicroQRMaxAlphanumeric),
		TypeCode128: fmt.Sprintf("Up to %d printable ASCII characters.", Code128MaxLength),
	}
	var out []Info
	for _, t := range AllowedGeneratorTypes() {
		g := registry[t]
		levels := g.ErrorCorrectionLevels()
		if levels == nil {
			levels = []string{}
		}
		out = append(out, Info{Type: t, Name: names[t], ErrorCorrectionLevels: levels, CodeRules: rules[t]})
	}
	return out
}
