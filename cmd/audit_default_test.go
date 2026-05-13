package cmd

import (
	"testing"

	"github.com/mudrii/golink/internal/audit"
)

// TestApp_AuditSinkDefaultsToNoopWhenUnset asserts that normalizeDependencies
// returns a non-nil audit.Sink even when none is injected. The auditMutation
// path must never have to nil-check the sink — that would leak the nullable
// contract into every call site.
func TestApp_AuditSinkDefaultsToNoopWhenUnset(t *testing.T) {
	deps := normalizeDependencies(Dependencies{})
	if deps.AuditSink == nil {
		t.Fatal("AuditSink: want non-nil default, got nil")
	}
	if _, ok := deps.AuditSink.(audit.NoopSink); !ok {
		t.Fatalf("AuditSink: want audit.NoopSink default, got %T", deps.AuditSink)
	}
}
