package knowledge

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

type Loader interface {
	Load(path string) (Document, error)
}

type FileLoader struct{}

func (FileLoader) Load(path string) (Document, error) {
	if strings.TrimSpace(path) == "" {
		return Document{}, fmt.Errorf("knowledge: file path is required")
	}
	ext := filepath.Ext(path)
	var content string
	var err error
	switch strings.ToLower(ext) {
	case ".txt", ".md", ".markdown":
		b, e := os.ReadFile(path)
		err = e
		content = string(b)
	case ".html", ".htm":
		b, e := os.ReadFile(path)
		err = e
		content = stripHTML(string(b))
	case ".pdf":
		content, err = loadPDF(path)
	case ".docx":
		content, err = loadDOCX(path)
	case ".xlsx":
		content, err = loadXLSX(path)
	case ".pptx":
		content, err = loadPPTX(path)
	case ".doc", ".xls", ".ppt":
		content, err = loadLegacyOffice(path)
	default:
		return Document{}, fmt.Errorf("knowledge: unsupported file extension %q", ext)
	}
	if err != nil {
		return Document{}, fmt.Errorf("knowledge: parse %s: %w", ext, err)
	}
	if strings.TrimSpace(content) == "" {
		return Document{}, fmt.Errorf("knowledge: file content is empty")
	}
	return Document{ID: path, Title: filepath.Base(path), Content: content, Metadata: map[string]string{"source": path, "extension": strings.ToLower(ext)}}, nil
}

func loadPDF(path string) (string, error) {
	f, r, e := pdf.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	rr, e := r.GetPlainText()
	if e != nil {
		return "", e
	}
	b, e := io.ReadAll(rr)
	content := strings.TrimSpace(string(b))
	if e != nil || content != "" {
		return content, e
	}
	return loadPDFOCR(path)
}

