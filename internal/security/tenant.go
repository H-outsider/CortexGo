package security

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/cortexgo/cortexgo/internal/knowledge"
	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
)

type tenantKey struct{}

func WithTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}
func TenantFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tenantKey{}).(string)
	return v, ok
}
func requireTenant(ctx context.Context) (string, error) {
	t, ok := TenantFromContext(ctx)
	if !ok || strings.TrimSpace(t) == "" {
		return "", fmt.Errorf("security: tenant is required")
	}
	return t, nil
}

type TenantMemory struct{ Store memory.Store }

func (s TenantMemory) Append(ctx context.Context, session string, messages ...provider.Message) error {
	t, e := requireTenant(ctx)
	if e != nil {
		return e
	}
	return s.Store.Append(ctx, t+":"+session, messages...)
}
func (s TenantMemory) List(ctx context.Context, session string) ([]provider.Message, error) {
	t, e := requireTenant(ctx)
	if e != nil {
		return nil, e
	}
	return s.Store.List(ctx, t+":"+session)
}
func (s TenantMemory) Replace(ctx context.Context, session string, messages ...provider.Message) error {
	t, e := requireTenant(ctx)
	if e != nil {
		return e
	}
	if r, ok := s.Store.(memory.ReplaceStore); ok {
		return r.Replace(ctx, t+":"+session, messages...)
	}
	return fmt.Errorf("security: wrapped memory store does not support replace")
}

type TenantIndex struct{ Index knowledge.Index }

func (i TenantIndex) Add(ctx context.Context, doc knowledge.Document) error {
	t, e := requireTenant(ctx)
	if e != nil {
		return e
	}
	if doc.Metadata == nil {
		doc.Metadata = map[string]string{}
	}
	doc.Metadata["tenant_id"] = t
	return i.Index.Add(ctx, doc)
}
func (i TenantIndex) Search(ctx context.Context, q string, l int) ([]knowledge.Result, error) {
	t, e := requireTenant(ctx)
	if e != nil {
		return nil, e
	}
	if f, ok := i.Index.(knowledge.FilteredIndex); ok {
		return f.SearchWithFilter(ctx, q, l, knowledge.MetadataFilter{"tenant_id": t})
	}
	return nil, fmt.Errorf("security: tenant isolation requires filtered index")
}

func ValidateRemoteURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" {
		return fmt.Errorf("security: only https URLs are allowed")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("security: URL host is required")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("security: private network URL is blocked")
		}
	}
	return nil
}
func ValidateUpload(path string, max int64, allowed map[string]bool) error {
	if path == "" || filepath.Base(path) != path {
		return fmt.Errorf("security: invalid upload path")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !allowed[ext] {
		return fmt.Errorf("security: file type %q is not allowed", ext)
	}
	return nil
}
