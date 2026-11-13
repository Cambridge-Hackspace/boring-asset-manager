// Package epc turns asset codes into the EPCs that long-range (UHF, EPC
// Gen 2) RFID tags hold, and back (docs/rfid.md).
//
// Three schemes are offered. GIAI-96 and GIAI-202 are GS1's Global
// Individual Asset Identifier, as GS1's Tag Data Standard lays it out:
// they need a GS1 company prefix, and a reader anywhere knows them for
// assets. The text scheme, for an organization without a prefix, writes
// the code's own characters, so that it reads back as itself.
package epc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// The schemes.
const (
	SchemeText    = "text"
	SchemeGIAI96  = "giai-96"
	SchemeGIAI202 = "giai-202"
)

// Schemes lists the schemes, the default first.
func Schemes() []string { return []string{SchemeText, SchemeGIAI96, SchemeGIAI202} }

// MaxTextBytes is the most a text EPC holds: the 31 words a tag's
// protocol control word can declare.
const MaxTextBytes = 62

// MaxFilter is the highest GIAI filter value.
const MaxFilter = 7

// ErrInvalid is wrapped when a scheme is not set up right, or a code does
// not fit it.
var ErrInvalid = errors.New("invalid EPC")

// Scheme is how an instance's codes are written to tags.
type Scheme struct {
	// Kind is one of Schemes.
	Kind string
	// CompanyPrefix is the GS1 company prefix, 6 to 12 digits, for GIAI.
	CompanyPrefix string
	// Filter is the GIAI filter value, 0 to 7, which readers can select
	// tags by; 0 is "all others".
	Filter int
}

// Check reports what is wrong with the scheme.
func (s Scheme) Check() error {
	switch s.Kind {
	case SchemeText:
		return nil
	case SchemeGIAI96, SchemeGIAI202:
	default:
		return fmt.Errorf("%w: the scheme must be one of %s", ErrInvalid, strings.Join(Schemes(), ", "))
	}
	if _, ok := giaiPartition(s.CompanyPrefix); !ok {
		return fmt.Errorf("%w: GIAI needs a GS1 company prefix of 6 to 12 digits", ErrInvalid)
	}
	if s.Filter < 0 || s.Filter > MaxFilter {
		return fmt.Errorf("%w: the filter must be 0 to %d", ErrInvalid, MaxFilter)
	}
	return nil
}

