package output

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// CompileSchema loads and compiles the shared strict JSON schema.
func CompileSchema(schemaPath string) (*jsonschema.Schema, error) {
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaPath, bytes.NewReader(schemaData)); err != nil {
		return nil, err
	}

	return compiler.Compile(schemaPath)
}

// ValidateEnvelopeRoundTrip validates JSON payload against schema and validates JSON round-trip.
func ValidateEnvelopeRoundTrip(t *testing.T, schemaPath string, payload []byte) {
	t.Helper()

	if !filepath.IsAbs(schemaPath) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to resolve working directory: %v", err)
		}
		schemaPath = filepath.Clean(filepath.Join(cwd, schemaPath))
	}

	schema, err := CompileSchema(schemaPath)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload must be valid JSON: %v", err)
	}

	if err := schema.Validate(decoded); err != nil {
		if validationErr, ok := err.(*jsonschema.ValidationError); ok {
			t.Fatalf("schema validation failed: %s at %s", validationErr.Message, validationErr.InstanceLocation)
		}
		t.Fatalf("schema validation failed: %v", err)
	}

	roundTrip, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decodedAgain map[string]any
	if err := json.Unmarshal(roundTrip, &decodedAgain); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(decoded, decodedAgain) {
		t.Fatalf("round-trip mismatch\ninput: %#v\noutput: %#v", decoded, decodedAgain)
	}
}
