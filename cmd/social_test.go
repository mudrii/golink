package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mudrii/golink/internal/output"
)

func TestSocialMetadataNoArgs(t *testing.T) {
	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "social", "metadata"},
		testDepsOptions{})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, stderr)
	}
}

func TestSocialMetadataTooManyURNs(t *testing.T) {
	urns := make([]string, 101)
	for i := range urns {
		urns[i] = "urn:li:share:1"
	}
	args := append([]string{"--json", "social", "metadata"}, urns...)
	code, _, stderr := executeTestCommand(t, args, testDepsOptions{
		store:            authenticatedStore(t),
		transportFactory: factoryReturning(&fakeTransport{name: "official"}),
	})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, stderr)
	}
}

func TestSocialMetadataSuccess(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "social", "metadata", "urn:li:share:1", "urn:li:share:2"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}

	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var env struct {
		Status  string `json:"status"`
		Command string `json:"command"`
		Data    struct {
			Count int `json:"count"`
			Items []struct {
				PostURN   string `json:"post_urn"`
				LikeCount int    `json:"like_count"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, stdout.String())
	}
	if env.Status != "ok" {
		t.Errorf("status = %q, want ok", env.Status)
	}
	if env.Command != "social metadata" {
		t.Errorf("command = %q, want 'social metadata'", env.Command)
	}
	if env.Data.Count != 2 {
		t.Errorf("count = %d, want 2", env.Data.Count)
	}
	if len(env.Data.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(env.Data.Items))
	}
	if env.Data.Items[0].PostURN != "urn:li:share:1" {
		t.Errorf("item[0].post_urn = %q", env.Data.Items[0].PostURN)
	}
}

func TestSocialMetadataTableOutput(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--output=table", "social", "metadata", "urn:li:share:111"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "COMMENTS") {
		t.Errorf("table header missing COMMENTS; got:\n%s", out)
	}
	if !strings.Contains(out, "REACTIONS") {
		t.Errorf("table header missing REACTIONS; got:\n%s", out)
	}
}
