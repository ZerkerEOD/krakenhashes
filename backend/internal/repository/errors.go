package repository

import "errors"

var (
	// ErrNotFound is returned when a requested resource is not found
	ErrNotFound = errors.New("resource not found")

	// ErrTaskTerminal is returned when a guarded state transition was refused
	// because the row exists but is ALREADY in a terminal status
	// ('completed', 'cancelled', 'failed').
	//
	// This is deliberately distinct from ErrNotFound: "the task you are
	// talking about is gone" and "your message lost a race against another
	// path that already finalised this task" demand opposite reactions.
	// ErrNotFound means the caller should give up quietly; ErrTaskTerminal
	// means the caller must NOT retry, must NOT resurrect the row, and must
	// NOT run any of the once-only side effects (benchmark EMA updates,
	// completion notifications, job-completion cascades) that normally
	// follow a successful transition — someone else already ran them.
	//
	// Callers use errors.Is to tell the two apart. The crack-handshake
	// paths in internal/integration rely on this: three independently
	// scheduled agent messages can each believe they are the one finishing
	// a task, and the guarded UPDATE + this sentinel are what make exactly
	// one of them right.
	ErrTaskTerminal = errors.New("task is already in a terminal status")

	// ErrInvalidStatus is returned when an invalid status is provided
	ErrInvalidStatus = errors.New("invalid status")

	// ErrInvalidVoucher is returned when a voucher is invalid or expired
	ErrInvalidVoucher = errors.New("invalid or expired voucher")

	// ErrVoucherAlreadyUsed is returned when a single-use voucher has already been used
	ErrVoucherAlreadyUsed = errors.New("voucher has already been used")

	// ErrVoucherDeactivated is returned when attempting to use a deactivated voucher
	ErrVoucherDeactivated = errors.New("voucher has been deactivated")

	// ErrVoucherExpired is returned when attempting to use an expired voucher
	ErrVoucherExpired = errors.New("voucher has expired")

	// ErrDuplicateToken is returned when attempting to create an agent with a duplicate token
	ErrDuplicateToken = errors.New("agent token already exists")

	// ErrInvalidToken is returned when an invalid token is provided
	ErrInvalidToken = errors.New("invalid token")

	// ErrAgentNotFound is returned when an agent is not found
	ErrAgentNotFound = errors.New("agent not found")

	// ErrInvalidHardware is returned when invalid hardware information is provided
	ErrInvalidHardware = errors.New("invalid hardware information")

	// ErrInvalidMetrics is returned when invalid metrics are provided
	ErrInvalidMetrics = errors.New("invalid metrics")

	// ErrDuplicateRecord is returned when attempting to create a record that violates a unique constraint
	ErrDuplicateRecord = errors.New("duplicate record")
)
