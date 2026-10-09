package branding

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxAppNameLen   = 64
	maxPageTitleLen = 120
)

// ErrNotConfigured is returned by mutating calls before Configure ran.
var ErrNotConfigured = errors.New("branding service not configured")

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidationError reports a rejected admin input; handlers map it to HTTP 400.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Msg) }

// IsValidation reports whether err is a *ValidationError.
func IsValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// sanitizeText trims whitespace and removes control characters (including
// CR/LF, which would otherwise reach email subjects and HTML titles).
func sanitizeText(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// stripPoweredBySuffix removes any trailing attribution an admin typed into
// the title field (case-insensitively, along with a preceding separator) so
// the server-side suffix is never doubled.
func stripPoweredBySuffix(s string) string {
	suffix := strings.ToLower(PoweredBy)
	for {
		lower := strings.ToLower(s)
		if !strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(s)
		}
		s = s[:len(s)-len(suffix)] // PoweredBy is ASCII, so byte length is safe
		s = strings.TrimRightFunc(s, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("·-–—|:,", r)
		})
	}
}

// normalizeColor validates a #rrggbb colour and lowercases it. Empty is allowed.
func normalizeColor(field, in string) (string, error) {
	c := strings.TrimSpace(in)
	if c == "" {
		return "", nil
	}
	if !hexColorRe.MatchString(c) {
		return "", &ValidationError{Field: field, Msg: "must be a hex colour in the form #rrggbb"}
	}
	return strings.ToLower(c), nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// validateSettings turns admin input into the store map (nil = NULL).
func validateSettings(in Settings) (map[string]*string, error) {
	name := sanitizeText(in.AppName)
	if utf8.RuneCountInString(name) > maxAppNameLen {
		return nil, &ValidationError{Field: "app_name", Msg: fmt.Sprintf("must be at most %d characters", maxAppNameLen)}
	}
	if strings.EqualFold(name, DefaultAppName) {
		name = "" // the default is expressed as "unset"
	}

	title := stripPoweredBySuffix(sanitizeText(in.PageTitle))
	if utf8.RuneCountInString(title) > maxPageTitleLen {
		return nil, &ValidationError{Field: "page_title", Msg: fmt.Sprintf("must be at most %d characters", maxPageTitleLen)}
	}

	primary, err := normalizeColor("primary_color", in.PrimaryColor)
	if err != nil {
		return nil, err
	}
	if primary == DefaultPrimary {
		primary = ""
	}
	secondary, err := normalizeColor("secondary_color", in.SecondaryColor)
	if err != nil {
		return nil, err
	}

	return map[string]*string{
		KeyAppName:   nullable(name),
		KeyPageTitle: nullable(title),
		KeyPrimary:   nullable(primary),
		KeySecondary: nullable(secondary),
	}, nil
}
