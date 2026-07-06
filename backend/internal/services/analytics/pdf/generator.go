package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/go-pdf/fpdf"
)

// emblemPNG is the KrakenHashes kraken emblem, embedded into the binary so the PDF
// generator has no runtime filesystem dependency on the frontend assets.
//
//go:embed assets/emblem.png
var emblemPNG []byte

// emblemImageName is the fpdf-registered name for the embedded emblem.
const emblemImageName = "kraken-emblem"

// Classification selects how much of the report is rendered.
type Classification string

const (
	// Internal renders the full report including plaintext passwords, usernames
	// and hash values.
	Internal Classification = "internal"
	// External renders an aggregate-only summary. It must be fed data that has
	// already been passed through BuildExternalAnalytics; as defense in depth the
	// renderer also skips sensitive sections when the classification is External.
	External Classification = "external"
)

const (
	pageMarginLeft   = 15.0
	pageMarginTop    = 15.0
	pageMarginRight  = 15.0
	pageHeight       = 297.0
	footerReserve    = 18.0
	contentWidth     = 210.0 - pageMarginLeft - pageMarginRight
	pageBreakTrigger = pageHeight - footerReserve
)

// Generator renders analytics reports to PDF. It is stateless and safe to reuse.
type Generator struct{}

// NewGenerator constructs a PDF generator.
func NewGenerator() *Generator { return &Generator{} }

// renderer carries per-document rendering state.
type renderer struct {
	pdf   *fpdf.Fpdf
	tr    func(string) string // UTF-8 -> cp1252 translator for core fonts
	class Classification
}

// Generate renders the given analytics data as a PDF and returns the bytes.
// data must already be redacted when class == External (see BuildExternalAnalytics).
func (g *Generator) Generate(report *models.AnalyticsReport, client *models.Client, data *models.AnalyticsData, class Classification) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("analytics data is nil")
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pageMarginLeft, pageMarginTop, pageMarginRight)
	pdf.SetAutoPageBreak(true, footerReserve)
	pdf.AliasNbPages("")

	// Register the brand emblem once; the cover and running header place it by name.
	if len(emblemPNG) > 0 {
		pdf.RegisterImageOptionsReader(emblemImageName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(emblemPNG))
	}

	r := &renderer{
		pdf:   pdf,
		tr:    pdf.UnicodeTranslatorFromDescriptor(""),
		class: class,
	}

	r.installHeaderFooter()
	r.renderCover(report, client)

	pdf.AddPage()
	r.renderAll(report, data)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// --- branding / colors ---
//
// KrakenHashes brand palette (source of truth: frontend/src/styles/theme.ts):
// black #000000 / near-black #121212, red #ff0000, white #ffffff. Contrast rule:
// white text only on dark/red fills; near-black text only on white/light fills;
// pure red is reserved for the emblem, rules, bars and the section accent (never
// used as body text on black); white text on red uses a deepened red for AA contrast.

func (r *renderer) brandFill()      { r.pdf.SetFillColor(18, 18, 18) }     // near-black (#121212)
func (r *renderer) accentFill()     { r.pdf.SetFillColor(255, 0, 0) }      // brand red (#ff0000): rules, bars, accents
func (r *renderer) headerRowFill()  { r.pdf.SetFillColor(236, 239, 241) } // light gray (table headers on white pages)
func (r *renderer) zebraFill()      { r.pdf.SetFillColor(247, 249, 250) } // very light gray
func (r *renderer) whiteText()      { r.pdf.SetTextColor(255, 255, 255) }
func (r *renderer) darkText()       { r.pdf.SetTextColor(17, 17, 17) }    // near-black (#111)
func (r *renderer) mutedText()      { r.pdf.SetTextColor(110, 110, 110) }
func (r *renderer) resetDrawColor() { r.pdf.SetDrawColor(200, 200, 200) }

func (r *renderer) classBannerFill() {
	if r.class == Internal {
		r.pdf.SetFillColor(204, 0, 0) // deepened brand red (#cc0000) for white text
	} else {
		r.pdf.SetFillColor(58, 58, 58) // neutral dark gray (#3a3a3a), kept distinct from Internal red
	}
}

