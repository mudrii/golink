package output

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const defaultSchemaPath = "../../schemas/golink-output.schema.json"

// Test fixtures for every command output and error envelope shape described in PROMPT_golink.md v3.
type schemaFixture struct {
	name    string
	payload []byte
}

func schemaFixtures() []schemaFixture {
	var fixtures []schemaFixture
	fixtures = append(fixtures, approvalSchemaFixtures()...)
	fixtures = append(fixtures, authSchemaFixtures()...)
	fixtures = append(fixtures, batchSchemaFixtures()...)
	fixtures = append(fixtures, commentSchemaFixtures()...)
	fixtures = append(fixtures, doctorSchemaFixtures()...)
	fixtures = append(fixtures, errorSchemaFixtures()...)
	fixtures = append(fixtures, miscSchemaFixtures()...)
	fixtures = append(fixtures, orgSchemaFixtures()...)
	fixtures = append(fixtures, planSchemaFixtures()...)
	fixtures = append(fixtures, post1SchemaFixtures()...)
	fixtures = append(fixtures, post2SchemaFixtures()...)
	fixtures = append(fixtures, profileSchemaFixtures()...)
	fixtures = append(fixtures, reactSchemaFixtures()...)
	fixtures = append(fixtures, schedule1SchemaFixtures()...)
	fixtures = append(fixtures, schedule2SchemaFixtures()...)
	fixtures = append(fixtures, searchSchemaFixtures()...)
	fixtures = append(fixtures, socialSchemaFixtures()...)
	fixtures = append(fixtures, unsupportedSchemaFixtures()...)
	fixtures = append(fixtures, versionSchemaFixtures()...)
	return fixtures
}

func TestGolinkOutputSchemaRoundTrips(t *testing.T) {
	for _, tc := range schemaFixtures() {
		t.Run(tc.name, func(t *testing.T) {
			ValidateEnvelopeRoundTrip(t, defaultSchemaPath, tc.payload)
		})
	}
}

// ensure default schema path exists and is an actual file at runtime
func TestSchemaFileExists(t *testing.T) {
	path := defaultSchemaPath
	if !filepath.IsAbs(path) {
		if cwd, err := os.Getwd(); err == nil {
			path = filepath.Clean(filepath.Join(cwd, path))
		}
	}
	if _, err := os.Stat(path); err != nil {
		if err, ok := err.(*fs.PathError); ok {
			t.Fatalf("schema file missing: %v", err)
		}
		t.Fatalf("schema file check failed: %v", err)
	}
}
