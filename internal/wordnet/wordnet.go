// Package wordnet reads Princeton's WordNet database and turns the nouns
// an instance uses into related terms (docs/data-model.md#related-terms):
// each synset a term, its words the term's names, and its hypernyms its
// broader terms.
//
// The file formats are as WordNet documents them:
//   - wndb(5WN), https://wordnet.princeton.edu/documentation/wndb5wn: the
//     index (index.noun) and data (data.noun) files;
//   - morphy(7WN), https://wordnet.princeton.edu/documentation/morphy7wn:
//     how an inflected word, such as "drills", finds its base form, by the
//     exception list (noun.exc) and the noun detachment rules.
//
// Only nouns are read; things in an asset list are named by nouns.
package wordnet

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode"
)

// Source is where the database comes from: the file to download, and the
// SHA-256 it must have, so a mirror, or a copy uploaded by hand, is the
// same file.
type Source struct {
	// Name and Version say what it is, as an import of it is named.
	Name    string
	Version string
	URL     string
	SHA256  string
	// Size is its length in bytes, a bound on what is read.
	Size int64
}

// WordNet31 is WordNet 3.1, the last release of the database, as Princeton
// publishes it. Its SHA-256 was taken from the file as downloaded from URL.
var WordNet31 = Source{
	Name:    "WordNet",
	Version: "3.1",
	URL:     "https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz",
	SHA256:  "3f7d8be8ef6ecc7167d39b10d66954ec734280b5bdcd57f7d9eafe429d11c22a",
	Size:    16358468,
}

// Title is what an import from the source is called, such as "WordNet 3.1".
func (s Source) Title() string { return s.Name + " " + s.Version }

// Notice is WordNet's license, which asks that it appear with every copy
// of the database, including modifications; terms seeded from it carry it
// on the Search terms page and in docs/data-model.md#related-terms. The
// copy kept in an instance's storage is the file as published, which
// holds it at the head of each data file.
const Notice = `WordNet Release 3.1

This software and database is being provided to you, the LICENSEE, by
Princeton University under the following license. By obtaining, using
and/or copying this software and database, you agree that you have
read, understood, and will comply with these terms and conditions.:

Permission to use, copy, modify and distribute this software and
database and its documentation for any purpose and without fee or
royalty is hereby granted, provided that you agree to comply with
the following copyright notice and statements, including the disclaimer,
and that the same appear on ALL copies of the software, database and
documentation, including modifications that you make for internal
use or for distribution.

WordNet 3.1 Copyright 2011 by Princeton University. All rights reserved.

THIS SOFTWARE AND DATABASE IS PROVIDED "AS IS" AND PRINCETON
UNIVERSITY MAKES NO REPRESENTATIONS OR WARRANTIES, EXPRESS OR
IMPLIED. BY WAY OF EXAMPLE, BUT NOT LIMITATION, PRINCETON
UNIVERSITY MAKES NO REPRESENTATIONS OR WARRANTIES OF MERCHANT-
ABILITY OR FITNESS FOR ANY PARTICULAR PURPOSE OR THAT THE USE
OF THE LICENSED SOFTWARE, DATABASE OR DOCUMENTATION WILL NOT
INFRINGE ANY THIRD PARTY PATENTS, COPYRIGHTS, TRADEMARKS OR
OTHER RIGHTS.

The name of Princeton University or Princeton may not be used in
advertising or publicity pertaining to distribution of the software
and/or database. Title to copyright in this software, database and
any associated documentation shall at all times remain with
Princeton University and LICENSEE agrees to preserve same.`

// ErrMismatch is a file whose SHA-256 is not the source's: another file,
// or one cut short.
var ErrMismatch = errors.New("wordnet: the file is not the one expected")

// Check reads the whole of r, at most the source's size, and returns it if
// its SHA-256 is the source's.
func (s Source) Check(r io.Reader) ([]byte, error) {
	limit := s.Size
	if limit <= 0 {
		limit = 64 << 20
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) > limit || !strings.EqualFold(hex.EncodeToString(sum[:]), s.SHA256) {
		return nil, ErrMismatch
	}
	return data, nil
}

// DB is the noun half of the database, read into memory: data.noun is
// looked up by the byte offset each synset is known by.
type DB struct {
	index map[string][]int64 // lemma → its synsets' offsets, most used sense first
	data  []byte
	exc   map[string][]string // inflected form → base forms
}

// Open reads a database from the .tar.gz Princeton publishes: its
// dict/index.noun, dict/data.noun and dict/noun.exc.
func Open(archive []byte) (*DB, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("wordnet: reading the archive: %w", err)
	}
	defer gz.Close()
	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("wordnet: reading the archive: %w", err)
		}
		switch name := path.Base(h.Name); name {
		case "index.noun", "data.noun", "noun.exc":
			if files[name], err = io.ReadAll(tr); err != nil {
				return nil, fmt.Errorf("wordnet: reading %s: %w", name, err)
			}
		}
	}
	for _, name := range []string{"index.noun", "data.noun"} {
		if files[name] == nil {
			return nil, fmt.Errorf("wordnet: the archive has no %s", name)
		}
	}
	return Parse(files["index.noun"], files["data.noun"], files["noun.exc"])
}

