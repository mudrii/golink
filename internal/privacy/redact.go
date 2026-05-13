// Package privacy contains redaction helpers for local persistence surfaces.
package privacy

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

const redacted = "REDACTED"

var (
	emailPattern       = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)
	linkedinURNPattern = regexp.MustCompile(`urn:li:(person|member|organization):[A-Za-z0-9_-]+`)
	unixUserPath       = regexp.MustCompile(`/Users/[A-Za-z0-9._-]+(?:/[^\s"'<>]*)?`)
	homeRelativePath   = regexp.MustCompile(`~/[^\s"'<>]+`)
)

// String redacts personal identifiers inside a scalar string.
func String(s string) string {
	s = linkedinURNPattern.ReplaceAllString(s, redacted)
	s = unixUserPath.ReplaceAllString(s, redacted)
	s = homeRelativePath.ReplaceAllString(s, redacted)
	s = emailPattern.ReplaceAllString(s, redacted)
	return s
}

// URL redacts personal identifiers in URL paths and query values while keeping
// enough structure for deterministic record/replay matching.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return String(raw)
	}

	u.Path = String(u.Path)
	u.RawPath = ""

	query := u.Query()
	for key, values := range query {
		for i := range values {
			if SensitiveKey(key) || String(values[i]) != values[i] {
				values[i] = redacted
			}
		}
		query[key] = values
	}
	u.RawQuery = query.Encode()

	return u.String()
}

// JSON redacts sensitive fields in a JSON document. If the body is not valid
// JSON, it returns a redacted placeholder rather than persisting raw text.
func JSON(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return []byte(redacted)
	}
	v = Value("", v)
	out, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"redacted":true}`)
	}
	return out
}

// Form redacts sensitive values in an application/x-www-form-urlencoded body.
func Form(body []byte) []byte {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return []byte(redacted)
	}
	for key, vs := range values {
		for i := range vs {
			if SensitiveKey(key) || String(vs[i]) != vs[i] {
				vs[i] = redacted
			}
		}
		values[key] = vs
	}
	return []byte(values.Encode())
}

// Value redacts sensitive fields in a decoded JSON-like value.
func Value(key string, v any) any {
	if SensitiveKey(key) {
		return redacted
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = Value(k, val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = Value("", x[i])
		}
		return out
	case string:
		return String(x)
	default:
		return v
	}
}

// SensitiveKey reports whether a JSON/form field commonly carries personal
// data, local paths, or credentials.
func SensitiveKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	switch normalized {
	case "accesstoken",
		"refreshtoken",
		"idtoken",
		"authorization",
		"cookie",
		"setcookie",
		"clientsecret",
		"codeverifier",
		"codechallenge",
		"email",
		"emailaddress",
		"sub",
		"memberurn",
		"authorurn",
		"actorurn",
		"ownerurn",
		"personurn",
		"author",
		"actor",
		"owner",
		"member",
		"firstname",
		"lastname",
		"localizedfirstname",
		"localizedlastname",
		"fullname",
		"localizedname",
		"name",
		"headline",
		"location",
		"profilepicture",
		"vanityname",
		"text",
		"commentary",
		"comment",
		"message",
		"body",
		"imagealt",
		"imagepath",
		"filepath":
		return true
	default:
		return false
	}
}
