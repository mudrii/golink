package cmd

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/output"
)

// authenticatedStoreWithScopes seeds a session that includes the given scopes.
func authenticatedStoreWithScopes(t *testing.T, scopes []string) auth.Store {
	t.Helper()
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "token-xyz",
		MemberURN:   "urn:li:person:abc123",
		ProfileID:   "abc123",
		Name:        "Ion Mudreac",
		Email:       "ion@example.com",
		Scopes:      scopes,
		ExpiresAt:   time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return store
}

func TestOrgListSuccess(t *testing.T) {
	store := authenticatedStoreWithScopes(t, []string{"openid", "profile", "email", "w_member_social_feed", "w_organization_social_feed"})

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "org", "list"},
		testDepsOptions{
			store:            store,
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Count int `json:"count"`
			Items []struct {
				URN  string `json:"urn"`
				Name string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("status = %q, want ok", payload.Status)
	}
	if payload.Data.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Data.Count)
	}
	if len(payload.Data.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(payload.Data.Items))
	}
	if payload.Data.Items[0].URN != "urn:li:organization:111" {
		t.Fatalf("items[0].urn = %q", payload.Data.Items[0].URN)
	}
	if payload.Data.Items[0].Name != "Acme Corp" {
		t.Fatalf("items[0].name = %q", payload.Data.Items[0].Name)
	}
}

func TestOrgListMissingScope(t *testing.T) {
	// Session without organization social write scope.
	store := authenticatedStoreWithScopes(t, []string{"openid", "profile", "email", "w_member_social_feed"})

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "org", "list"},
		testDepsOptions{
			store:            store,
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 2 {
		t.Fatalf("expected exit 2 (validation error), got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "validation_error" {
		t.Fatalf("status = %q, want validation_error", payload.Status)
	}
}

func TestOrgListNoSession(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "org", "list"},
		testDepsOptions{})
	if code != 4 {
		t.Fatalf("expected exit 4 (auth error), got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestPostCreateAsOrgMissingScope(t *testing.T) {
	// Session without organization social write scope.
	store := authenticatedStoreWithScopes(t, []string{"openid", "profile", "email", "w_member_social_feed"})

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "hello", "--as-org", "urn:li:organization:111"},
		testDepsOptions{
			store:            store,
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 2 {
		t.Fatalf("expected exit 2 (validation error), got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "validation_error" {
		t.Fatalf("status = %q, want validation_error", payload.Status)
	}
}

func TestPostCreateAsOrgInvalidURN(t *testing.T) {
	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "post", "create", "--text", "hello", "--as-org", "notaurn"},
		testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit 2 for invalid --as-org, got %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestPostCreateAsOrgDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "post", "create", "--text", "org post", "--as-org", "urn:li:organization:111"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Mode string `json:"mode"`
		Data struct {
			WouldPost struct {
				AuthorURN string `json:"author_urn"`
			} `json:"would_post"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Mode != "dry_run" {
		t.Fatalf("mode = %q, want dry_run", payload.Mode)
	}
	if payload.Data.WouldPost.AuthorURN != "urn:li:organization:111" {
		t.Fatalf("author_urn = %q, want urn:li:organization:111", payload.Data.WouldPost.AuthorURN)
	}
}

func TestPostCreateAsOrgLive(t *testing.T) {
	store := authenticatedStoreWithScopes(t, []string{"openid", "profile", "email", "w_member_social_feed", "w_organization_social_feed"})

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "org post", "--as-org", "urn:li:organization:111"},
		testDepsOptions{
			store:            store,
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || payload.Data.ID != "urn:li:share:42" {
		t.Fatalf("envelope = %+v", payload)
	}
}