// loadPDFOCR renders image-only PDF pages and sends each page to Tesseract.
// The binaries are intentionally discovered at runtime so deployments can
// choose their own OCR installation (and tests can provide a fake executable).
func loadPDFOCR(path string) (string, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return "", fmt.Errorf("scanned PDF requires pdftoppm and tesseract")
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		return "", fmt.Errorf("scanned PDF requires tesseract (install Tesseract OCR)")
	}
	dir, err := os.MkdirTemp("", "cortexgo-pdf-ocr-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	prefix := filepath.Join(dir, "page")
	if err := runCommand(context.Background(), "pdftoppm", "-png", path, prefix); err != nil {
		return "", fmt.Errorf("render PDF pages: %w", err)
	}
	pages, err := filepath.Glob(prefix + "-*.png")
	if err != nil || len(pages) == 0 {
		return "", fmt.Errorf("PDF contains no renderable pages")
	}
	sort.Slice(pages, func(i, j int) bool {
		num := func(p string) int {
			b := filepath.Base(p)
			b = strings.TrimSuffix(strings.TrimPrefix(b, "page-"), ".png")
			n, _ := strconv.Atoi(b)
			return n
		}
		return num(pages[i]) < num(pages[j])
	})
	lang := os.Getenv("CORTEXGO_OCR_LANG")
	if lang == "" {
		lang = "eng"
	}
	var out []string
	for _, page := range pages {
		cmd := exec.CommandContext(context.Background(), "tesseract", page, "stdout", "-l", lang)
		b, e := cmd.Output()
		if e != nil {
			return "", fmt.Errorf("OCR page %s: %w", filepath.Base(page), e)
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n"), nil
}

func loadLegacyOffice(path string) (string, error) {
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		return "", fmt.Errorf("legacy Office parsing requires LibreOffice/soffice")
	}
	dir, err := os.MkdirTemp("", "cortexgo-office-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := runCommand(context.Background(), soffice, "--headless", "--convert-to", "txt:Text", "--outdir", dir, path); err != nil {
		return "", fmt.Errorf("convert legacy Office file: %w", err)
	}
	out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".txt")
	b, err := os.ReadFile(out)
	if err != nil {
		return "", fmt.Errorf("read converted text: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if b, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(b))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
func zipXML(path, name string, out any) error {
	z, e := zip.OpenReader(path)
	if e != nil {
		return e
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == name {
			rc, e := f.Open()
			if e != nil {
				return e
			}
			defer rc.Close()
			return xml.NewDecoder(rc).Decode(out)
		}
	}
	return fmt.Errorf("entry %q not found", name)
}
func loadDOCX(path string) (string, error) {
	z, e := zip.OpenReader(path)
	if e != nil {
		return "", e
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, e := f.Open()
		if e != nil {
			return "", e
		}
		defer rc.Close()
		d := xml.NewDecoder(rc)
		var parts []string
		var cur strings.Builder
		for {
			t, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", e
			}
			s, ok := t.(xml.StartElement)
			if !ok {
				continue
			}
			if s.Name.Local == "p" && cur.Len() > 0 {
				parts = append(parts, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			if s.Name.Local == "t" {
				var v string
				if e = d.DecodeElement(&v, &s); e != nil {
					return "", e
				}
				cur.WriteString(v)
			}
		}
		if cur.Len() > 0 {
			parts = append(parts, strings.TrimSpace(cur.String()))
		}
		return strings.Join(parts, "\n"), nil
	}
	return "", fmt.Errorf("entry %q not found", "word/document.xml")
}

type xmlText struct {
	Text string `xml:",chardata"`
}
type sharedStrings struct {
	Items []struct {
		Texts []xmlText `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main t"`
	} `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main si"`
}
type xlsxCell struct {
	Type   string    `xml:"t,attr"`
	Value  string    `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main v"`
	Inline []xmlText `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main is>t"`
}
type xlsxRow struct {
	Cells []xlsxCell `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main c"`
}
type xlsxSheet struct {
	Rows []xlsxRow `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main sheetData>row"`
}

func loadXLSX(path string) (string, error) {
	var ss sharedStrings
	_ = zipXML(path, "xl/sharedStrings.xml", &ss)
	shared := make([]string, len(ss.Items))
	for i, x := range ss.Items {
		for _, t := range x.Texts {
			shared[i] += t.Text
		}
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		return "", e
	}
	defer z.Close()
	var names []string
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		var s xlsxSheet
		if e := zipXML(path, n, &s); e != nil {
			return "", e
		}
		var rows []string
		for _, r := range s.Rows {
			v := make([]string, len(r.Cells))
			for i, c := range r.Cells {
				x := c.Value
				if c.Type == "s" {
					var j int
					if _, e := fmt.Sscan(x, &j); e == nil && j >= 0 && j < len(shared) {
						x = shared[j]
					}
				}
				for _, t := range c.Inline {
					x += t.Text
				}
				v[i] = x
			}
			rows = append(rows, strings.Join(v, "\t"))
		}
		out = append(out, strings.Join(rows, "\n"))
	}
	return strings.Join(out, "\n\n"), nil
}

type pptxSlide struct {
	Texts []xmlText `xml:"http://schemas.openxmlformats.org/drawingml/2006/main t"`
}

func loadPPTX(path string) (string, error) {
	z, e := zip.OpenReader(path)
	if e != nil {
		return "", e
	}
	defer z.Close()
	var n []string
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			n = append(n, f.Name)
		}
	}
	sort.Strings(n)
	var out []string
	for _, x := range n {
		var s pptxSlide
		if e := zipXML(path, x, &s); e != nil {
			return "", e
		}
		v := make([]string, len(s.Texts))
		for i, t := range s.Texts {
			v[i] = t.Text
		}
		out = append(out, strings.Join(v, " "))
	}
	return strings.Join(out, "\n"), nil
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

func stripHTML(content string) string {
	content = htmlTagPattern.ReplaceAllString(content, " ")
	content = html.UnescapeString(content)
	return strings.Join(strings.Fields(content), " ")
}
