package privacy

import (
	"net/url"
	"strings"
	"testing"
)

func TestURLRedactsPersonalQueryValues(t *testing.T) {
	raw := "https://api.linkedin.com/v2/ugcPosts?author=urn%3Ali%3Amember%3Aabc123&email=ion@example.com&count=10"

	got := URL(raw)
	for _, leaked := range []string{"urn:li:member:abc123", "abc123", "ion@example.com"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("URL leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "count=10") {
		t.Fatalf("URL should preserve non-personal query values: %s", got)
	}
}

func TestJSONRedactsPersonalFieldsAndFreeText(t *testing.T) {
	raw := []byte(`{"data":{"author_urn":"urn:li:person:abc123","email":"ion@example.com","text":"private post","visibility":"PUBLIC"}}`)

	got := string(JSON(raw))
	for _, leaked := range []string{"urn:li:person:abc123", "ion@example.com", "private post"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("JSON leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "PUBLIC") {
		t.Fatalf("JSON should preserve non-personal fields: %s", got)
	}
}

func TestStringRedactsLinkedInURNsAndLocalPaths(t *testing.T) {
	raw := `owner urn:li:organization:123 at /Users/ada/private/image.png and ~/secret.txt`

	got := String(raw)
	for _, leaked := range []string{"urn:li:organization:123", "/Users/ada", "~/secret.txt"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("String leaked %q: %s", leaked, got)
		}
	}
}

func TestFormRedactsSensitiveAndPersonalValues(t *testing.T) {
	got := string(Form([]byte("client_secret=secret-123&author=urn%3Ali%3Aperson%3Aabc123&email=ion%40example.com&count=10&visibility=PUBLIC")))
	values, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("parse redacted form: %v", err)
	}

	for _, key := range []string{"client_secret", "author", "email"} {
		if values.Get(key) != redacted {
			t.Fatalf("%s = %q, want %q in %s", key, values.Get(key), redacted, got)
		}
	}
	if values.Get("count") != "10" {
		t.Fatalf("count = %q, want 10 in %s", values.Get("count"), got)
	}
	if values.Get("visibility") != "PUBLIC" {
		t.Fatalf("visibility = %q, want PUBLIC in %s", values.Get("visibility"), got)
	}
}

func TestFormMalformedBodyRedactsWholePayload(t *testing.T) {
	got := string(Form([]byte("%zz")))
	if got != redacted {
		t.Fatalf("Form malformed = %q, want %q", got, redacted)
	}
}

func TestJSONRedactsNestedArraysAndMalformedPayload(t *testing.T) {
	raw := []byte(`{"items":[{"author_urn":"urn:li:member:abc123","nested":["ion@example.com","PUBLIC"]}]}`)
	got := string(JSON(raw))
	for _, leaked := range []string{"urn:li:member:abc123", "ion@example.com", "abc123"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("JSON leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "PUBLIC") {
		t.Fatalf("JSON should preserve non-personal nested value: %s", got)
	}

	if got := string(JSON([]byte(`{`))); got != redacted {
		t.Fatalf("malformed JSON = %q, want %q", got, redacted)
	}
}

func TestSensitiveKeyVariants(t *testing.T) {
	for _, key := range []string{"access_token", "refresh-token", "client.secret", "memberURN", "image_path", "text"} {
		if !SensitiveKey(key) {
			t.Fatalf("SensitiveKey(%q) = false", key)
		}
	}
	for _, key := range []string{"visibility", "count", "state"} {
		if SensitiveKey(key) {
			t.Fatalf("SensitiveKey(%q) = true", key)
		}
	}
}
