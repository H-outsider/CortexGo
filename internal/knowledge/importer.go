package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

type OCRInfo struct {
	Available bool
	Languages []string
	Missing   []string
}

// DetectOCR reports runtime OCR binaries and requested language packages.
func DetectOCR(languages ...string) OCRInfo {
	info := OCRInfo{Languages: append([]string(nil), languages...)}
	_, pdftoppm := exec.LookPath("pdftoppm")
	_, tesseract := exec.LookPath("tesseract")
	info.Available = pdftoppm == nil && tesseract == nil
	for _, lang := range languages {
		if lang == "" {
			continue
		}
		if _, err := os.Stat("/usr/share/tesseract-ocr/5/tessdata/" + lang + ".traineddata"); err != nil {
			info.Missing = append(info.Missing, lang)
		}
	}
	return info
}

type DocumentVersion struct {
	DocumentID, Version, Hash string
	CreatedAt                 time.Time
	Document                  Document
}
type VersionStore struct {
	mu       sync.RWMutex
	versions map[string][]DocumentVersion
}

func NewVersionStore() *VersionStore {
	return &VersionStore{versions: make(map[string][]DocumentVersion)}
}
func (s *VersionStore) Add(document Document) (DocumentVersion, bool) {
	h := sha256.Sum256([]byte(document.Content))
	hash := hex.EncodeToString(h[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.versions[document.ID]
	if len(list) > 0 && list[len(list)-1].Hash == hash {
		return list[len(list)-1], false
	}
	v := DocumentVersion{DocumentID: document.ID, Version: fmt.Sprintf("v%d", len(list)+1), Hash: hash, CreatedAt: time.Now().UTC(), Document: document}
	s.versions[document.ID] = append(list, v)
	return v, true
}
func (s *VersionStore) List(documentID string) []DocumentVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]DocumentVersion(nil), s.versions[documentID]...)
}

type ImportProgress struct {
	ID, DocumentID, Status string
	Chunks                 int
	Err                    error
}
type AsyncImporter struct {
	loader   Loader
	index    Index
	versions *VersionStore
	mu       sync.RWMutex
	jobs     map[string]ImportProgress
}

func NewAsyncImporter(loader Loader, index Index, versions *VersionStore) *AsyncImporter {
	if versions == nil {
		versions = NewVersionStore()
	}
	return &AsyncImporter{loader: loader, index: index, versions: versions, jobs: make(map[string]ImportProgress)}
}
func (i *AsyncImporter) Submit(ctx context.Context, path string) (string, error) {
	if i.loader == nil || i.index == nil {
		return "", fmt.Errorf("knowledge: importer dependencies are required")
	}
	if err := contextError(ctx); err != nil {
		return "", err
	}
	id := fmt.Sprintf("job-%d", time.Now().UnixNano())
	i.mu.Lock()
	i.jobs[id] = ImportProgress{ID: id, DocumentID: path, Status: "queued"}
	i.mu.Unlock()
	go i.run(id, path)
	return id, nil
}
func (i *AsyncImporter) run(id, path string) {
	i.update(id, func(p *ImportProgress) { p.Status = "running" })
	doc, err := i.loader.Load(path)
	if err == nil {
		_, _ = i.versions.Add(doc)
		err = i.index.Add(context.Background(), doc)
	}
	i.update(id, func(p *ImportProgress) {
		if err != nil {
			p.Status = "failed"
			p.Err = err
		} else {
			p.Status = "completed"
		}
	})
}
func (i *AsyncImporter) update(id string, fn func(*ImportProgress)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.jobs[id]
	fn(&p)
	i.jobs[id] = p
}
func (i *AsyncImporter) Progress(id string) (ImportProgress, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	p, ok := i.jobs[id]
	return p, ok
}
