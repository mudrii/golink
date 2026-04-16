// Package plan provides the golink.plan/v1 document type used by the
// plan/execute split. A Plan describes a single mutating operation without
// performing it; golink execute loads, validates, and dispatches the plan
// via the normal Transport path.
package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// SchemaV1 is the canonical schema identifier for golink plan documents.
const SchemaV1 = "golink.plan/v1"

// ErrInvalidSchema is returned when the plan document carries an unknown schema string.
var ErrInvalidSchema = errors.New("unknown plan schema")

// ErrInvalidCommand is returned when the command is not in the plannable whitelist.
var ErrInvalidCommand = errors.New("command not plannable")

// plannableCommands is the whitelist of commands that produce side effects and
// therefore benefit from a plan/review step. Read-only commands are excluded.
var plannableCommands = map[string]struct{}{
	"post create":   {},
	"post delete":   {},
	"post edit":     {},
	"post reshare":  {},
	"post schedule": {},
	"comment add":   {},
	"react add":     {},
}

// Plan is a golink.plan/v1 document. It describes one mutating operation
// without executing it. The zero value is not valid; use Load to construct.
type Plan struct {
	Schema         string         `json:"schema"`
	CreatedAt      time.Time      `json:"created_at"`
	Command        string         `json:"command"`
	Args           map[string]any `json:"args"`
	Transport      string         `json:"transport"`
	Profile        string         `json:"profile"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	DryRun         bool           `json:"dry_run,omitempty"`
	Notes          string         `json:"notes,omitempty"`
}

// Load reads and validates a plan document from r.
// It accepts either a bare golink.plan/v1 document or the full output envelope
// produced by `golink plan` (in which case the nested data field is extracted
// automatically). It returns ErrInvalidSchema if the schema field is not
// "golink.plan/v1" and ErrInvalidCommand if the command is not in the
// plannable whitelist.
func Load(r io.Reader) (*Plan, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}

	// Detect envelope: if the top-level JSON has a "data" key whose value
	// contains a "schema" key, unwrap data first so the workflow
	//   golink plan post create ... > plan.json
	//   golink execute plan.json
	// works without manual jq post-processing.
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Data) > 0 {
		var inner struct {
			Schema string `json:"schema"`
		}
		if json.Unmarshal(wrapper.Data, &inner) == nil && inner.Schema != "" {
			raw = wrapper.Data
		}
	}

	var p Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	if p.Schema != SchemaV1 {
		return nil, fmt.Errorf("%w: %q (want %q)", ErrInvalidSchema, p.Schema, SchemaV1)
	}

	if _, ok := plannableCommands[p.Command]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidCommand, p.Command)
	}

	return &p, nil
}

// IsPlannableCommand reports whether cmd is in the plannable whitelist.
func IsPlannableCommand(cmd string) bool {
	_, ok := plannableCommands[cmd]
	return ok
}

// SHA256 returns the hex-encoded SHA-256 of the canonical JSON representation
// of the plan. The result is deterministic for equal plans.
func (p *Plan) SHA256() string {
	canonical, err := json.Marshal(p)
	if err != nil {
		// json.Marshal on a plain struct cannot fail; this branch is unreachable.
		return ""
	}
	// Compact to strip any whitespace variation before hashing.
	var buf bytes.Buffer
	if err := json.Compact(&buf, canonical); err != nil {
		return ""
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}
