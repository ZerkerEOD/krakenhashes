package branding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// AssetKind selects the logo or the favicon.
type AssetKind string

const (
	Logo    AssetKind = "logo"
	Favicon AssetKind = "favicon"

	// MaxLogoBytes / MaxFaviconBytes bound upload size (and PDF memory).
	MaxLogoBytes    = 2 << 20
	MaxFaviconBytes = 512 << 10
	maxLogoDim      = 2048
	maxFaviconDim   = 512
)

var (
	// ErrUnsupportedImage is returned for anything but an RGB PNG/JPEG logo or
	// a PNG/ICO favicon. SVG is rejected on purpose: served inline it is an
	// XSS vector.
	ErrUnsupportedImage = errors.New("unsupported image: use a PNG or JPEG logo, or a PNG/ICO favicon")
	// ErrImageTooLarge is returned when the upload exceeds the byte or pixel cap.
	ErrImageTooLarge = errors.New("image exceeds the size limit")
	// ErrNoAsset is returned when no file of that kind is configured.
	ErrNoAsset = errors.New("no branding asset configured")
	// ErrInvalidAsset is returned for a stored name that fails confinement.
	ErrInvalidAsset = errors.New("invalid branding asset name")
)

// assetNameRe is the only shape a stored basename may take: our own prefix,
// a UUID we generated, and a known extension. Anything else is refused before
// it is ever joined to the data directory.
var assetNameRe = regexp.MustCompile(`^(logo|favicon)-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|jpg|ico)$`)

// Valid reports whether k is a known kind.
func (k AssetKind) Valid() bool { return k == Logo || k == Favicon }

// Key is the settings key holding this kind's file name.
func (k AssetKind) Key() string {
	if k == Favicon {
		return KeyFaviconFile
	}
	return KeyLogoFile
}

// MaxBytes is the upload cap for this kind.
func (k AssetKind) MaxBytes() int {
	if k == Favicon {
		return MaxFaviconBytes
	}
	return MaxLogoBytes
}

func (k AssetKind) maxDim() int {
	if k == Favicon {
		return maxFaviconDim
	}
	return maxLogoDim
}

// isICO checks the ICONDIR header (reserved=0, type=1).
func isICO(b []byte) bool {
	return len(b) >= 6 && b[0] == 0 && b[1] == 0 && b[2] == 1 && b[3] == 0
}

// validateICO walks the ICONDIR directory: at least one entry, every entry's
// image must lie inside the buffer, and no entry may exceed maxDim. A byte of 0
// in the entry header means 256 px (the ICO convention). Payloads that merely
// start with the magic bytes are rejected here instead of being stored and
// served publicly.
func validateICO(b []byte, maxDim int) error {
	if !isICO(b) {
		return ErrUnsupportedImage
	}
	count := int(b[4]) | int(b[5])<<8
	if count == 0 || count > 64 {
		return ErrUnsupportedImage
	}
	const dirStart, entrySize = 6, 16
	if len(b) < dirStart+count*entrySize {
		return ErrUnsupportedImage
	}
	for i := 0; i < count; i++ {
		e := b[dirStart+i*entrySize : dirStart+(i+1)*entrySize]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if w > maxDim || h > maxDim {
			return fmt.Errorf("%w: at most %dx%d pixels", ErrImageTooLarge, maxDim, maxDim)
		}
		size := int(e[8]) | int(e[9])<<8 | int(e[10])<<16 | int(e[11])<<24
		offset := int(e[12]) | int(e[13])<<8 | int(e[14])<<16 | int(e[15])<<24
		if size <= 0 || offset < dirStart+count*entrySize || offset > len(b) || size > len(b)-offset {
			return ErrUnsupportedImage
		}
	}
	return nil
}

// validateImage sniffs and decodes the header of an upload and returns the
// extension to store it under.
func validateImage(kind AssetKind, data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrUnsupportedImage
	}
	if len(data) > kind.MaxBytes() {
		return "", ErrImageTooLarge
	}
	ct := http.DetectContentType(data)
	switch {
	case ct == "image/png":
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", ErrUnsupportedImage
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > kind.maxDim() || cfg.Height > kind.maxDim() {
			return "", fmt.Errorf("%w: at most %dx%d pixels", ErrImageTooLarge, kind.maxDim(), kind.maxDim())
		}
		return "png", nil
	case ct == "image/jpeg" && kind == Logo:
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", ErrUnsupportedImage
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > kind.maxDim() || cfg.Height > kind.maxDim() {
			return "", fmt.Errorf("%w: at most %dx%d pixels", ErrImageTooLarge, kind.maxDim(), kind.maxDim())
		}
		return "jpg", nil
	case kind == Favicon && isICO(data):
		if err := validateICO(data, kind.maxDim()); err != nil {
			return "", err
		}
		return "ico", nil
	}
	return "", ErrUnsupportedImage
}

