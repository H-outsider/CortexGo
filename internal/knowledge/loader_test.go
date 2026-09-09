package knowledge

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, content := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileLoaderLoadsMarkdownAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("# Guide\n\nCortexGo knowledge"), 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := (FileLoader{}).Load(path)
	if err != nil || document.Title != "guide.md" || document.Metadata["extension"] != ".md" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
}

func TestFileLoaderStripsHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte("<h1>Guide</h1><p>Hello &amp; welcome</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := (FileLoader{}).Load(path)
	if err != nil || document.Content != "Guide Hello & welcome" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
}

func TestFileLoaderRejectsUnsupportedExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.pdf")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileLoader{}).Load(path); err == nil {
		t.Fatal("expected PDF parse error")
	}
}

func TestFileLoaderLoadsDOCX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guide.docx")
	writeZip(t, path, map[string]string{"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p><w:p><w:r><w:t>World</w:t></w:r></w:p></w:body></w:document>`})
	doc, err := (FileLoader{}).Load(path)
	if err != nil || doc.Content != "Hello\nWorld" {
		t.Fatalf("doc=%#v err=%v", doc, err)
	}
}

func TestFileLoaderLoadsXLSXAndPPTX(t *testing.T) {
	dir := t.TempDir()
	xlsx := filepath.Join(dir, "data.xlsx")
	writeZip(t, xlsx, map[string]string{
		"xl/sharedStrings.xml":     `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Name</t></si><si><t>Ada</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row></sheetData></worksheet>`,
	})
	doc, err := (FileLoader{}).Load(xlsx)
	if err != nil || doc.Content != "Name\tAda" {
		t.Fatalf("xlsx=%#v err=%v", doc, err)
	}
	pptx := filepath.Join(dir, "slides.pptx")
	writeZip(t, pptx, map[string]string{"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:t>Quarterly</a:t><a:t>Results</a:t></p:sld>`})
	doc, err = (FileLoader{}).Load(pptx)
	if err != nil || doc.Content != "Quarterly Results" {
		t.Fatalf("pptx=%#v err=%v", doc, err)
	}
}
