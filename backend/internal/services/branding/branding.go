// Package branding resolves the admin-configurable application branding
// (GitHub issue #41): display name, page title, accent colours and the uploaded
// logo / favicon. It is the single source of truth for the "powered by
// KrakenHashes" attribution rule: the suffix is appended to the effective page
// title on every read and stripped from the stored title on every write, so it
// cannot be removed or doubled through the settings API.
//
// The package deliberately depends on nothing but a tiny Store interface so it
// can be reached from leaf packages (email, auth handlers, webhook service)
// through the process-wide Default() accessor without constructor churn.
package branding

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const (
	// DefaultAppName is the product name used whenever no custom name is set.
	DefaultAppName = "KrakenHashes"
	// PoweredBy is the mandatory attribution. It is never stored; it is
	// composed into the page title and rendered by the UI / PDF footer.
	PoweredBy = "powered by KrakenHashes"
	// DefaultPrimary is the stock accent colour (frontend/src/styles/theme.ts).
	DefaultPrimary = "#ff0000"

	titleSeparator = " · " // " · " (cp1252-safe middle dot)

	// Setting keys (seeded by 20261008120000_add_branding_settings).
	KeyAppName     = "branding_app_name"
	KeyPageTitle   = "branding_page_title"
	KeyPrimary     = "branding_primary_color"
	KeySecondary   = "branding_secondary_color"
	KeyLogoFile    = "branding_logo_file"
	KeyFaviconFile = "branding_favicon_file"

	// SubDir is the directory under KH_DATA_DIR holding uploaded assets.
	SubDir = "branding"

	cacheTTL = 30 * time.Second
	// errorCacheTTL bounds how often a failing store is retried by the
	// unauthenticated endpoints during an outage.
	errorCacheTTL = 5 * time.Second
)

// AllKeys lists every branding setting key.
var AllKeys = []string{KeyAppName, KeyPageTitle, KeyPrimary, KeySecondary, KeyLogoFile, KeyFaviconFile}

// Store is the persistence contract, satisfied by *repository.SystemSettingsRepository.
type Store interface {
	GetBrandingSettings(ctx context.Context) (map[string]*string, time.Time, error)
	UpdateBrandingSettings(ctx context.Context, values map[string]*string) error
}

// Settings holds the raw, admin-editable text values. Empty means "unset".
type Settings struct {
	AppName        string `json:"app_name"`
	PageTitle      string `json:"page_title"`
	PrimaryColor   string `json:"primary_color"`
	SecondaryColor string `json:"secondary_color"`
}

// Branding is the resolved, effective branding with defaults applied.
type Branding struct {
	AppName   string
	PageTitle string
	PoweredBy string
	// Branded is true when a custom name or logo is set, i.e. when the UI must
	// render the attribution tagline under the custom identity.
	Branded        bool
	PrimaryColor   string // always set; DefaultPrimary when unconfigured
	SecondaryColor string // "" when unconfigured
	LogoFile       string // basename under the branding dir; "" when none
	FaviconFile    string
	Version        int64 // unix seconds of the latest change; cache-busting token
}

// Defaults returns the stock KrakenHashes branding.
func Defaults() Branding {
	return Branding{
		AppName:      DefaultAppName,
		PageTitle:    DefaultAppName,
		PoweredBy:    PoweredBy,
		PrimaryColor: DefaultPrimary,
	}
}

// Service resolves branding from the store with a short in-process cache.
type Service struct {
	store Store
	dir   string
	ttl   time.Duration

	mu       sync.RWMutex
	cached   *Branding
	cachedAt time.Time
	cacheTTL time.Duration // TTL of the current cache entry (shorter after a store error)

	// assetMu serialises SaveAsset/RemoveAsset so two concurrent uploads cannot
	// both read the same "previous" file and orphan one of them on disk.
	assetMu sync.Mutex
}

// New creates a service storing assets under <dataDir>/branding.
func New(store Store, dataDir string) *Service {
	return &Service{store: store, dir: filepath.Join(dataDir, SubDir), ttl: cacheTTL}
}

// Dir returns the asset directory.
func (s *Service) Dir() string { return s.dir }

var (
	currentMu sync.RWMutex
	current   *Service
)

// Configure installs the process-wide service returned by Default(). Called
// once from routes.SetupRoutes after the settings repository exists.
func Configure(store Store, dataDir string) *Service {
	s := New(store, dataDir)
	currentMu.Lock()
	current = s
	currentMu.Unlock()
	return s
}

