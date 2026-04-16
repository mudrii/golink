package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// EnvelopeMeta contains the shared metadata used to build response envelopes.
type EnvelopeMeta struct {
	Status      CommandStatus
	CommandID   string
	Command     string
	Transport   string
	Mode        string
	RequestID   string
	GeneratedAt time.Time
	RateLimit   *RateLimitInfo
}

// Success builds a success envelope with consistent shared metadata.
func Success[D any](meta EnvelopeMeta, data D) SuccessEnvelope[D] {
	return SuccessEnvelope[D]{
		BaseEnvelope: base(meta),
		Data:         data,
	}
}

// Error builds a standard command error envelope.
func Error(meta EnvelopeMeta, code ErrorCode, message, details string) ErrorEnvelope {
	return ErrorEnvelope{
		BaseEnvelope: base(meta),
		Error:        message,
		Code:         code,
		Details:      details,
	}
}

// ValidationError builds a standard validation error envelope.
func ValidationError(meta EnvelopeMeta, message, details string) ValidationErrorEnvelope {
	return ValidationErrorEnvelope{
		BaseEnvelope: base(meta),
		Error:        message,
		Code:         ErrorCodeValidation,
		Details:      details,
	}
}

// WriteJSON encodes a single JSON object followed by a newline.
func WriteJSON(w io.Writer, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return fmt.Errorf("encode json envelope: %w", err)
	}

	return nil
}

func base(meta EnvelopeMeta) BaseEnvelope {
	generatedAt := meta.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	return BaseEnvelope{
		Status:      meta.Status,
		CommandID:   meta.CommandID,
		Command:     meta.Command,
		Transport:   meta.Transport,
		Mode:        meta.Mode,
		RequestID:   meta.RequestID,
		GeneratedAt: generatedAt,
		RateLimit:   meta.RateLimit,
	}
}

// BuildBase constructs a BaseEnvelope from EnvelopeMeta.
// Used by callers that need the raw base for renderer dispatch.
func BuildBase(meta EnvelopeMeta) BaseEnvelope {
	return base(meta)
}

// ExtractErrorEnvelope extracts the BaseEnvelope, error message, and code from
// a typed error payload (ErrorEnvelope or ValidationErrorEnvelope). Returns
// false if the payload is not a recognised error type.
func ExtractErrorEnvelope(payload any) (env BaseEnvelope, msg, code string, ok bool) {
	switch v := payload.(type) {
	case ErrorEnvelope:
		return v.BaseEnvelope, v.Error, string(v.Code), true
	case ValidationErrorEnvelope:
		return v.BaseEnvelope, v.Error, string(v.Code), true
	default:
		return BaseEnvelope{}, "", "", false
	}
}
