package cloud

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
)

// The three AWS codes below were collapsed into a bare ErrOfferUnavailable, so
// a permanent account quota and a transient capacity shortage produced the
// identical operator-facing line ("cloud offer no longer available"). They need
// opposite responses — retry vs. file a limit increase — so the wrap has to
// keep the sentinel (retry semantics) AND carry the code (diagnosis).

func apiErr(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: code + " from test"}
}

func TestIsCapacityError(t *testing.T) {
	capacity := []string{
		"InsufficientInstanceCapacity",
		"VcpuLimitExceeded",
		"MaxSpotInstanceCountExceeded",
	}
	for _, code := range capacity {
		if !isCapacityError(apiErr(code)) {
			t.Errorf("isCapacityError(%s) = false, want true", code)
		}
	}

	// Anything else must NOT be treated as retryable: retrying is only safe
	// because the sentinel means "the provider rejected the create, so no
	// instance exists". A misclassified ambiguous error could retry on top of
	// an instance that is already billing.
	notCapacity := []string{
		"UnauthorizedOperation",
		"InvalidParameterValue",
		"RequestLimitExceeded",
		"InvalidSubnetID.NotFound",
	}
	for _, code := range notCapacity {
		if isCapacityError(apiErr(code)) {
			t.Errorf("isCapacityError(%s) = true, want false", code)
		}
	}

	if isCapacityError(errors.New("plain error")) {
		t.Error("isCapacityError(non-API error) = true, want false")
	}
	if isCapacityError(nil) {
		t.Error("isCapacityError(nil) = true, want false")
	}
}

// The wrap must satisfy both contracts at once, which is the whole point:
// ProvisionForJob keys retry on errors.Is, and the operator keys diagnosis on
// the text.
func TestCapacityErrorWrapKeepsSentinelAndCode(t *testing.T) {
	for _, code := range []string{
		"InsufficientInstanceCapacity",
		"VcpuLimitExceeded",
		"MaxSpotInstanceCountExceeded",
	} {
		raw := apiErr(code)
		wrapped := fmt.Errorf("%w: %w", ErrOfferUnavailable, raw)

		if !errors.Is(wrapped, ErrOfferUnavailable) {
			t.Errorf("%s: errors.Is(ErrOfferUnavailable) = false; retry-next-candidate would break", code)
		}
		if !strings.Contains(wrapped.Error(), code) {
			t.Errorf("%s: wrapped error %q does not name the AWS code", code, wrapped.Error())
		}

		// The raw API error must stay reachable, not just be flattened into a
		// string, so callers can still switch on the code.
		var got smithy.APIError
		if !errors.As(wrapped, &got) {
			t.Errorf("%s: errors.As(smithy.APIError) = false", code)
		} else if got.ErrorCode() != code {
			t.Errorf("%s: recovered code = %s", code, got.ErrorCode())
		}
	}
}
