// Package hashvalidator validates a hash string against its declared hashcat
// mode. It is used at hashlist upload time to surface malformed hashes before
// jobs reach the agent fleet (GitHub issue #38).
//
// Lookup order for Validate:
//  1. Hand-written structural validator (per hashcat mode) — used for modes
//     where the vendored regex is too loose to be useful (e.g. WPA-PMKID, JWT).
//  2. Vendored regex from data.RawPatterns (sourced from HashPals/Name-That-Hash).
//  3. Example-based fallback derived from hash_types.example loaded at startup.
//  4. If no validator applies, return Result{Valid: true, Unvalidated: true}
//     so the caller can surface a "no known validator" notice rather than
//     blocking the upload.
package hashvalidator

import (
	"regexp"
	"strings"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/hashvalidator/data"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Result is the outcome of validating a single hash.
type Result struct {
	// Valid is true if the hash matches the expected format for its hashcat mode.
	// For modes with no validator coverage, Valid is true and Unvalidated is also true.
	Valid bool

	// Unvalidated is true when no validator existed for the requested hashcat mode.
	// Callers should surface a notice to the user; the hash is allowed through.
	Unvalidated bool

	// Reason is a short, user-actionable explanation when Valid is false.
	// Empty when Valid is true.
	Reason string
}

// BatchResult is the per-hashlist aggregate from ValidateBatch.
type BatchResult struct {
	// Results parallels the input slice; Results[i] is the outcome for hashes[i].
	Results []Result

	// AllUnvalidated is true when every Result has Unvalidated=true — i.e. the
	// hashcat mode has no validator coverage at all. In this case the handler
	// should skip preview gating and attach a notice to the response.
	AllUnvalidated bool

	// ValidCount and InvalidCount exclude lines marked Unvalidated.
	ValidCount   int
	InvalidCount int
}

// StructuralValidator validates a hash with custom Go logic. Use only when a
// regex from the upstream source is too loose or too complex to maintain.
type StructuralValidator func(hash string) Result

// Validator is the public interface; consumers depend on this, not the impl.
type Validator interface {
	Validate(hashcatMode int, hash string) Result
	ValidateBatch(hashcatMode int, hashes []string) BatchResult

	// HasValidator reports whether the given mode has any validation coverage
	// (structural, vendored regex, or example fallback). The handler uses this
	// to short-circuit and write a validation_notice without iterating lines.
	HasValidator(hashcatMode int) bool

	// TypeName returns the human-readable name for a hashcat mode if known,
	// falling back to a generic label. Used in error messages.
	TypeName(hashcatMode int) string
}

// validator is the production implementation.
type validator struct {
	// patternsByMode holds every vendored regex that claims a given hashcat
	// mode. Several upstream entries may share a mode (e.g. mode 1800 has
	// both sha512crypt and the 128-hex bucket Name-That-Hash uses for
	// Keccak-512); the validator accepts a hash when ANY pattern matches.
	patternsByMode   map[int][]*regexp.Regexp
	nameByMode       map[int]string
	structuralByMode map[int]StructuralValidator
	exampleByMode    map[int]*regexp.Regexp
}

// Option configures a Validator at construction time.
type Option func(*validator)

// WithExamples wires example strings (typically from hash_types.example) so
// the validator can derive simple regex fallbacks for modes the upstream
// source doesn't cover. The map is keyed by hashcat mode number.
//
// Examples that don't resolve to a simple character class are silently
// skipped — modes still resolve to Unvalidated at validation time.
func WithExamples(examples map[int]string) Option {
	return func(v *validator) {
		for mode, ex := range examples {
			if _, already := v.patternsByMode[mode]; already {
				continue // prefer vendored over example
			}
			if _, already := v.structuralByMode[mode]; already {
				continue // prefer structural over example
			}
			if re := deriveExamplePattern(ex); re != nil {
				v.exampleByMode[mode] = re
			}
		}
	}
}

// WithStructural registers a hand-written validator for a specific hashcat
// mode. Structural validators take precedence over regex.
func WithStructural(mode int, fn StructuralValidator, name string) Option {
	return func(v *validator) {
		v.structuralByMode[mode] = fn
		if v.nameByMode[mode] == "" {
			v.nameByMode[mode] = name
		}
	}
}

// New constructs a Validator. It compiles every vendored pattern from
// data.RawPatterns once; patterns that fail RE2 syntax are logged and skipped.
func New(opts ...Option) Validator {
	v := &validator{
		patternsByMode:   make(map[int][]*regexp.Regexp, len(data.RawPatterns)),
		nameByMode:       make(map[int]string, len(data.RawPatterns)),
		structuralByMode: make(map[int]StructuralValidator),
		exampleByMode:    make(map[int]*regexp.Regexp),
	}
	v.loadVendoredPatterns()
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func (v *validator) Validate(hashcatMode int, hash string) Result {
	// Trim whitespace defensively — the line reader in the handler already
	// trims, but inline callers might not.
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return Result{Valid: false, Reason: "empty line"}
	}

	if fn, ok := v.structuralByMode[hashcatMode]; ok {
		return fn(hash)
	}
	if patterns, ok := v.patternsByMode[hashcatMode]; ok {
		for _, re := range patterns {
			if re.MatchString(hash) {
				return Result{Valid: true}
			}
		}
		return Result{
			Valid:  false,
			Reason: "does not match expected format for " + v.TypeName(hashcatMode),
		}
	}
	if re, ok := v.exampleByMode[hashcatMode]; ok {
		if re.MatchString(hash) {
			return Result{Valid: true}
		}
		return Result{
			Valid:  false,
			Reason: "does not match the example length/character class for " + v.TypeName(hashcatMode),
		}
	}
	return Result{Valid: true, Unvalidated: true}
}

func (v *validator) ValidateBatch(hashcatMode int, hashes []string) BatchResult {
	out := BatchResult{Results: make([]Result, len(hashes)), AllUnvalidated: true}
	for i, h := range hashes {
		r := v.Validate(hashcatMode, h)
		out.Results[i] = r
		if !r.Unvalidated {
			out.AllUnvalidated = false
		}
		if r.Valid && !r.Unvalidated {
			out.ValidCount++
		} else if !r.Valid {
			out.InvalidCount++
		}
	}
	return out
}

func (v *validator) HasValidator(hashcatMode int) bool {
	if _, ok := v.structuralByMode[hashcatMode]; ok {
		return true
	}
	if _, ok := v.patternsByMode[hashcatMode]; ok {
		return true
	}
	if _, ok := v.exampleByMode[hashcatMode]; ok {
		return true
	}
	return false
}

func (v *validator) TypeName(hashcatMode int) string {
	if n, ok := v.nameByMode[hashcatMode]; ok && n != "" {
		return n
	}
	return "hashcat mode " + itoa(hashcatMode)
}

// loadVendoredPatterns compiles every entry in data.RawPatterns. Each
// hashcat mode may receive multiple patterns; Validate accepts a hash when
// any pattern matches (upstream sometimes assigns one mode number to several
// formats — e.g. mode 1800 covers both sha512crypt and Keccak-512). Display
// names are aggregated so error messages list every variant.
func (v *validator) loadVendoredPatterns() {
	var skipped []string
	nameAccum := make(map[int][]string)
	for _, p := range data.RawPatterns {
		pattern := p.Pattern
		if p.CaseInsensitive && !strings.HasPrefix(pattern, "(?i)") {
			pattern = "(?i)" + pattern
		}
		pattern = clampQuantifiers(pattern)
		re, err := regexp.Compile(pattern)
		if err != nil {
			skipped = append(skipped, p.Name+" (mode "+itoa(p.HashcatMode)+"): "+err.Error())
			continue
		}
		v.patternsByMode[p.HashcatMode] = append(v.patternsByMode[p.HashcatMode], re)
		nameAccum[p.HashcatMode] = append(nameAccum[p.HashcatMode], p.Name)
	}
	for mode, names := range nameAccum {
		v.nameByMode[mode] = strings.Join(dedupStrings(names), " / ")
	}
	if len(skipped) > 0 {
		debug.Warning("hashvalidator: %d vendored patterns failed Go RE2 compile and were skipped: %s",
			len(skipped), strings.Join(skipped, "; "))
	}
	debug.Info("hashvalidator: loaded %d patterns across %d hashcat modes", countPatterns(v.patternsByMode), len(v.patternsByMode))
}

// clampQuantifiers rewrites {n,m} where m exceeds Go's RE2 max (1000) to
// {n,}. Upstream uses bounds like {64,40960} for variable-length Kerberos
// payloads; the upper bound was a sanity check, not a hard requirement, so
// dropping it preserves the intent without breaking compilation.
var quantifierRE = regexp.MustCompile(`\{(\d+),(\d+)\}`)

func clampQuantifiers(pattern string) string {
	return quantifierRE.ReplaceAllStringFunc(pattern, func(m string) string {
		// m is like "{64,40960}". Parse the upper bound.
		matches := quantifierRE.FindStringSubmatch(m)
		if len(matches) != 3 {
			return m
		}
		var upper int
		for i := 0; i < len(matches[2]); i++ {
			upper = upper*10 + int(matches[2][i]-'0')
			if upper > 1000 {
				return "{" + matches[1] + ",}"
			}
		}
		return m
	})
}

func dedupStrings(s []string) []string {
	if len(s) <= 1 {
		return s
	}
	seen := make(map[string]struct{}, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func countPatterns(m map[int][]*regexp.Regexp) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

// itoa avoids pulling strconv into this hot path indirectly via fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Default returns a process-wide singleton validator. It is constructed on
// first use with the vendored patterns and any structural validators
// registered via Register*. The handler typically wires an instance with
// WithExamples at startup; tests construct their own.
//
// Initialization is lazy and deferred so callers that don't use the validator
// (most tests) don't pay the regex-compile cost.
var (
	defaultOnce sync.Once
	defaultVal  Validator
)

// Default returns the lazily-initialized package-level validator. It has the
// vendored regex map loaded but no example fallbacks; for production usage
// prefer constructing via New(WithExamples(...)) so example data from the
// hash_types table is wired in.
func Default() Validator {
	defaultOnce.Do(func() {
		defaultVal = New()
	})
	return defaultVal
}