// Parse reads the noun files themselves. exc may be nil.
func Parse(index, data, exc []byte) (*DB, error) {
	db := &DB{index: map[string][]int64{}, data: data, exc: map[string][]string{}}
	sc := bufio.NewScanner(bytes.NewReader(index))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		// The license is at the head, each of its lines indented.
		if line == "" || line[0] == ' ' {
			continue
		}
		// lemma pos synset_cnt p_cnt [ptr_symbol...] sense_cnt
		// tagsense_cnt synset_offset [synset_offset...]
		f := strings.Fields(line)
		if len(f) < 6 {
			return nil, fmt.Errorf("wordnet: index.noun: a short line: %q", line)
		}
		n, err := strconv.Atoi(f[2])
		if err != nil || n < 1 || n > len(f)-4 {
			return nil, fmt.Errorf("wordnet: index.noun: a bad synset count: %q", line)
		}
		offsets := make([]int64, 0, n)
		for _, o := range f[len(f)-n:] {
			off, err := strconv.ParseInt(o, 10, 64)
			if err != nil || off < 0 || off >= int64(len(data)) {
				return nil, fmt.Errorf("wordnet: index.noun: a bad offset: %q", line)
			}
			offsets = append(offsets, off)
		}
		db.index[f[0]] = offsets
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("wordnet: index.noun: %w", err)
	}
	if len(db.index) == 0 {
		return nil, errors.New("wordnet: index.noun lists no words")
	}
	// noun.exc: an inflected form, then its base forms.
	sc = bufio.NewScanner(bytes.NewReader(exc))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) >= 2 {
			db.exc[f[0]] = f[1:]
		}
	}
	return db, nil
}

// Synset is one meaning: the words it goes by, most used first, what it
// means, and the offsets of the synsets it is a kind, or an instance, of.
type Synset struct {
	Offset  int64
	Words   []string
	Gloss   string
	Broader []int64
}

// Synset reads the synset at offset in data.noun:
//
//	synset_offset lex_filenum ss_type w_cnt word lex_id [word lex_id...]
//	p_cnt [ptr...] | gloss
//
// where w_cnt is two hexadecimal digits, each pointer is pointer_symbol
// synset_offset pos source/target, and @ and @i point to hypernyms.
func (db *DB) Synset(offset int64) (*Synset, error) {
	if offset < 0 || offset >= int64(len(db.data)) {
		return nil, fmt.Errorf("wordnet: no synset at %d", offset)
	}
	line := db.data[offset:]
	if end := bytes.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	text := strings.TrimRight(string(line), "\r ")
	gloss := ""
	if bar := strings.Index(text, " | "); bar >= 0 {
		text, gloss = text[:bar], strings.TrimSpace(text[bar+3:])
	}
	f := strings.Fields(text)
	bad := func() (*Synset, error) { return nil, fmt.Errorf("wordnet: a bad synset at %d", offset) }
	if len(f) < 4 || f[0] != fmt.Sprintf("%08d", offset) {
		return bad()
	}
	wc, err := strconv.ParseInt(f[3], 16, 32)
	if err != nil || wc < 1 || 4+2*int(wc) >= len(f) {
		return bad()
	}
	s := &Synset{Offset: offset, Gloss: definition(gloss)}
	for i := 0; i < int(wc); i++ {
		s.Words = append(s.Words, strings.ReplaceAll(f[4+2*i], "_", " "))
	}
	at := 4 + 2*int(wc)
	pc, err := strconv.Atoi(f[at])
	if err != nil || at+1+4*pc > len(f) {
		return bad()
	}
	for i := 0; i < pc; i++ {
		p := f[at+1+4*i : at+5+4*i]
		if (p[0] == "@" || p[0] == "@i") && p[2] == "n" {
			o, err := strconv.ParseInt(p[1], 10, 64)
			if err != nil {
				return bad()
			}
			s.Broader = append(s.Broader, o)
		}
	}
	return s, nil
}

// definition is a gloss without its examples, which follow it quoted.
func definition(gloss string) string {
	if i := strings.Index(gloss, `; "`); i >= 0 {
		gloss = gloss[:i]
	}
	return strings.TrimSpace(gloss)
}

// detach is morphy's noun detachment rules: an ending, and what replaces
// it.
var detach = [][2]string{
	{"s", ""}, {"ses", "s"}, {"xes", "x"}, {"zes", "z"}, {"ches", "ch"}, {"shes", "sh"}, {"men", "man"}, {"ies", "y"},
}

// Base is the forms of word (lowercase, words joined by _) that WordNet
// lists as nouns: the word itself, its exceptions, and what the detachment
// rules make of it, as morphy finds them.
func (db *DB) Base(word string) []string {
	var out []string
	add := func(w string) {
		if _, ok := db.index[w]; !ok {
			return
		}
		for _, o := range out {
			if o == w {
				return
			}
		}
		out = append(out, w)
	}
	add(word)
	for _, w := range db.exc[word] {
		add(w)
	}
	// "ful" endings are left as they are: "boxesful" is not a word.
	if !strings.HasSuffix(word, "ful") {
		for _, d := range detach {
			if strings.HasSuffix(word, d[0]) {
				add(strings.TrimSuffix(word, d[0]) + d[1])
			}
		}
	}
	return out
}

