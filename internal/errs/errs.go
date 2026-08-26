// Package errs defines the stable error codes and the uniform rejection
// envelope used across the steel-fireproofing evidence-closure domain.
// Every business rejection carries a deterministic Code, a human message,
// an ordered list of reasons and the operation/version context so callers
// can rely on machine-readable failure boundaries.
package errs

// Code is a stable, machine-readable rejection identifier. Codes are grouped
// by failure boundary (catalog/input, construction state, resource
// contention, device faults, idempotency and terminal arbitration).
type Code string

const (
	// Catalog and input validation boundaries.
	CodeCatalogStale       Code = "CATALOG_STALE"
	CodeMemberMismatch     Code = "MEMBER_MISMATCH"
	CodeFireRatingMismatch Code = "FIRE_RATING_MISMATCH"
	CodeFaceIncomplete     Code = "FACE_INCOMPLETE"
	CodePointDuplicate     Code = "POINT_DUPLICATE"
	CodePrimerIncompatible Code = "PRIMER_INCOMPATIBLE"
	CodeFixedPointOverflow Code = "FIXED_POINT_OVERFLOW"

	// Construction state boundaries.
	CodeCoatOutOfOrder      Code = "COAT_OUT_OF_ORDER"
	CodeCoatDuplicate       Code = "COAT_DUPLICATE"
	CodeGenerationStale     Code = "GENERATION_STALE"
	CodeMaterialExpired     Code = "MATERIAL_EXPIRED"
	CodeOpenTimeExpired     Code = "OPEN_TIME_EXPIRED"
	CodeCuringWindowInvalid Code = "CURING_WINDOW_INVALID"

	// Resource contention boundaries.
	CodeMaterialInsufficient Code = "MATERIAL_INSUFFICIENT"
	CodeLeaseBusy            Code = "LEASE_BUSY"
	CodeLeaseExpired         Code = "LEASE_EXPIRED"

	// Device fault boundaries.
	CodeDeviceRejected      Code = "DEVICE_REJECTED"
	CodeDeviceDisconnected  Code = "DEVICE_DISCONNECTED"
	CodeDeviceTimeout       Code = "DEVICE_TIMEOUT"
	CodeDeviceFormatInvalid Code = "DEVICE_FORMAT_INVALID"

	// Idempotency and arbitration boundaries.
	CodeIdempotencyConflict    Code = "IDEMPOTENCY_CONFLICT"
	CodeGenerationConflict     Code = "GENERATION_CONFLICT"
	CodeTerminalAlreadyDecided Code = "TERMINAL_ALREADY_DECIDED"

	// Generic boundary.
	CodeInvalidInput Code = "INVALID_INPUT"
	CodeNotFound     Code = "NOT_FOUND"
)

// Error is a stable domain rejection carrying a code, ordered reasons and
// the operation/version context that the HTTP layer serializes verbatim.
type Error struct {
	Code           Code
	Message        string
	Reasons        []string
	OperationID    string
	CurrentVersion int64
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// New constructs a domain error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithReasons attaches an ordered list of rejection reasons and returns the
// same error for fluent construction.
func (e *Error) WithReasons(reasons ...string) *Error {
	e.Reasons = append(e.Reasons, reasons...)
	return e
}

// WithOperation sets the idempotency operation identifier.
func (e *Error) WithOperation(op string) *Error {
	e.OperationID = op
	return e
}

// WithVersion sets the aggregate version observed at rejection time.
func (e *Error) WithVersion(v int64) *Error {
	e.CurrentVersion = v
	return e
}
