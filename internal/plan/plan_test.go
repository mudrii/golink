package plan_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/plan"
)

func TestLoad_valid(t *testing.T) {
	raw := `{
		"schema": "golink.plan/v1",
		"created_at": "2026-04-17T12:00:00Z",
		"command": "post create",
		"args": {"text": "hello", "visibility": "PUBLIC"},
		"transport": "official",
		"profile": "default"
	}`
	p, err := plan.Load(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Command != "post create" {
		t.Errorf("command = %q, want %q", p.Command, "post create")
	}
	if p.Transport != "official" {
		t.Errorf("transport = %q, want %q", p.Transport, "official")
	}
}

func TestLoad_invalidSchema(t *testing.T) {
	raw := `{"schema":"golink.plan/v99","created_at":"2026-04-17T12:00:00Z","command":"post create","args":{},"transport":"official","profile":"default"}`
	_, err := plan.Load(strings.NewReader(raw))
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
	if !strings.Contains(err.Error(), "unknown plan schema") {
		t.Errorf("error %q does not mention schema", err)
	}
}

func TestLoad_invalidCommand(t *testing.T) {
	raw := `{"schema":"golink.plan/v1","created_at":"2026-04-17T12:00:00Z","command":"post list","args":{},"transport":"official","profile":"default"}`
	_, err := plan.Load(strings.NewReader(raw))
	if err == nil {
		t.Fatal("expected error for non-plannable command, got nil")
	}
	if !strings.Contains(err.Error(), "command not plannable") {
		t.Errorf("error %q does not mention plannable", err)
	}
}

func TestLoad_malformedJSON(t *testing.T) {
	_, err := plan.Load(strings.NewReader("{not json"))
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestSHA256_deterministic(t *testing.T) {
	p := &plan.Plan{
		Schema:    plan.SchemaV1,
		CreatedAt: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Command:   "post create",
		Args:      map[string]any{"text": "hello"},
		Transport: "official",
		Profile:   "default",
	}
	h1 := p.SHA256()
	h2 := p.SHA256()
	if h1 != h2 {
		t.Errorf("SHA256 not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex, got %d chars", len(h1))
	}
}

func TestSHA256_differsOnChange(t *testing.T) {
	base := &plan.Plan{
		Schema:    plan.SchemaV1,
		CreatedAt: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Command:   "post create",
		Args:      map[string]any{"text": "hello"},
		Transport: "official",
		Profile:   "default",
	}
	modified := &plan.Plan{
		Schema:    plan.SchemaV1,
		CreatedAt: base.CreatedAt,
		Command:   "post create",
		Args:      map[string]any{"text": "world"},
		Transport: "official",
		Profile:   "default",
	}
	if base.SHA256() == modified.SHA256() {
		t.Error("expected different SHA256 for different args")
	}
}

func TestIsPlannableCommand(t *testing.T) {
	for _, cmd := range []string{"post create", "post delete", "post edit", "post reshare", "post schedule", "comment add", "react add"} {
		if !plan.IsPlannableCommand(cmd) {
			t.Errorf("expected %q to be plannable", cmd)
		}
	}
	for _, cmd := range []string{"post list", "post get", "search people", "auth status", "version"} {
		if plan.IsPlannableCommand(cmd) {
			t.Errorf("expected %q to be non-plannable", cmd)
		}
	}
}
