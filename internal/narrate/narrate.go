// Package narrate extracts an FB2 book into per-unit narration text for the auto-F5 TTS pipeline —
// the native (Go) replacement for the old xmllint/awk extraction and the fb2_extract.py script.
//
// Two book shapes are handled:
//   - Sectioned (the common case): the main <body> nests <section>/<section> (part → chapter). A
//     unit is a whole part or a nested chapter, selectable via PARTS "P" / "P:S" / ranges + COMBINE.
//   - Flat (rare, e.g. "11/22/63"): one <section> whose parts/chapters are bold heading paragraphs
//     (<p><strong>Часть N…/Раздел N</strong></p>) in the paragraph stream. The same P:S addressing
//     applies, S being the position of a "Раздел" within its "Часть".
//
// FB2 is parsed with a token stream (encoding/xml struct unmarshaling loses the interleaved order of
// <p> and <section>, which narration and flat-heading detection both need).
package narrate

import (
	"encoding/xml"
	"io"
	"regexp"
	"strings"
)

// node is an ordered FB2 element we care about for narration: a section (recursive), a paragraph, or
// a title. Order among a parent's children is preserved (unlike struct unmarshaling).
type node struct {
	kind     nodeKind
	id       string  // section id (for footnote targets) / body name
	children []*node // sections + paragraphs, in document order
	text     string  // flattened text (paragraphs, titles)
	bold     bool     // paragraph is entirely a single <strong>/<b> (flat-heading candidate)
}

type nodeKind int

const (
	kSection nodeKind = iota
	kParagraph
	kTitle
	kBody
)

// Private-use markers: a footnote reference in paragraph text (refOpen<id>refClose), and a hard chunk
// boundary (SEP) that brackets an inlined note so it becomes its own chunk, never packed mid-narrative.
const (
	refOpen  = ''
	refClose = ''
	SEP      = ""
)

var wsRe = regexp.MustCompile(`\s+`)

func collapse(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

// Parse reads an FB2 document and returns its <body> nodes (main first, then named bodies like notes).
func Parse(r io.Reader) ([]*node, error) {
	dec := xml.NewDecoder(r)
	var bodies []*node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "body" {
			continue
		}
		b := &node{kind: kBody, id: attr(se, "name")}
		if err := parseChildren(dec, b); err != nil {
			return nil, err
		}
		bodies = append(bodies, b)
	}
	return bodies, nil
}

// parseChildren consumes elements until the current parent's end tag, appending sections/paragraphs/
// titles (recursively) to parent.children in document order.
func parseChildren(dec *xml.Decoder, parent *node) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "section":
				s := &node{kind: kSection, id: attr(t, "id")}
				if err := parseChildren(dec, s); err != nil {
					return err
				}
				parent.children = append(parent.children, s)
			case "title":
				ti := &node{kind: kTitle}
				txt, err := elementText(dec)
				if err != nil {
					return err
				}
				ti.text = collapse(txt)
				parent.children = append(parent.children, ti)
			case "p":
				p := &node{kind: kParagraph}
				txt, bold, err := paragraphText(dec)
				if err != nil {
					return err
				}
				p.text = collapse(txt)
				p.bold = bold
				parent.children = append(parent.children, p)
			default:
				// epigraph, subtitle, empty-line, poem, cite, annotation, image… — pull their flat
				// text as a paragraph so narration keeps it; skip zero-text ones.
				txt, err := elementText(dec)
				if err != nil {
					return err
				}
				if c := collapse(txt); c != "" {
					parent.children = append(parent.children, &node{kind: kParagraph, text: c})
				}
			}
		case xml.EndElement:
			return nil
		}
	}
}

// elementText returns the flattened character data of the current element (consumes to its end tag).
func elementText(dec *xml.Decoder) (string, error) {
	var b strings.Builder
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			depth--
			if depth == 0 {
				return b.String(), nil
			}
		}
	}
}

// paragraphText returns a <p>'s flattened text and whether the paragraph is entirely a single bold
// run (a lone <strong>/<b> with no other significant text) — the flat-book heading signal.
func paragraphText(dec *xml.Decoder) (string, bool, error) {
	var all strings.Builder
	var outsideBold strings.Builder // chardata NOT inside a strong/b
	var boldRuns int
	depth := 1
	boldDepth := 0
	aDepth := 0 // >0 while inside an <a href="#…"> footnote ref (its visible number is skipped)
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "strong", "b":
				if boldDepth == 0 {
					boldRuns++
				}
				boldDepth++
			case "a":
				if href := attr(t, "href"); aDepth == 0 && strings.HasPrefix(href, "#") {
					// emit a footnote-ref marker in place of the link's visible number
					marker := string(refOpen) + href[1:] + string(refClose)
					all.WriteString(marker)
					if boldDepth == 0 {
						outsideBold.WriteString(marker)
					}
					aDepth = depth
				}
			}
		case xml.CharData:
			if aDepth > 0 {
				break // skip the ref's visible text (the footnote number)
			}
			all.Write(t)
			if boldDepth == 0 {
				outsideBold.Write(t)
			}
		case xml.EndElement:
			if aDepth > 0 && depth == aDepth {
				aDepth = 0
			}
			depth--
			if (t.Name.Local == "strong" || t.Name.Local == "b") && boldDepth > 0 {
				boldDepth--
			}
			if depth == 0 {
				// a marker in outsideBold shouldn't count as real text for bold detection
				plain := strings.ReplaceAll(outsideBold.String(), string(refOpen), "")
				plain = strings.ReplaceAll(plain, string(refClose), "")
				bold := boldRuns >= 1 && strings.TrimSpace(plain) == ""
				return all.String(), bold, nil
			}
		}
	}
}

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// MainBody returns the first unnamed <body> (the narration body); nil if none.
func MainBody(bodies []*node) *node {
	for _, b := range bodies {
		if b.id == "" {
			return b
		}
	}
	if len(bodies) > 0 {
		return bodies[0]
	}
	return nil
}

func (n *node) sections() []*node {
	var out []*node
	for _, c := range n.children {
		if c.kind == kSection {
			out = append(out, c)
		}
	}
	return out
}

func (n *node) titleText() string {
	for _, c := range n.children {
		if c.kind == kTitle {
			return c.text
		}
	}
	return ""
}
