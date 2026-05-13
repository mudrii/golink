package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// negativeSchemaFixture is a payload the schema MUST reject. Pair every shape
// rule the CLI relies on (e.g. minLength on text bodies) with a fixture here so
// the contract failure mode is exercised, not just the happy path.
type negativeSchemaFixture struct {
	name    string
	payload []byte
}

func negativeSchemaFixtures() []negativeSchemaFixture {
	return []negativeSchemaFixture{
		{
			// LinkedIn rejects empty post bodies; an edited post with an empty
			// "text" must fail the CLI's own contract before any network call.
			name: "post edit empty text",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_edit_empty_text_01",
				"command": "post edit",
				"transport": "official",
				"generated_at": "2026-04-17T12:00:00Z",
				"data": {
					"id": "urn:li:share:42",
					"created_at": "2026-04-16T09:00:00Z",
					"text": "",
					"visibility": "PUBLIC",
					"url": "https://www.linkedin.com/feed/update/urn:li:share:42",
					"author_urn": "urn:li:person:abc123",
					"updated_at": "2026-04-17T12:00:00Z"
				}
			}`),
		},
	}
}

func TestGolinkOutputSchemaRejectsInvalidPayloads(t *testing.T) {
	path := defaultSchemaPath
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("resolve cwd: %v", err)
		}
		path = filepath.Clean(filepath.Join(cwd, path))
	}
	schema, err := CompileSchema(path)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	for _, tc := range negativeSchemaFixtures() {
		t.Run(tc.name, func(t *testing.T) {
			var decoded map[string]any
			if err := json.Unmarshal(tc.payload, &decoded); err != nil {
				t.Fatalf("fixture must be valid JSON: %v", err)
			}
			err := schema.Validate(decoded)
			if err == nil {
				t.Fatalf("schema accepted payload that should be rejected")
			}
			if _, ok := err.(*jsonschema.ValidationError); !ok {
				t.Fatalf("expected *jsonschema.ValidationError, got %T: %v", err, err)
			}
		})
	}
}
