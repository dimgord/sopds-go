package scanner

import (
	"archive/zip"
	"bytes"
	"testing"
)

// makeEpub builds a minimal in-memory epub (a zip with container.xml + the OPF).
func makeEpub(t *testing.T, opfPath, opf string) []byte {
	t.Helper()
	container := `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="` + opfPath + `" media-type="application/oebps-package+xml"/></rootfiles>
</container>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"META-INF/container.xml": container,
		opfPath:                  opf,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseEPUB_Calibre(t *testing.T) {
	// Prefixed dc:/opf: markup like a real Calibre export — exercises the
	// namespace-agnostic (local-name) matching and the opf:file-as author split.
	opf := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>Kredu min, sinjorino!</dc:title>
    <dc:creator opf:role="aut" opf:file-as="Cesare, Rossetti">Rossetti Cesare</dc:creator>
    <dc:language>eo</dc:language>
    <dc:description>Priskribo.</dc:description>
    <meta name="calibre:series" content="Testa Serio"/>
    <meta name="calibre:series_index" content="3.0"/>
  </metadata>
  <manifest/>
</package>`
	meta, err := parseEPUB(makeEpub(t, "content.opf", opf))
	if err != nil {
		t.Fatalf("parseEPUB: %v", err)
	}
	if meta.Title != "Kredu min, sinjorino!" {
		t.Errorf("Title = %q", meta.Title)
	}
	if meta.Lang != "eo" {
		t.Errorf("Lang = %q", meta.Lang)
	}
	if meta.Annotation != "Priskribo." {
		t.Errorf("Annotation = %q", meta.Annotation)
	}
	if len(meta.Authors) != 1 || meta.Authors[0].LastName != "Cesare" || meta.Authors[0].FirstName != "Rossetti" {
		t.Errorf("Authors = %+v (want file-as split Last=Cesare First=Rossetti)", meta.Authors)
	}
	if len(meta.Series) != 1 || meta.Series[0].Name != "Testa Serio" || meta.Series[0].Number != 3 {
		t.Errorf("Series = %+v (want Testa Serio #3)", meta.Series)
	}
}

func TestParseEPUB_NoFileAs(t *testing.T) {
	// No opf:file-as -> fall back to last-word-as-surname (parseAuthorFullName).
	opf := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>The Silver Skates</dc:title>
    <dc:creator>Mary Mapes Dodge</dc:creator>
  </metadata>
</package>`
	meta, err := parseEPUB(makeEpub(t, "OEBPS/content.opf", opf))
	if err != nil {
		t.Fatalf("parseEPUB: %v", err)
	}
	if len(meta.Authors) != 1 || meta.Authors[0].FirstName != "Mary Mapes" || meta.Authors[0].LastName != "Dodge" {
		t.Errorf("Authors = %+v (want First='Mary Mapes' Last='Dodge')", meta.Authors)
	}
}

func TestParseEPUB_Epub3Collection(t *testing.T) {
	opf := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Book Two</dc:title>
    <meta property="belongs-to-collection" id="c01">The Saga</meta>
    <meta refines="#c01" property="group-position">2</meta>
  </metadata>
</package>`
	meta, err := parseEPUB(makeEpub(t, "content.opf", opf))
	if err != nil {
		t.Fatalf("parseEPUB: %v", err)
	}
	if len(meta.Series) != 1 || meta.Series[0].Name != "The Saga" || meta.Series[0].Number != 2 {
		t.Errorf("Series = %+v (want The Saga #2)", meta.Series)
	}
}

func TestParseEPUB_Errors(t *testing.T) {
	if _, err := parseEPUB([]byte("not a zip")); err == nil {
		t.Error("expected error for non-zip input")
	}
	// valid title-less epub: no error, empty title (caller falls back to filename)
	opf := `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf"><metadata/></package>`
	meta, err := parseEPUB(makeEpub(t, "content.opf", opf))
	if err != nil {
		t.Fatalf("parseEPUB: %v", err)
	}
	if meta.Title != "" {
		t.Errorf("Title = %q, want empty", meta.Title)
	}
}
