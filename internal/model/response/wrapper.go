package response

// APIResponse is the standardized envelope for all API responses.
//
// Field names are camelCase, which is the convention for the whole API: a JSON
// key is a single lowercase word or lowerCamelCase, never snake_case. There is
// a test in this package that enforces it across every response type.
type APIResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    interface{}       `json:"data,omitempty"`
	Meta    *Meta             `json:"meta,omitempty"`
	Errors  map[string]string `json:"errors,omitempty"`

	// RequestID and TraceID are attached to error responses so a user can quote
	// an identifier back that leads straight to the server-side logs and trace.
	// They are omitted from successful responses, where the X-Request-ID header
	// already carries the same value and the body should stay lean.
	RequestID string `json:"requestId,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
}

// Meta holds pagination metadata
type Meta struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

// OK returns a success response with data
func OK(message string, data interface{}) APIResponse {
	return APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	}
}

// OKWithMeta returns a success response with data and pagination metadata
func OKWithMeta(message string, data interface{}, meta *Meta) APIResponse {
	return APIResponse{
		Success: true,
		Message: message,
		Data:    data,
		Meta:    meta,
	}
}

// Err returns an error response
func Err(message string) APIResponse {
	return APIResponse{
		Success: false,
		Message: message,
	}
}

// ValidationErr returns a validation error response with field-level errors
func ValidationErr(message string, errors map[string]string) APIResponse {
	return APIResponse{
		Success: false,
		Message: message,
		Errors:  errors,
	}
}

// WithCorrelation returns a copy tagged with the identifiers that tie this
// response to its log lines and its trace. Empty values are left off.
func (r APIResponse) WithCorrelation(requestID, traceID string) APIResponse {
	r.RequestID = requestID
	r.TraceID = traceID
	return r
}
