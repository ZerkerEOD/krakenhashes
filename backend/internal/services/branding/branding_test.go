package branding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeStore is an in-memory Store seeded with every key as NULL.
type fakeStore struct {
	values    map[string]*string
	updatedAt time.Time
	writes    int
	failRead  error
}

func newFakeStore() *fakeStore {
	fs := &fakeStore{values: map[string]*string{}, updatedAt: time.Unix(1_700_000_000, 0)}
	for _, k := range AllKeys {
		fs.values[k] = nil
	}
	return fs
}

func (f *fakeStore) GetBrandingSettings(context.Context) (map[string]*string, time.Time, error) {
	if f.failRead != nil {
		return nil, time.Time{}, f.failRead
	}
	out := make(map[string]*string, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out, f.updatedAt, nil
}

func (f *fakeStore) UpdateBrandingSettings(_ context.Context, values map[string]*string) error {
	for k, v := range values {
		if _, ok := f.values[k]; !ok {
			return errors.New("unknown key " + k)
		}
		f.values[k] = v
	}
	f.writes++
	f.updatedAt = f.updatedAt.Add(time.Second)
	return nil
}

func sp(s string) *string { return &s }

func TestResolveDefaults(t *testing.T) {
	svc := New(newFakeStore(), t.TempDir())
	b, err := svc.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.AppName != DefaultAppName || b.PageTitle != DefaultAppName || b.Branded ||
		b.PrimaryColor != DefaultPrimary || b.SecondaryColor != "" || b.LogoFile != "" || b.PoweredBy != PoweredBy {
		t.Fatalf("unexpected defaults: %+v", b)
	}
	if b.Version == 0 {
		t.Fatal("version should derive from updated_at")
	}
}

func TestNilServiceIsSafe(t *testing.T) {
	var svc *Service
	b, err := svc.Resolve(context.Background())
	if err != nil || b.AppName != DefaultAppName {
		t.Fatalf("nil service should yield defaults: %+v %v", b, err)
	}
	if AppName(context.Background()) == "" {
		t.Fatal("AppName must never be empty")
	}
	if _, err := svc.Update(context.Background(), Settings{AppName: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestResolveStoreErrorFallsBackToDefaults(t *testing.T) {
	fs := newFakeStore()
	fs.failRead = errors.New("db down")
	b, err := New(fs, t.TempDir()).Resolve(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if b.AppName != DefaultAppName {
		t.Fatalf("expected defaults, got %+v", b)
	}
}

func TestUpdateAndAttribution(t *testing.T) {
	fs := newFakeStore()
	svc := New(fs, t.TempDir())
	ctx := context.Background()

	b, err := svc.Update(ctx, Settings{AppName: "  Acme Security ", PrimaryColor: "#1E88E5", SecondaryColor: "#FFB300"})
	if err != nil {
		t.Fatal(err)
	}
	if b.AppName != "Acme Security" || !b.Branded {
		t.Fatalf("custom name not applied: %+v", b)
	}
	if b.PageTitle != "Acme Security · powered by KrakenHashes" {
		t.Fatalf("page title missing attribution: %q", b.PageTitle)
	}
	if b.PrimaryColor != "#1e88e5" || b.SecondaryColor != "#ffb300" {
		t.Fatalf("colours not normalised: %+v", b)
	}

	// Custom title with the suffix already typed in: stripped on write, single suffix on read.
	b, err = svc.Update(ctx, Settings{AppName: "Acme", PageTitle: "Acme Portal - Powered By KrakenHashes"})
	if err != nil {
		t.Fatal(err)
	}
	if got := *fs.values[KeyPageTitle]; got != "Acme Portal" {
		t.Fatalf("stored title should be stripped, got %q", got)
	}
	if strings.Count(strings.ToLower(b.PageTitle), "powered by krakenhashes") != 1 {
		t.Fatalf("suffix doubled or missing: %q", b.PageTitle)
	}

	// Typing the default name is the same as clearing it.
	b, err = svc.Update(ctx, Settings{AppName: "krakenhashes"})
	if err != nil {
		t.Fatal(err)
	}
	if fs.values[KeyAppName] != nil || b.Branded || b.PageTitle != DefaultAppName {
		t.Fatalf("default name should clear branding: %+v", b)
	}
}

func TestUpdateCacheInvalidation(t *testing.T) {
	fs := newFakeStore()
	svc := New(fs, t.TempDir())
	ctx := context.Background()
	if b, _ := svc.Resolve(ctx); b.AppName != DefaultAppName {
		t.Fatal("warm-up failed")
	}
	fs.values[KeyAppName] = sp("Behind cache")
	if b, _ := svc.Resolve(ctx); b.AppName != DefaultAppName {
		t.Fatal("expected cached value")
	}
	svc.Invalidate()
	if b, _ := svc.Resolve(ctx); b.AppName != "Behind cache" {
		t.Fatal("expected fresh value after Invalidate")
	}
	if _, err := svc.Update(ctx, Settings{AppName: "Written"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := svc.Resolve(ctx); b.AppName != "Written" {
		t.Fatal("Update must invalidate the cache")
	}
}

func TestValidation(t *testing.T) {
	svc := New(newFakeStore(), t.TempDir())
	ctx := context.Background()
	bad := []Settings{
		{PrimaryColor: "#fff"},
		{PrimaryColor: "blue"},
		{PrimaryColor: "#abcdefg"},
		{SecondaryColor: "ff0000"},
		{AppName: strings.Repeat("a", maxAppNameLen+1)},
		{PageTitle: strings.Repeat("t", maxPageTitleLen+1)},
	}
	for _, in := range bad {
		if _, err := svc.Update(ctx, in); !IsValidation(err) {
			t.Errorf("expected validation error for %+v, got %v", in, err)
		}
	}
	// Control characters are stripped, not rejected.
	b, err := svc.Update(ctx, Settings{AppName: "Acme\r\nEvil\x00Corp"})
	if err != nil {
		t.Fatal(err)
	}
	if b.AppName != "AcmeEvilCorp" {
		t.Fatalf("control chars not stripped: %q", b.AppName)
	}
}

func TestStripPoweredBySuffix(t *testing.T) {
	cases := map[string]string{
		"Acme":                           "Acme",
		"Acme powered by KrakenHashes":   "Acme",
		"Acme · powered by KrakenHashes": "Acme",
		"Acme - Powered By KRAKENHASHES": "Acme",
		"Acme | powered by KrakenHashes powered by KrakenHashes": "Acme",
		"powered by KrakenHashes":                                "",
	}
	for in, want := range cases {
		if got := stripPoweredBySuffix(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestComposePageTitle(t *testing.T) {
	if got := ComposePageTitle("", "KrakenHashes"); got != "KrakenHashes" {
		t.Errorf("stock: %q", got)
	}
	if got := ComposePageTitle("", "Acme"); got != "Acme · powered by KrakenHashes" {
		t.Errorf("from name: %q", got)
	}
	if got := ComposePageTitle("Portal", "Acme"); got != "Portal · powered by KrakenHashes" {
		t.Errorf("custom: %q", got)
	}
}