// Words is the words of names as WordNet would look them up: lowercase,
// each word alone and each two side by side, joined by _ as WordNet joins
// a collocation such as soldering_iron. Words shorter than three letters,
// and those with digits, are left out, as too likely to mean something
// else.
func Words(names []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(w string) {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	for _, name := range names {
		var words []string
		for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '\''
		}) {
			w = strings.Trim(w, "-'")
			if len([]rune(w)) < 3 || strings.IndexFunc(w, unicode.IsDigit) >= 0 {
				words = append(words, "")
				continue
			}
			words = append(words, w)
		}
		for i, w := range words {
			if w == "" {
				continue
			}
			add(w)
			if i+1 < len(words) && words[i+1] != "" {
				add(w + "_" + words[i+1])
			}
		}
	}
	return out
}

// Item is a term to import (store.TermImportItem's shape): its preferred
// label, other names, what it means, and the labels of its broader terms.
type Item struct {
	Label     string
	AltLabels []string
	Note      string
	Broader   []string
}

// maxAltLabels is how many other names a term may have
// (store.MaxTermAltLabels).
const maxAltLabels = 50

// maxClimb bounds how far up the hypernyms a synset looks for a broader
// one that is seeded; WordNet's nouns are well within it.
const maxClimb = 30

// Seed is the terms for the words of names: for each word WordNet lists
// as a noun, its first senses meanings (most used first; 0 for all), each
// a term named by its words, the first its preferred label. Each term is
// narrower than the nearest of its hypernyms also seeded. Two meanings
// with the same first word are told apart by what they are a kind of, as
// "drill (tool)", and keep the word as another name.
func (db *DB) Seed(names []string, senses int) ([]Item, error) {
	var order []int64
	chosen := map[int64]bool{}
	for _, w := range Words(names) {
		for _, base := range db.Base(w) {
			offs := db.index[base]
			if senses > 0 && len(offs) > senses {
				offs = offs[:senses]
			}
			for _, o := range offs {
				if !chosen[o] {
					chosen[o] = true
					order = append(order, o)
				}
			}
		}
	}
	synsets := map[int64]*Synset{}
	read := func(o int64) (*Synset, error) {
		if s, ok := synsets[o]; ok {
			return s, nil
		}
		s, err := db.Synset(o)
		if err != nil {
			return nil, err
		}
		synsets[o] = s
		return s, nil
	}
	// Labels first, so that broader terms are named as they will be.
	byLabel := map[string][]int64{}
	for _, o := range order {
		s, err := read(o)
		if err != nil {
			return nil, err
		}
		byLabel[fold(s.Words[0])] = append(byLabel[fold(s.Words[0])], o)
	}
	labels := map[int64]string{}
	taken := map[string]bool{}
	for _, o := range order {
		s := synsets[o]
		if len(byLabel[fold(s.Words[0])]) == 1 {
			labels[o] = s.Words[0]
			taken[fold(s.Words[0])] = true
		}
	}
	for _, o := range order {
		if _, ok := labels[o]; ok {
			continue
		}
		s := synsets[o]
		label := ""
		if len(s.Broader) > 0 {
			if b, err := read(s.Broader[0]); err == nil {
				label = s.Words[0] + " (" + b.Words[0] + ")"
			}
		}
		for n := 2; label == "" || taken[fold(label)]; n++ {
			label = fmt.Sprintf("%s (%d)", s.Words[0], n)
		}
		labels[o] = label
		taken[fold(label)] = true
	}
	items := make([]Item, 0, len(order))
	for _, o := range order {
		s := synsets[o]
		it := Item{Label: labels[o], Note: s.Gloss, AltLabels: []string{}, Broader: []string{}}
		for _, w := range s.Words {
			if fold(w) != fold(it.Label) && len(it.AltLabels) < maxAltLabels {
				it.AltLabels = append(it.AltLabels, w)
			}
		}
		broader, err := db.nearest(s, chosen, read)
		if err != nil {
			return nil, err
		}
		for _, b := range broader {
			it.Broader = append(it.Broader, labels[b])
		}
		items = append(items, it)
	}
	return items, nil
}

// nearest is the synsets among chosen nearest above s, climbing its
// hypernyms: on each path up, the first chosen one it meets.
func (db *DB) nearest(s *Synset, chosen map[int64]bool, read func(int64) (*Synset, error)) ([]int64, error) {
	var out []int64
	seen := map[int64]bool{s.Offset: true}
	frontier := s.Broader
	for depth := 0; depth < maxClimb && len(frontier) > 0; depth++ {
		var next []int64
		for _, o := range frontier {
			if seen[o] {
				continue
			}
			seen[o] = true
			if chosen[o] {
				out = append(out, o)
				continue
			}
			up, err := read(o)
			if err != nil {
				return nil, err
			}
			next = append(next, up.Broader...)
		}
		frontier = next
	}
	return out, nil
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