// Default returns the process-wide service, or nil before Configure. Every
// method on *Service is nil-safe and yields Defaults() in that case.
func Default() *Service {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}

// AppName is a convenience for leaf packages that only need the display name.
func AppName(ctx context.Context) string {
	b, _ := Default().Resolve(ctx)
	return b.AppName
}

// Resolve returns the effective branding. On a store error it returns
// Defaults() together with the error so callers can degrade gracefully.
func (s *Service) Resolve(ctx context.Context) (Branding, error) {
	if s == nil {
		return Defaults(), nil
	}
	s.mu.RLock()
	if s.cached != nil && time.Since(s.cachedAt) < s.cacheTTL {
		b := *s.cached
		s.mu.RUnlock()
		return b, nil
	}
	s.mu.RUnlock()

	raw, updatedAt, err := s.store.GetBrandingSettings(ctx)
	if err != nil {
		debug.Error("branding: failed to load settings, using defaults: %v", err)
		// Negative cache: keep serving defaults for a few seconds instead of
		// hitting the failing store on every anonymous request.
		d := Defaults()
		s.mu.Lock()
		s.cached = &d
		s.cachedAt = time.Now()
		s.cacheTTL = errorCacheTTL
		s.mu.Unlock()
		return d, err
	}
	b := resolve(raw, updatedAt)

	s.mu.Lock()
	s.cached = &b
	s.cachedAt = time.Now()
	s.cacheTTL = s.ttl
	s.mu.Unlock()
	return b, nil
}

// Invalidate drops the cache so the next Resolve re-reads the store.
func (s *Service) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// RawSettings returns the stored text values (for the admin form) and the
// effective branding derived from them.
func (s *Service) RawSettings(ctx context.Context) (Settings, Branding, error) {
	if s == nil {
		return Settings{}, Defaults(), nil
	}
	raw, updatedAt, err := s.store.GetBrandingSettings(ctx)
	if err != nil {
		return Settings{}, Defaults(), err
	}
	get := func(k string) string {
		if v := raw[k]; v != nil {
			return strings.TrimSpace(*v)
		}
		return ""
	}
	return Settings{
		AppName:        get(KeyAppName),
		PageTitle:      get(KeyPageTitle),
		PrimaryColor:   get(KeyPrimary),
		SecondaryColor: get(KeySecondary),
	}, resolve(raw, updatedAt), nil
}

// Update validates and stores the text settings, then returns the new
// effective branding. Validation failures are *ValidationError.
func (s *Service) Update(ctx context.Context, in Settings) (Branding, error) {
	values, err := validateSettings(in)
	if err != nil {
		return Defaults(), err
	}
	if s == nil {
		return Defaults(), ErrNotConfigured
	}
	if err := s.store.UpdateBrandingSettings(ctx, values); err != nil {
		return Defaults(), err
	}
	s.Invalidate()
	return s.Resolve(ctx)
}

// resolve applies defaults and the attribution rule to raw stored values.
func resolve(raw map[string]*string, updatedAt time.Time) Branding {
	get := func(k string) string {
		if v := raw[k]; v != nil {
			return strings.TrimSpace(*v)
		}
		return ""
	}
	b := Defaults()
	if name := get(KeyAppName); name != "" {
		b.AppName = name
	}
	b.LogoFile = get(KeyLogoFile)
	b.FaviconFile = get(KeyFaviconFile)
	b.Branded = !strings.EqualFold(b.AppName, DefaultAppName) || b.LogoFile != ""
	if c := get(KeyPrimary); c != "" {
		b.PrimaryColor = strings.ToLower(c)
	}
	if c := get(KeySecondary); c != "" {
		b.SecondaryColor = strings.ToLower(c)
	}
	b.PageTitle = ComposePageTitle(get(KeyPageTitle), b.AppName)
	if !updatedAt.IsZero() {
		b.Version = updatedAt.Unix()
	}
	return b
}

// ComposePageTitle builds the effective browser title. A custom title (or,
// when empty, the app name) always carries the attribution suffix; the stock
// name stays bare because "KrakenHashes · powered by KrakenHashes" is silly.
func ComposePageTitle(customTitle, appName string) string {
	base := strings.TrimSpace(customTitle)
	if base == "" {
		base = strings.TrimSpace(appName)
	}
	if base == "" || strings.EqualFold(base, DefaultAppName) {
		return DefaultAppName
	}
	return base + titleSeparator + PoweredBy
}
