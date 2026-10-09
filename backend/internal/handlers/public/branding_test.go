package public

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/branding"
)

type memStore struct {
	values    map[string]*string
	updatedAt time.Time
}

func newMemStore() *memStore {
	m := &memStore{values: map[string]*string{}, updatedAt: time.Unix(1_700_000_000, 0)}
	for _, k := range branding.AllKeys {
		m.values[k] = nil
	}
	return m
}

func (m *memStore) GetBrandingSettings(context.Context) (map[string]*string, time.Time, error) {
	out := map[string]*string{}
	for k, v := range m.values {
		out[k] = v
	}
	return out, m.updatedAt, nil
}

func (m *memStore) UpdateBrandingSettings(_ context.Context, values map[string]*string) error {
	for k, v := range values {
		m.values[k] = v
	}
	m.updatedAt = m.updatedAt.Add(time.Second)
	return nil
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGetBrandingDefaults(t *testing.T) {
	h := NewBrandingHandler(branding.New(newMemStore(), t.TempDir()))
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/branding", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got PublicBranding
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.AppName != "KrakenHashes" || got.PageTitle != "KrakenHashes" || got.Branded || got.LogoURL != nil || got.FaviconURL != nil || got.PoweredBy != branding.PoweredBy {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestGetBrandingNilService(t *testing.T) {
	h := NewBrandingHandler(nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/branding", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.Logo(rec, httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("logo status %d, want 404", rec.Code)
	}
}

func TestServeLogo(t *testing.T) {
	svc := branding.New(newMemStore(), t.TempDir())
	h := NewBrandingHandler(svc)

	rec := httptest.NewRecorder()
	h.Logo(rec, httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unset logo: status %d, want 404", rec.Code)
	}

	b, err := svc.SaveAsset(context.Background(), branding.Logo, smallPNG(t))
	if err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/branding", nil))
	var pub PublicBranding
	_ = json.NewDecoder(rec.Body).Decode(&pub)
	if pub.LogoURL == nil || *pub.LogoURL != "/api/branding/logo?v="+itoa(b.Version) || !pub.Branded {
		t.Fatalf("logo url wrong: %+v", pub)
	}

	rec = httptest.NewRecorder()
	h.Logo(rec, httptest.NewRequest(http.MethodGet, "/api/branding/logo?v=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type %q", ct)
	}
	if rec.Header().Get("ETag") != `"`+b.LogoFile+`"` || rec.Header().Get("Cache-Control") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", rec.Header())
	}
	if !bytes.Equal(rec.Body.Bytes(), smallPNG(t)) {
		t.Fatal("body mismatch")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil)
	req.Header.Set("If-None-Match", `"`+b.LogoFile+`"`)
	rec = httptest.NewRecorder()
	h.Logo(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional status %d, want 304", rec.Code)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
