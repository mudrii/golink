package privacy

import (
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
