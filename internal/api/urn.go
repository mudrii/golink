package api

import (
	"fmt"
	"net/url"
	"strings"
)

// EncodeURN URL-encodes a URN value so it can be embedded in LinkedIn REST
// paths and manually-built Rest.li query fragments.
func EncodeURN(urn string) (string, error) {
	trimmed := strings.TrimSpace(urn)
	if trimmed == "" {
		return "", fmt.Errorf("urn must not be empty")
	}
	if !strings.HasPrefix(trimmed, "urn:") {
		return "", fmt.Errorf("urn must start with urn")
	}

	return strings.ReplaceAll(url.QueryEscape(trimmed), "+", "%20"), nil
}

// ActivityURNFromPost converts a share/ugcPost URN into the corresponding
// activity URN used by the comments API. LinkedIn preserves the numeric id
// across URN families, so the conversion is a prefix rewrite.
func ActivityURNFromPost(postURN string) (string, error) {
	trimmed := strings.TrimSpace(postURN)
	if trimmed == "" {
		return "", fmt.Errorf("post urn must not be empty")
	}
	switch {
	case strings.HasPrefix(trimmed, "urn:li:activity:"):
		return trimmed, nil
	case strings.HasPrefix(trimmed, "urn:li:share:"):
		return "urn:li:activity:" + strings.TrimPrefix(trimmed, "urn:li:share:"), nil
	case strings.HasPrefix(trimmed, "urn:li:ugcPost:"):
		return "urn:li:activity:" + strings.TrimPrefix(trimmed, "urn:li:ugcPost:"), nil
	default:
		return "", fmt.Errorf("unrecognized post urn family: %s", trimmed)
	}
}
