package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/branding"
	"github.com/gorilla/mux"
)

type memBrandingStore struct {
	values    map[string]*string
	updatedAt time.Time
}

func newMemBrandingStore() *memBrandingStore {
	m := &memBrandingStore{values: map[string]*string{}, updatedAt: time.Unix(1_700_000_000, 0)}
	for _, k := range branding.AllKeys {
		m.values[k] = nil
	}
	return m
}

func (m *memBrandingStore) GetBrandingSettings(context.Context) (map[string]*string, time.Time, error) {
	out := map[string]*string{}
	for k, v := range m.values {
		out[k] = v
	}
	return out, m.updatedAt, nil
}

func (m *memBrandingStore) UpdateBrandingSettings(_ context.Context, values map[string]*string) error {
	for k, v := range values {
		m.values[k] = v
	}
	m.updatedAt = m.updatedAt.Add(time.Second)
	return nil
}

func brandingRouter(t *testing.T) (*mux.Router, *memBrandingStore) {
	t.Helper()
	store := newMemBrandingStore()
	r := mux.NewRouter()
	NewBrandingSettingsHandler(branding.New(store, t.TempDir())).RegisterRoutes(r)
	return r, store
}

func multipartBody(t *testing.T, field string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBrandingUpdateValidation(t *testing.T) {
	r, store := brandingRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/branding", strings.NewReader(`{"primary_color":"blue"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid hex: status %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/branding", strings.NewReader(`{"app_name":"Acme","page_title":"Acme Portal powered by KrakenHashes","primary_color":"#1E88E5"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid update: status %d body %s", rec.Code, rec.Body.String())
	}
	var resp AdminBranding
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Settings.AppName != "Acme" || resp.Settings.PageTitle != "Acme Portal" || resp.Settings.PrimaryColor != "#1e88e5" {
		t.Fatalf("stored settings wrong: %+v", resp.Settings)
	}
	if resp.Effective.PageTitle != "Acme Portal · powered by KrakenHashes" || !resp.Effective.Branded {
		t.Fatalf("effective wrong: %+v", resp.Effective)
	}
	if store.values[branding.KeyAppName] == nil {
		t.Fatal("store not written")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/branding", strings.NewReader(`not json`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status %d", rec.Code)
	}
}

func TestBrandingUploads(t *testing.T) {
	r, _ := brandingRouter(t)

	do := func(path string, data []byte) *httptest.ResponseRecorder {
		body, ct := multipartBody(t, "file", data)
		req := httptest.NewRequest(http.MethodPost, path, body)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("/settings/branding/logo", []byte("<svg/>")); rec.Code != http.StatusBadRequest {
		t.Fatalf("svg: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := do("/settings/branding/favicon", make([]byte, branding.MaxFaviconBytes+1)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := do("/settings/branding/logo", encodePNG(t, 3000, 10)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too wide: status %d body %s", rec.Code, rec.Body.String())
	}

	rec := do("/settings/branding/logo", encodePNG(t, 64, 32))
	if rec.Code != http.StatusOK {
		t.Fatalf("png logo: status %d body %s", rec.Code, rec.Body.String())
	}
	var resp AdminBranding
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.HasLogo || resp.Effective.LogoURL == nil {
		t.Fatalf("logo not recorded: %+v", resp)
	}

	// Missing field
	req := httptest.NewRequest(http.MethodPost, "/settings/branding/logo", strings.NewReader("x"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=zzz")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed multipart: status %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/branding/logo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d", rec.Code)
	}
	resp = AdminBranding{}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.HasLogo || resp.Effective.LogoURL != nil {
		t.Fatalf("logo not cleared: %+v", resp)
	}
}
