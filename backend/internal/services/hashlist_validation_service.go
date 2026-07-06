package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/hashvalidator"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// HashlistValidationService runs upload-time hash-format validation against a
// declared hashcat mode (GitHub issue #38). It owns the lazily-built
// hashvalidator.Validator instance (seeded from hash_types.example) and the
// persistence of invalid lines to the invalid_hashes table.
type HashlistValidationService struct {
	hashlistRepo     *repository.HashListRepository
	invalidHashRepo  *repository.InvalidHashRepository
	hashTypeRepo     *repository.HashTypeRepository
	validatorOnce    sync.Once
	validator        hashvalidator.Validator
}

// NewHashlistValidationService constructs the service. The validator itself
// is built on first use so test setups that don't exercise validation pay no
// startup cost.
func NewHashlistValidationService(
	hashlistRepo *repository.HashListRepository,
	invalidHashRepo *repository.InvalidHashRepository,
	hashTypeRepo *repository.HashTypeRepository,
) *HashlistValidationService {
	return &HashlistValidationService{
		hashlistRepo:    hashlistRepo,
		invalidHashRepo: invalidHashRepo,
		hashTypeRepo:    hashTypeRepo,
	}
}

// ValidationOutcome is the per-upload result returned to the handler.
type ValidationOutcome struct {
	// HasValidator is true when the declared hashcat mode has any validator
	// coverage (structural, vendored regex, or example fallback). When false,
	// the service has persisted a validation_notice on the hashlist and the
	// caller should resume the normal upload path.
	HasValidator bool

	// TotalInputLines counts every non-empty, non-comment line in the file.
	TotalInputLines int

	// ValidCount and InvalidCount sum to TotalInputLines when HasValidator is
	// true. When HasValidator is false both are zero.
	ValidCount   int
	InvalidCount int

	// InvalidSample is the first N invalid entries (capped by the caller's
	// preview budget — currently 20). The full list is in the invalid_hashes
	// table and is reachable via InvalidHashRepository.
	InvalidSample []models.InvalidHash

	// Truncated indicates the persisted invalid list was capped at
	// repository.MaxInvalidHashRowsPerHashlist (10000) and additional invalid
	// lines were observed but discarded.
	Truncated bool

	// Notice is the user-facing notice text when HasValidator is false. The
	// service has already written this to hashlists.validation_notice.
	Notice string
}

// ValidateUpload reads the source file, validates each non-empty line against
// the declared hashcat mode, and persists invalid lines + per-row metadata.
// It updates hashlists.invalid_count, total_input_lines, and (for unvalidated
// modes) validation_notice on success. The caller is responsible for the
// subsequent status transition (proceed / awaiting_validation_decision).
func (s *HashlistValidationService) ValidateUpload(ctx context.Context, hashlistID int64, filePath string, hashTypeID int) (*ValidationOutcome, error) {
	v, err := s.getValidator(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain validator: %w", err)
	}

	if !v.HasValidator(hashTypeID) {
		// Count input lines so the user still sees an accurate total.
		total, countErr := countInputLines(filePath)
		if countErr != nil {
			return nil, countErr
		}
		notice := buildNoValidatorNotice(v.TypeName(hashTypeID))
		if err := s.hashlistRepo.UpdateValidationFields(ctx, hashlistID, 0, total, &notice); err != nil {
			return nil, fmt.Errorf("failed to record validation notice: %w", err)
		}
		return &ValidationOutcome{
			HasValidator:    false,
			TotalInputLines: total,
			Notice:          notice,
		}, nil
	}

	out, err := s.scanAndValidate(ctx, hashlistID, filePath, hashTypeID, v)
	if err != nil {
		return nil, err
	}
	if err := s.hashlistRepo.UpdateValidationFields(ctx, hashlistID, out.InvalidCount, out.TotalInputLines, nil); err != nil {
		return nil, fmt.Errorf("failed to record validation counts: %w", err)
	}
	return out, nil
}

const previewSampleSize = 20

func (s *HashlistValidationService) scanAndValidate(ctx context.Context, hashlistID int64, filePath string, hashTypeID int, v hashvalidator.Validator) (*ValidationOutcome, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open hashlist file %s for validation: %w", filePath, err)
	}
	defer f.Close()

	out := &ValidationOutcome{HasValidator: true}
	invalid := make([]models.InvalidHash, 0, 64)

	scanner := bufio.NewScanner(f)
	// Allow long lines for tokenised formats (NetNTLMv2 / Kerberos).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	lineNo := 0
	for scanner.Scan() {
		raw := scanner.Text()
		lineNo++

		// Skip pure-whitespace and comment lines so they don't inflate counts.
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out.TotalInputLines++

		r := v.Validate(hashTypeID, trimmed)
		if r.Valid {
			out.ValidCount++
			continue
		}
		out.InvalidCount++
		if len(invalid) < repository.MaxInvalidHashRowsPerHashlist {
			invalid = append(invalid, models.InvalidHash{
				HashlistID: hashlistID,
				LineNumber: lineNo,
				Content:    truncateForStorage(raw),
				Reason:     r.Reason,
			})
		} else {
			out.Truncated = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan hashlist file %s: %w", filePath, err)
	}

	if len(invalid) > 0 {
		if err := s.invalidHashRepo.BulkInsert(ctx, nil, hashlistID, invalid); err != nil {
			return nil, fmt.Errorf("failed to persist invalid hashes for hashlist %d: %w", hashlistID, err)
		}
		if len(invalid) > previewSampleSize {
			out.InvalidSample = append(out.InvalidSample, invalid[:previewSampleSize]...)
		} else {
			out.InvalidSample = append(out.InvalidSample, invalid...)
		}
	}
	return out, nil
}

func (s *HashlistValidationService) getValidator(ctx context.Context) (hashvalidator.Validator, error) {
	var initErr error
	s.validatorOnce.Do(func() {
		examples, err := s.hashTypeRepo.GetAllExamples(ctx)
		if err != nil {
			initErr = err
			return
		}
		opts := hashvalidator.DefaultStructural()
		opts = append(opts, hashvalidator.WithExamples(examples))
		s.validator = hashvalidator.New(opts...)
		debug.Info("HashlistValidationService: validator initialized with %d example fallbacks", len(examples))
	})
	if initErr != nil {
		return nil, initErr
	}
	return s.validator, nil
}

// countInputLines returns the number of non-empty, non-comment lines. Used
// for hashlists whose declared mode has no validator coverage so we can still
// populate total_input_lines.
func countInputLines(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open hashlist file %s: %w", filePath, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for scanner.Scan() {
		t := strings.TrimSpace(scanner.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		n++
	}
	return n, scanner.Err()
}

// truncateForStorage caps the persisted content of an invalid line. Some
// password hash dumps include very long lines (e.g. truncated NetNTLMv2
// captures); 2048 bytes is enough for any legitimate hashcat-supported
// format we care to surface, and bounds DB row size.
func truncateForStorage(s string) string {
	const max = 2048
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// buildNoValidatorNotice is centralized so the wording stays consistent
// across the upload response, the hashlists.validation_notice column, and
// the frontend banner. The text is also referenced by tests.
func buildNoValidatorNotice(typeName string) string {
	return fmt.Sprintf(
		"The selected hash type (%s) doesn't have a known validator. Please submit a GitHub issue at https://github.com/ZerkerEOD/krakenhashes/issues/new if you'd like one added. If you experience any issues with failed jobs please make sure your hashes are in the correct format.",
		typeName,
	)
}
