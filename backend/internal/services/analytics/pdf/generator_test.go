package pdf

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

func testReport() (*models.AnalyticsReport, *models.Client) {
	now := time.Now()
	return &models.AnalyticsReport{
			ID:             uuid.New(),
			StartDate:      now.AddDate(0, -1, 0),
			EndDate:        now,
			TotalHashlists: 2,
			TotalHashes:    1000,
			CompletedAt:    &now,
		}, &models.Client{
			Name: "Test Client",
		}
}

func testLogoPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 40))
	for x := 0; x < 120; x++ {
		for y := 0; y < 40; y++ {
			img.Set(x, y, color.RGBA{R: 0, G: 100, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGenerateWithoutBranding(t *testing.T) {
	report, client := testReport()
	for _, class := range []Classification{Internal, External} {
		out, err := NewGenerator().Generate(report, client, sensitiveFixture(), class, nil)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF")) {
			t.Fatalf("%s: not a PDF", class)
		}
	}
}

func TestGenerateWithBranding(t *testing.T) {
	report, client := testReport()
	brand := &Branding{
		OrgName:   "Acme Ünicode Security",
		Logo:      testLogoPNG(t),
		LogoType:  "PNG",
		Accent:    &RGB{R: 0, G: 100, B: 200},
		Secondary: &RGB{R: 255, G: 179, B: 0},
	}
	out, err := NewGenerator().Generate(report, client, sensitiveFixture(), Internal, brand)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
	// Both the org logo and the emblem (footer attribution) must be embedded:
	// one more image object than the stock document.
	base, err := NewGenerator().Generate(report, client, sensitiveFixture(), Internal, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Count(base, []byte("/Subtype /Image")) + 1
	if got := bytes.Count(out, []byte("/Subtype /Image")); got != want {
		t.Fatalf("expected %d embedded images (org logo + emblem), found %d", want, got)
	}
}

func TestGenerateBrokenLogoFallsBack(t *testing.T) {
	report, client := testReport()
	brand := &Branding{OrgName: "Acme", Logo: []byte("\x89PNG\r\n\x1a\nnot really a png"), LogoType: "PNG"}
	out, err := NewGenerator().Generate(report, client, sensitiveFixture(), External, brand)
	if err != nil {
		t.Fatalf("broken logo must not fail the export: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
}

// fpdf cannot embed interlaced PNGs (it sets an internal error on
// registration). A real-world upload of one must still export, falling back to
// the emblem for the brand mark.
func TestGenerateInterlacedLogoFallsBack(t *testing.T) {
	logo, err := os.ReadFile("testdata/interlaced-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	report, client := testReport()
	brand := &Branding{OrgName: "Acme", Logo: logo, LogoType: "PNG"}
	out, err := NewGenerator().Generate(report, client, sensitiveFixture(), Internal, brand)
	if err != nil {
		t.Fatalf("interlaced logo must not fail the export: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
	// The org logo must have been dropped: the image count equals the stock
	// document's (emblem + its alpha mask), not one more.
	base, err := NewGenerator().Generate(report, client, sensitiveFixture(), Internal, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Count(base, []byte("/Subtype /Image"))
	if got := bytes.Count(out, []byte("/Subtype /Image")); got != want {
		t.Fatalf("expected %d embedded images (emblem fallback), found %d", want, got)
	}
}
