package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Output mode constants for --output flag resolution.
const (
	ModeText    = "text"
	ModeJSON    = "json"
	ModeJSONL   = "jsonl"
	ModeCompact = "compact"
	ModeTable   = "table"
)

// TabularData is implemented by list-data types that support table/jsonl rendering.
type TabularData interface {
	Headers() []string
	Rows() [][]string
}

// ValidateMode returns nil for the five valid modes, else an error.
func ValidateMode(mode string) error {
	switch mode {
	case ModeText, ModeJSON, ModeJSONL, ModeCompact, ModeTable:
		return nil
	default:
		return fmt.Errorf("output must be one of text|json|jsonl|compact|table, got %q", mode)
	}
}

// compactSuccess is the compact success envelope shape (lossy — no command_id, generated_at, rate_limit).
type compactSuccess struct {
	Status  CommandStatus `json:"status"`
	Command string        `json:"command"`
	Mode    string        `json:"mode,omitempty"`
	Data    any           `json:"data"`
}

// compactError is the compact error envelope shape.
type compactError struct {
	Status  CommandStatus `json:"status"`
	Command string        `json:"command"`
	Mode    string        `json:"mode,omitempty"`
	Error   string        `json:"error"`
	Code    ErrorCode     `json:"code,omitempty"`
}

// RenderSuccess writes a success envelope to w according to mode.
// For ModeText, the caller-provided text string is emitted as-is.
// For ModeJSON, the full envelope is marshalled.
// For ModeCompact, a stripped envelope (no command_id/generated_at/rate_limit) is emitted.
// For ModeJSONL, list data emits one JSON object per item; scalar data emits the envelope as one line.
// For ModeTable, list data renders via tabwriter; scalar data falls back to text.
//
// Compact and JSONL outputs are lossy renderings — only ModeJSON is schema-validated.
func RenderSuccess(w io.Writer, mode string, envelope BaseEnvelope, data any, text string) error {
	switch mode {
	case ModeJSON, "": // "" treated as json for safety
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(struct {
			BaseEnvelope
			Data any `json:"data"`
		}{envelope, data}); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		return nil

	case ModeCompact:
		env := compactSuccess{
			Status:  envelope.Status,
			Command: envelope.Command,
			Mode:    envelope.Mode,
			Data:    data,
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(env); err != nil {
			return fmt.Errorf("encode compact: %w", err)
		}
		return nil

	case ModeJSONL:
		if td, ok := data.(TabularData); ok {
			return renderJSONL(w, td)
		}
		// Scalar: emit the envelope as a single line.
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(compactSuccess{
			Status:  envelope.Status,
			Command: envelope.Command,
			Mode:    envelope.Mode,
			Data:    data,
		}); err != nil {
			return fmt.Errorf("encode jsonl scalar: %w", err)
		}
		return nil

	case ModeTable:
		if td, ok := data.(TabularData); ok {
			return renderTable(w, td)
		}
		// Scalar: fall back to text.
		_, err := fmt.Fprintln(w, text)
		return err

	default: // ModeText and anything else
		_, err := fmt.Fprintln(w, text)
		return err
	}
}

// RenderError writes an error envelope to w according to mode.
// For text mode the text string is emitted. For all structured modes a
// compact envelope is written (errors are never schema-validated for non-json).
func RenderError(w io.Writer, mode string, envelope BaseEnvelope, errMsg, code, text string) error {
	switch mode {
	case ModeJSON, "":
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		// Full envelope — caller has already built the typed payload.
		// This path is not reached directly; WriteJSON is used for json mode.
		// Kept here for completeness.
		if err := enc.Encode(envelope); err != nil {
			return fmt.Errorf("encode json error: %w", err)
		}
		return nil

	case ModeCompact, ModeJSONL:
		env := compactError{
			Status:  envelope.Status,
			Command: envelope.Command,
			Mode:    envelope.Mode,
			Error:   errMsg,
			Code:    ErrorCode(code),
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(env); err != nil {
			return fmt.Errorf("encode compact error: %w", err)
		}
		return nil

	case ModeTable:
		_, err := fmt.Fprintln(w, text)
		return err

	default: // ModeText
		_, err := fmt.Fprintln(w, text)
		return err
	}
}

// renderJSONL emits one JSON object per row, keyed by the headers.
func renderJSONL(w io.Writer, td TabularData) error {
	headers := td.Headers()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, row := range td.Rows() {
		obj := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(row) {
				obj[strings.ToLower(h)] = row[i]
			}
		}
		if err := enc.Encode(obj); err != nil {
			return fmt.Errorf("encode jsonl row: %w", err)
		}
	}
	return nil
}

const (
	maxCellWidth  = 60
	tableMaxWidth = 120
)

// renderTable writes tabular data using text/tabwriter with two-space padding.
func renderTable(w io.Writer, td TabularData) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	headers := td.Headers()
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))

	// Separator line.
	seps := make([]string, len(headers))
	for i, h := range headers {
		seps[i] = strings.Repeat("-", len(h))
	}
	_, _ = fmt.Fprintln(tw, strings.Join(seps, "\t"))

	for _, row := range td.Rows() {
		cells := make([]string, len(headers))
		for i := range headers {
			if i < len(row) {
				cells[i] = truncateCell(row[i])
			}
		}
		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}

	return tw.Flush()
}

// truncateCell truncates a cell value to maxCellWidth chars with ellipsis.
func truncateCell(s string) string {
	if len(s) <= maxCellWidth {
		return s
	}
	return s[:maxCellWidth] + "..."
}