// ContentTypeFor maps a stored basename to its MIME type.
func ContentTypeFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	}
	return "application/octet-stream"
}

// AssetPath resolves a stored basename to an absolute path confined to the
// branding directory and returns its content type.
func (s *Service) AssetPath(kind AssetKind, name string) (string, string, error) {
	if s == nil || name == "" {
		return "", "", ErrNoAsset
	}
	base := filepath.Base(name)
	if base != name || !assetNameRe.MatchString(base) || !strings.HasPrefix(base, string(kind)+"-") {
		return "", "", ErrInvalidAsset
	}
	dir := filepath.Clean(s.dir)
	abs := filepath.Join(dir, base)
	if !strings.HasPrefix(abs, dir+string(os.PathSeparator)) {
		return "", "", ErrInvalidAsset
	}
	return abs, ContentTypeFor(base), nil
}

// LoadAsset returns the bytes and content type of the configured asset.
func (s *Service) LoadAsset(ctx context.Context, kind AssetKind) ([]byte, string, error) {
	b, err := s.Resolve(ctx)
	if err != nil {
		return nil, "", err
	}
	name := b.LogoFile
	if kind == Favicon {
		name = b.FaviconFile
	}
	abs, ct, err := s.AssetPath(kind, name)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	return data, ct, nil
}

// SaveAsset validates and stores an upload under a generated name, records it,
// and removes the previously stored file. Returns the new effective branding.
func (s *Service) SaveAsset(ctx context.Context, kind AssetKind, data []byte) (Branding, error) {
	if !kind.Valid() {
		return Defaults(), ErrInvalidAsset
	}
	ext, err := validateImage(kind, data)
	if err != nil {
		return Defaults(), err
	}
	if s == nil {
		return Defaults(), ErrNotConfigured
	}
	s.assetMu.Lock()
	defer s.assetMu.Unlock()

	raw, _, err := s.store.GetBrandingSettings(ctx)
	if err != nil {
		return Defaults(), err
	}
	previous := ""
	if v := raw[kind.Key()]; v != nil {
		previous = strings.TrimSpace(*v)
	}

	name := fmt.Sprintf("%s-%s.%s", kind, uuid.New().String(), ext)
	if err := os.MkdirAll(s.dir, 0750); err != nil {
		return Defaults(), fmt.Errorf("create branding dir: %w", err)
	}
	abs, _, err := s.AssetPath(kind, name)
	if err != nil {
		return Defaults(), err
	}
	if err := os.WriteFile(abs, data, 0640); err != nil {
		return Defaults(), fmt.Errorf("write branding asset: %w", err)
	}
	if err := s.store.UpdateBrandingSettings(ctx, map[string]*string{kind.Key(): &name}); err != nil {
		_ = os.Remove(abs)
		return Defaults(), err
	}
	s.Invalidate()
	s.removeStored(kind, previous)
	return s.Resolve(ctx)
}

// RemoveAsset clears the setting and deletes the stored file.
func (s *Service) RemoveAsset(ctx context.Context, kind AssetKind) (Branding, error) {
	if !kind.Valid() {
		return Defaults(), ErrInvalidAsset
	}
	if s == nil {
		return Defaults(), ErrNotConfigured
	}
	s.assetMu.Lock()
	defer s.assetMu.Unlock()

	raw, _, err := s.store.GetBrandingSettings(ctx)
	if err != nil {
		return Defaults(), err
	}
	previous := ""
	if v := raw[kind.Key()]; v != nil {
		previous = strings.TrimSpace(*v)
	}
	if err := s.store.UpdateBrandingSettings(ctx, map[string]*string{kind.Key(): nil}); err != nil {
		return Defaults(), err
	}
	s.Invalidate()
	s.removeStored(kind, previous)
	return s.Resolve(ctx)
}

// removeStored best-effort deletes a previously stored file.
func (s *Service) removeStored(kind AssetKind, name string) {
	if name == "" {
		return
	}
	abs, _, err := s.AssetPath(kind, name)
	if err != nil {
		debug.Warning("branding: not removing stored %s %q: %v", kind, name, err)
		return
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		debug.Warning("branding: failed to remove old %s %s: %v", kind, abs, err)
	}
}