// emblem draws the embedded kraken emblem at (x, y) w mm wide, preserving aspect
// ratio (height auto). It is a no-op if the asset failed to embed.
func (r *renderer) emblem(x, y, w float64) {
	if len(emblemPNG) == 0 {
		return
	}
	r.pdf.ImageOptions(emblemImageName, x, y, w, 0, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
}

func (r *renderer) classLabel() string {
	if r.class == Internal {
		return "INTERNAL - CONFIDENTIAL"
	}
	return "EXTERNAL - SUMMARY"
}

// s sanitizes a string for a single-line core-font cell.
func (r *renderer) s(text string) string {
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	return r.tr(text)
}

// --- header / footer ---

func (r *renderer) installHeaderFooter() {
	r.pdf.SetHeaderFunc(func() {
		if r.pdf.PageNo() == 1 {
			return // cover page has its own banner
		}
		// Left: small emblem + wordmark.
		r.emblem(pageMarginLeft, 5.5, 5)
		r.darkText()
		r.pdf.SetFont("Helvetica", "B", 8)
		r.pdf.SetXY(pageMarginLeft+6.5, 6.5)
		r.pdf.CellFormat(70, 5, r.s("KrakenHashes"), "", 0, "L", false, 0, "")
		// Right: classification chip.
		r.classBannerFill()
		r.whiteText()
		r.pdf.SetFont("Helvetica", "B", 7)
		w := r.pdf.GetStringWidth(r.classLabel()) + 6
		r.pdf.SetXY(210.0-pageMarginRight-w, 6)
		r.pdf.CellFormat(w, 5, r.s(r.classLabel()), "", 0, "C", true, 0, "")
		r.darkText()
		r.pdf.SetY(pageMarginTop)
	})

	r.pdf.SetFooterFunc(func() {
		if r.pdf.PageNo() == 1 {
			return
		}
		r.pdf.SetY(-12)
		// Left: emblem + "powered by KrakenHashes" attribution. This footer is intended to
		// stay constant even once custom branding is applied elsewhere, preserving attribution.
		r.emblem(pageMarginLeft, r.pdf.GetY()+0.3, 4)
		r.mutedText()
		r.pdf.SetFont("Helvetica", "I", 7)
		r.pdf.SetX(pageMarginLeft + 5.5)
		r.pdf.CellFormat(110, 5, r.s("powered by KrakenHashes   |   "+r.classLabel()), "", 0, "L", false, 0, "")
		// Right: page number, right-aligned to the margin.
		r.pdf.CellFormat(contentWidth-110-5.5, 5, fmt.Sprintf("Page %d of {nb}", r.pdf.PageNo()), "", 0, "R", false, 0, "")
		r.darkText()
	})
}

// --- cover page ---

func (r *renderer) renderCover(report *models.AnalyticsReport, client *models.Client) {
	p := r.pdf
	p.AddPage()

	// Brand band: near-black with the kraken emblem + wordmark, red accent rule beneath.
	const bandH = 52.0
	const emblemSize = 30.0
	r.brandFill()
	p.Rect(0, 0, 210, bandH, "F")
	r.emblem(pageMarginLeft, (bandH-emblemSize)/2, emblemSize)
	textX := pageMarginLeft + emblemSize + 8
	textW := contentWidth - emblemSize - 8
	r.whiteText()
	p.SetFont("Helvetica", "B", 30)
	p.SetXY(textX, 15)
	p.CellFormat(textW, 12, r.s("KrakenHashes"), "", 1, "L", false, 0, "")
	p.SetFont("Helvetica", "", 12)
	p.SetX(textX)
	p.CellFormat(textW, 8, r.s("Password Analysis Report"), "", 1, "L", false, 0, "")
	// Red accent rule beneath the band.
	r.accentFill()
	p.Rect(0, bandH, 210, 1.5, "F")

	// Classification banner
	p.SetY(70)
	r.classBannerFill()
	r.whiteText()
	p.SetFont("Helvetica", "B", 14)
	p.SetX(pageMarginLeft)
	p.CellFormat(contentWidth, 12, r.s(r.classLabel()), "", 1, "C", true, 0, "")

	// Metadata block
	r.darkText()
	p.SetY(100)
	clientName := "Unknown Client"
	if client != nil && client.Name != "" {
		clientName = client.Name
	}
	r.coverField("Client", clientName)
	r.coverField("Engagement Window", fmt.Sprintf("%s  to  %s",
		report.StartDate.Format("2006-01-02"), report.EndDate.Format("2006-01-02")))
	if report.CompletedAt != nil {
		r.coverField("Report Completed", report.CompletedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	r.coverField("Document Generated", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	r.coverField("Hashlists Analyzed", fmt.Sprintf("%d", report.TotalHashlists))
	r.coverField("Total Hashes", fmt.Sprintf("%d", report.TotalHashes))
	r.coverField("Report ID", report.ID.String())

	// Confidentiality note
	p.SetY(pageHeight - 45)
	r.mutedText()
	p.SetFont("Helvetica", "I", 9)
	note := r.confidentialityNote()
	p.SetX(pageMarginLeft)
	p.MultiCell(contentWidth, 5, r.tr(note), "", "L", false)
	r.darkText()
}

func (r *renderer) confidentialityNote() string {
	if r.class == Internal {
		return "CONFIDENTIAL - INTERNAL USE ONLY. This document contains recovered plaintext " +
			"passwords and account identifiers. Handle, store, and transmit it only through " +
			"approved secure channels. Do not forward externally."
	}
	return "This summary contains aggregate statistics only. It deliberately excludes recovered " +
		"passwords, usernames, and hash values. It is suitable for sharing with the engagement " +
		"stakeholders identified in the statement of work."
}

func (r *renderer) coverField(label, value string) {
	p := r.pdf
	p.SetX(pageMarginLeft)
	r.mutedText()
	p.SetFont("Helvetica", "B", 9)
	p.CellFormat(50, 7, r.s(strings.ToUpper(label)), "", 0, "L", false, 0, "")
	r.darkText()
	p.SetFont("Helvetica", "", 11)
	p.CellFormat(contentWidth-50, 7, r.s(value), "", 1, "L", false, 0, "")
}

// --- shared layout helpers ---

// ensureSpace adds a page if h mm would not fit before the footer.
func (r *renderer) ensureSpace(h float64) {
	if r.pdf.GetY()+h > pageBreakTrigger {
		r.pdf.AddPage()
	}
}

// sectionTitle renders a near-black bar with a red left accent block and a white title.
func (r *renderer) sectionTitle(title string) {
	r.ensureSpace(16)
	r.pdf.Ln(3)
	x, y := pageMarginLeft, r.pdf.GetY()
	// Near-black title bar.
	r.brandFill()
	r.pdf.Rect(x, y, contentWidth, 9, "F")
	// Red left accent block.
	r.accentFill()
	r.pdf.Rect(x, y, 3, 9, "F")
	// White title text, offset past the accent block.
	r.whiteText()
	r.pdf.SetFont("Helvetica", "B", 13)
	r.pdf.SetXY(x+6, y)
	r.pdf.CellFormat(contentWidth-6, 9, r.s(title), "", 1, "L", false, 0, "")
	r.darkText()
	r.pdf.Ln(2)
}

func (r *renderer) subTitle(title string) {
	r.ensureSpace(10)
	r.accentFill()
	r.pdf.SetFont("Helvetica", "B", 10)
	r.darkText()
	r.pdf.SetX(pageMarginLeft)
	// thin accent rule
	x, y := r.pdf.GetX(), r.pdf.GetY()
	r.pdf.Rect(x, y+1, 3, 4, "F")
	r.pdf.SetX(x + 5)
	r.pdf.CellFormat(contentWidth-5, 6, r.s(title), "", 1, "L", false, 0, "")
	r.pdf.Ln(1)
}

func (r *renderer) paragraph(text string) {
	r.ensureSpace(8)
	r.darkText()
	r.pdf.SetFont("Helvetica", "", 10)
	r.pdf.SetX(pageMarginLeft)
	r.pdf.MultiCell(contentWidth, 5, r.tr(text), "", "L", false)
	r.pdf.Ln(1)
}

func (r *renderer) note(text string) {
	r.ensureSpace(6)
	r.mutedText()
	r.pdf.SetFont("Helvetica", "I", 9)
	r.pdf.SetX(pageMarginLeft)
	r.pdf.MultiCell(contentWidth, 5, r.tr(text), "", "L", false)
	r.darkText()
	r.pdf.Ln(1)
}

// keyValues renders a two-column label/value list.
func (r *renderer) keyValues(pairs [][2]string) {
	r.pdf.SetFont("Helvetica", "", 10)
	for _, kv := range pairs {
		r.ensureSpace(7)
		r.pdf.SetX(pageMarginLeft)
		r.mutedText()
		r.pdf.SetFont("Helvetica", "B", 9)
		r.pdf.CellFormat(60, 6, r.s(kv[0]), "", 0, "L", false, 0, "")
		r.darkText()
		r.pdf.SetFont("Helvetica", "", 10)
		r.pdf.CellFormat(contentWidth-60, 6, r.s(kv[1]), "", 1, "L", false, 0, "")
	}
	r.pdf.Ln(1)
}

// dataTable renders a bordered table. widths must sum to <= contentWidth.
func (r *renderer) dataTable(headers []string, widths []float64, rows [][]string) {
	if len(rows) == 0 {
		r.note("No data for this section.")
		return
	}
	r.resetDrawColor()
	drawHeader := func() {
		r.headerRowFill()
		r.darkText()
		r.pdf.SetFont("Helvetica", "B", 9)
		r.pdf.SetX(pageMarginLeft)
		for i, h := range headers {
			r.pdf.CellFormat(widths[i], 7, r.s(h), "1", 0, "L", true, 0, "")
		}
		r.pdf.Ln(-1)
	}

	r.ensureSpace(14)
	drawHeader()
	r.pdf.SetFont("Helvetica", "", 9)
	for ri, row := range rows {
		if r.pdf.GetY()+7 > pageBreakTrigger {
			r.pdf.AddPage()
			drawHeader()
			r.pdf.SetFont("Helvetica", "", 9)
		}
		if ri%2 == 1 {
			r.zebraFill()
		} else {
			r.pdf.SetFillColor(255, 255, 255)
		}
		r.pdf.SetX(pageMarginLeft)
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			align := "L"
			// numeric-looking right column alignment
			r.pdf.CellFormat(widths[i], 6.5, r.s(cell), "1", 0, align, true, 0, "")
		}
		r.pdf.Ln(-1)
	}
	r.pdf.Ln(2)
}

type barItem struct {
	label string
	pct   float64
	count int
}

// barChart renders horizontal bars scaled to 100%.
func (r *renderer) barChart(items []barItem) {
	if len(items) == 0 {
		return
	}
	const labelW = 55.0
	const valueW = 30.0
	barAreaW := contentWidth - labelW - valueW
	r.pdf.SetFont("Helvetica", "", 9)
	for _, it := range items {
		r.ensureSpace(7)
		y := r.pdf.GetY()
		r.darkText()
		r.pdf.SetX(pageMarginLeft)
		r.pdf.CellFormat(labelW, 6, r.s(it.label), "", 0, "L", false, 0, "")
		// bar track
		x := pageMarginLeft + labelW
		r.pdf.SetFillColor(232, 234, 236)
		r.pdf.Rect(x, y+1, barAreaW, 4, "F")
		// bar fill
		pct := it.pct
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		r.accentFill()
		r.pdf.Rect(x, y+1, barAreaW*pct/100.0, 4, "F")
		// value text
		r.pdf.SetXY(pageMarginLeft+labelW+barAreaW, y)
		r.darkText()
		r.pdf.CellFormat(valueW, 6, r.s(fmt.Sprintf("%.1f%% (%d)", it.pct, it.count)), "", 1, "R", false, 0, "")
	}
	r.pdf.Ln(2)
}

// pct formats a percentage value.
func pctStr(v float64) string { return fmt.Sprintf("%.2f%%", v) }

// intStr formats an integer.
func intStr(v int) string { return fmt.Sprintf("%d", v) }
