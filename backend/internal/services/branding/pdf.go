package branding

import (
	"context"
	"strconv"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/analytics/pdf"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// ParseHex converts "#rrggbb" to an RGB triple.
func ParseHex(c string) (pdf.RGB, bool) {
	c = strings.TrimSpace(c)
	if !hexColorRe.MatchString(c) {
		return pdf.RGB{}, false
	}
	v, err := strconv.ParseUint(c[1:], 16, 32)
	if err != nil {
		return pdf.RGB{}, false
	}
	return pdf.RGB{R: int(v >> 16 & 0xff), G: int(v >> 8 & 0xff), B: int(v & 0xff)}, true
}

// PDFBranding builds the options the analytics PDF generator needs. It never
// fails: a missing or unreadable logo simply falls back to the emblem, and a
// nil receiver yields nil (stock output).
func (s *Service) PDFBranding(ctx context.Context) *pdf.Branding {
	if s == nil {
		return nil
	}
	b, err := s.Resolve(ctx)
	if err != nil {
		return nil
	}
	out := &pdf.Branding{OrgName: b.AppName}
	if b.LogoFile != "" {
		data, ct, err := s.LoadAsset(ctx, Logo)
		if err != nil {
			debug.Warning("branding: logo unavailable for PDF, using emblem: %v", err)
		} else {
			out.Logo = data
			out.LogoType = "PNG"
			if ct == "image/jpeg" {
				out.LogoType = "JPEG"
			}
		}
	}
	if rgb, ok := ParseHex(b.PrimaryColor); ok {
		out.Accent = &rgb
	}
	if rgb, ok := ParseHex(b.SecondaryColor); ok {
		out.Secondary = &rgb
	}
	return out
}

// RGBOf is a small constructor used by tests and callers that build colours.
func RGBOf(r, g, b int) pdf.RGB { return pdf.RGB{R: r, G: g, B: b} }