// Encode is the EPC for a code, as uppercase hexadecimal: 24 digits for
// GIAI-96, 52 for GIAI-202 (202 bits and the 6 that pad them to whole
// words), and two for each character of the text scheme, padded to whole
// words.
func (s Scheme) Encode(code string) (string, error) {
	if err := s.Check(); err != nil {
		return "", err
	}
	if code == "" {
		return "", fmt.Errorf("%w: there is no code to write", ErrInvalid)
	}
	switch s.Kind {
	case SchemeGIAI96:
		return s.giai96(code)
	case SchemeGIAI202:
		return s.giai202(code)
	}
	if len(code) > MaxTextBytes {
		return "", fmt.Errorf("%w: %q is longer than a tag holds (%d characters)", ErrInvalid, code, MaxTextBytes)
	}
	for _, c := range []byte(code) {
		if c < 0x20 || c > 0x7E {
			return "", fmt.Errorf("%w: %q has characters other than printable ASCII, which the text scheme writes", ErrInvalid, code)
		}
	}
	b := []byte(code)
	if len(b)%2 == 1 {
		b = append(b, 0)
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

// Decode is the code an EPC, in hexadecimal as a reader reports it,
// holds under the scheme, and whether it holds one: a GIAI of another
// company prefix holds none of this instance's.
func (s Scheme) Decode(epc string) (string, bool) {
	if s.Check() != nil {
		return "", false
	}
	raw, err := hex.DecodeString(strings.TrimSpace(epc))
	if err != nil || len(raw) < 2 || len(raw)%2 != 0 {
		return "", false
	}
	switch s.Kind {
	case SchemeGIAI96:
		return s.fromGIAI96(raw)
	case SchemeGIAI202:
		return s.fromGIAI202(raw)
	}
	text := strings.TrimRight(string(raw), "\x00")
	if text == "" {
		return "", false
	}
	for _, c := range []byte(text) {
		if c < 0x20 || c > 0x7E {
			return "", false
		}
	}
	return text, true
}

// GIAI headers, from the Tag Data Standard.
const (
	headerGIAI96  = 0x34
	headerGIAI202 = 0x38
)

// partition is one row of the GIAI partition table: how a company prefix
// of so many digits splits the bits with the asset reference.
type partition struct {
	value, prefixBits, prefixDigits, ref96Bits, ref202Bits int
}

var giaiPartitions = []partition{
	{0, 40, 12, 42, 148},
	{1, 37, 11, 45, 151},
	{2, 34, 10, 48, 154},
	{3, 30, 9, 52, 158},
	{4, 27, 8, 55, 161},
	{5, 24, 7, 58, 164},
	{6, 20, 6, 62, 168},
}

// maxGIAIDigits is the most characters a GIAI has, company prefix and
// asset reference together.
const maxGIAIDigits = 30

func giaiPartition(prefix string) (partition, bool) {
	if !allDigits(prefix) {
		return partition{}, false
	}
	for _, p := range giaiPartitions {
		if p.prefixDigits == len(prefix) {
			return p, true
		}
	}
	return partition{}, false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// head writes what both GIAIs start with: header, filter, partition and
// company prefix.
func (s Scheme) head(w *bits, header int) partition {
	p, _ := giaiPartition(s.CompanyPrefix)
	prefix, _ := strconv.ParseUint(s.CompanyPrefix, 10, 64)
	w.put(uint64(header), 8)
	w.put(uint64(s.Filter), 3)
	w.put(uint64(p.value), 3)
	w.put(prefix, p.prefixBits)
	return p
}

func (s Scheme) giai96(code string) (string, error) {
	p, _ := giaiPartition(s.CompanyPrefix)
	// The asset reference is a number, written without leading zeros,
	// since the binary form keeps none.
	if !allDigits(code) || (len(code) > 1 && code[0] == '0') {
		return "", fmt.Errorf("%w: GIAI-96 writes only codes that are whole numbers with no leading zeros, not %q; use GIAI-202 for others", ErrInvalid, code)
	}
	ref, ok := new(big.Int).SetString(code, 10)
	if !ok || ref.BitLen() > p.ref96Bits || len(code) > maxGIAIDigits-p.prefixDigits {
		return "", fmt.Errorf("%w: %s is too large for GIAI-96 with a %d-digit company prefix", ErrInvalid, code, p.prefixDigits)
	}
	var w bits
	s.head(&w, headerGIAI96)
	w.putBig(ref, p.ref96Bits)
	return w.hex(96), nil
}

// giaiChar reports whether c is in GS1's AI encodable character set 82,
// which GIAI-202 writes seven bits to a character.
func giaiChar(c byte) bool {
	switch {
	case c >= '0' && c <= '9', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return true
	}
	return strings.IndexByte(`!"%&'()*+,-./:;<=>?_`, c) >= 0
}

func (s Scheme) giai202(code string) (string, error) {
	p, _ := giaiPartition(s.CompanyPrefix)
	if most := maxGIAIDigits - p.prefixDigits; len(code) > most {
		return "", fmt.Errorf("%w: GIAI-202 with a %d-digit company prefix writes codes of up to %d characters, and %q has %d", ErrInvalid, p.prefixDigits, most, code, len(code))
	}
	for _, c := range []byte(code) {
		if !giaiChar(c) {
			return "", fmt.Errorf("%w: GIAI-202 writes only letters, digits and !\"%%&'()*+,-./:;<=>?_, so not %q", ErrInvalid, code)
		}
	}
	var w bits
	s.head(&w, headerGIAI202)
	for _, c := range []byte(code) {
		w.put(uint64(c), 7)
	}
	w.put(0, p.ref202Bits-7*len(code))
	return w.hex(208), nil
}

// readHead reads what both GIAIs start with, and whether it is this
// scheme's: its header, filter and company prefix.
func (s Scheme) readHead(r *reader, header int) (partition, bool) {
	if r.get(8) != uint64(header) || int(r.get(3)) != s.Filter {
		return partition{}, false
	}
	p, _ := giaiPartition(s.CompanyPrefix)
	if int(r.get(3)) != p.value {
		return partition{}, false
	}
	prefix := fmt.Sprintf("%0*d", p.prefixDigits, r.get(p.prefixBits))
	return p, prefix == s.CompanyPrefix
}

func (s Scheme) fromGIAI96(raw []byte) (string, bool) {
	if len(raw) != 12 {
		return "", false
	}
	r := &reader{b: raw}
	p, ok := s.readHead(r, headerGIAI96)
	if !ok {
		return "", false
	}
	return r.getBig(p.ref96Bits).String(), true
}

func (s Scheme) fromGIAI202(raw []byte) (string, bool) {
	if len(raw) != 26 {
		return "", false
	}
	r := &reader{b: raw}
	p, ok := s.readHead(r, headerGIAI202)
	if !ok {
		return "", false
	}
	var code []byte
	for range p.ref202Bits / 7 {
		c := byte(r.get(7))
		if c == 0 {
			break
		}
		if !giaiChar(c) {
			return "", false
		}
		code = append(code, c)
	}
	return string(code), len(code) > 0
}

// bits writes values most significant bit first.
type bits struct{ v []bool }

func (w *bits) put(x uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		w.v = append(w.v, i < 64 && x>>uint(i)&1 == 1)
	}
}

func (w *bits) putBig(x *big.Int, n int) {
	for i := n - 1; i >= 0; i-- {
		w.v = append(w.v, x.Bit(i) == 1)
	}
}

// hex is the bits, padded with zeros to size, in hexadecimal.
func (w *bits) hex(size int) string {
	out := make([]byte, size/8)
	for i, b := range w.v {
		if b {
			out[i/8] |= 0x80 >> (i % 8)
		}
	}
	return strings.ToUpper(hex.EncodeToString(out))
}

// reader reads bits most significant first.
type reader struct {
	b   []byte
	pos int
}

func (r *reader) bit() uint {
	v := r.b[r.pos/8] >> (7 - r.pos%8) & 1
	r.pos++
	return uint(v)
}

func (r *reader) get(n int) uint64 {
	var x uint64
	for range n {
		x = x<<1 | uint64(r.bit())
	}
	return x
}

func (r *reader) getBig(n int) *big.Int {
	x := new(big.Int)
	for range n {
		x.Lsh(x, 1)
		x.SetBit(x, 0, r.bit())
	}
	return x
}
