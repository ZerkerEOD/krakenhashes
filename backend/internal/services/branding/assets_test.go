package branding

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func icoBytes() []byte {
	// Minimal ICONDIR header + one directory entry; enough for sniffing.
	return append([]byte{0, 0, 1, 0, 1, 0}, make([]byte, 16)...)
}

func TestValidateImage(t *testing.T) {
	tests := []struct {
		name    string
		kind    AssetKind
		data    []byte
		wantExt string
		wantErr error
	}{
		{"png logo", Logo, pngBytes(t, 64, 32), "png", nil},
		{"jpeg logo", Logo, jpegBytes(t, 64, 32), "jpg", nil},
		{"png favicon", Favicon, pngBytes(t, 32, 32), "png", nil},
		{"ico favicon", Favicon, icoBytes(), "ico", nil},
		{"ico as logo", Logo, icoBytes(), "", ErrUnsupportedImage},
		{"jpeg as favicon", Favicon, jpegBytes(t, 32, 32), "", ErrUnsupportedImage},
		{"svg", Logo, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "", ErrUnsupportedImage},
		{"gif", Logo, []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), "", ErrUnsupportedImage},
		{"text", Logo, []byte("hello"), "", ErrUnsupportedImage},
		{"empty", Logo, nil, "", ErrUnsupportedImage},
		{"png too wide", Logo, pngBytes(t, maxLogoDim+1, 10), "", ErrImageTooLarge},
		{"favicon too wide", Favicon, pngBytes(t, maxFaviconDim+1, 10), "", ErrImageTooLarge},
		{"too many bytes", Favicon, append(pngBytes(t, 8, 8), make([]byte, MaxFaviconBytes)...), "", ErrImageTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ext, err := validateImage(tc.kind, tc.data)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || ext != tc.wantExt {
				t.Fatalf("got (%q, %v), want (%q, nil)", ext, err, tc.wantExt)
			}
		})
	}
}

func TestAssetPathConfinement(t *testing.T) {
	svc := New(newFakeStore(), t.TempDir())
	good := "logo-0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0.png"
	abs, ct, err := svc.AssetPath(Logo, good)
	if err != nil || ct != "image/png" || !strings.HasPrefix(abs, svc.Dir()) {
		t.Fatalf("valid name rejected: %q %q %v", abs, ct, err)
	}
	bad := []string{
		"", "../x.png", "/etc/passwd", "logo.png", "logo-x.png",
		"favicon-0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0.png", // wrong kind
		"logo-0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0.svg",
		"sub/" + good, "logo-0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0.png\x00",
	}
	for _, name := range bad {
		if _, _, err := svc.AssetPath(Logo, name); err == nil {
			t.Errorf("expected rejection for %q", name)
		}
	}
	if _, _, err := svc.AssetPath(Logo, ""); !errors.Is(err, ErrNoAsset) {
		t.Errorf("empty name should be ErrNoAsset, got %v", err)
	}
}

func TestSaveRemoveAsset(t *testing.T) {
	fs := newFakeStore()
	dir := t.TempDir()
	svc := New(fs, dir)
	ctx := context.Background()

	b, err := svc.SaveAsset(ctx, Logo, pngBytes(t, 40, 20))
	if err != nil {
		t.Fatal(err)
	}
	if b.LogoFile == "" || !b.Branded {
		t.Fatalf("logo not recorded: %+v", b)
	}
	first := filepath.Join(dir, SubDir, b.LogoFile)
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	data, ct, err := svc.LoadAsset(ctx, Logo)
	if err != nil || ct != "image/png" || len(data) == 0 {
		t.Fatalf("LoadAsset: %v %q %d", err, ct, len(data))
	}

	// Replacing removes the previous file.
	b2, err := svc.SaveAsset(ctx, Logo, jpegBytes(t, 40, 20))
	if err != nil {
		t.Fatal(err)
	}
	if b2.LogoFile == b.LogoFile || !strings.HasSuffix(b2.LogoFile, ".jpg") {
		t.Fatalf("expected a new jpg name, got %q", b2.LogoFile)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("previous file should be removed, stat err=%v", err)
	}

	// Invalid uploads leave state untouched.
	if _, err := svc.SaveAsset(ctx, Logo, []byte("nope")); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("expected ErrUnsupportedImage, got %v", err)
	}
	if got := *fs.values[KeyLogoFile]; got != b2.LogoFile {
		t.Fatalf("setting changed by invalid upload: %q", got)
	}

	// Remove clears the setting and the file.
	b3, err := svc.RemoveAsset(ctx, Logo)
	if err != nil {
		t.Fatal(err)
	}
	if b3.LogoFile != "" || b3.Branded || fs.values[KeyLogoFile] != nil {
		t.Fatalf("logo not cleared: %+v", b3)
	}
	if _, err := os.Stat(filepath.Join(dir, SubDir, b2.LogoFile)); !os.IsNotExist(err) {
		t.Fatalf("file should be deleted, stat err=%v", err)
	}
	if _, _, err := svc.LoadAsset(ctx, Logo); !errors.Is(err, ErrNoAsset) {
		t.Fatalf("expected ErrNoAsset, got %v", err)
	}
}

func TestPDFBranding(t *testing.T) {
	fs := newFakeStore()
	svc := New(fs, t.TempDir())
	ctx := context.Background()

	if pb := svc.PDFBranding(ctx); pb == nil || pb.OrgName != DefaultAppName || pb.Logo != nil || pb.Accent == nil || pb.Accent.R != 255 {
		t.Fatalf("stock PDF branding wrong: %+v", pb)
	}
	if _, err := svc.Update(ctx, Settings{AppName: "Acme", PrimaryColor: "#0064c8", SecondaryColor: "#ffb300"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAsset(ctx, Logo, jpegBytes(t, 20, 10)); err != nil {
		t.Fatal(err)
	}
	pb := svc.PDFBranding(ctx)
	if pb.OrgName != "Acme" || pb.LogoType != "JPEG" || len(pb.Logo) == 0 {
		t.Fatalf("org/logo wrong: %+v", pb)
	}
	if pb.Accent == nil || *pb.Accent != (RGBOf(0, 100, 200)) || pb.Secondary == nil || *pb.Secondary != RGBOf(255, 179, 0) {
		t.Fatalf("colours wrong: %+v %+v", pb.Accent, pb.Secondary)
	}
	var nilSvc *Service
	if nilSvc.PDFBranding(ctx) != nil {
		t.Fatal("nil service should yield nil branding")
	}
}
