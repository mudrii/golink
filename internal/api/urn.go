package api

import (
	"fmt"
	"strings"
)

// EncodeURN URL-encodes a URN value so it can be embedded in a LinkedIn REST
// path segment (e.g. urn:li:ugcPost:123 -> urn%3Ali%3AugcPost%3A123).
// net/url.PathEscape deliberately leaves the colon unescaped because it is
// legal in a path segment, but LinkedIn's Rest.li routes require the colon to
// be percent-encoded, so we do it explicitly here.
func EncodeURN(urn string) (string, error) {
	trimmed := strings.TrimSpace(urn)
	if trimmed == "" {
		return "", fmt.Errorf("urn must not be empty")
	}
	if !strings.HasPrefix(trimmed, "urn:") {
		return "", fmt.Errorf("urn must start with urn")
	}

	return strings.ReplaceAll(trimmed, ":", "%3A"), nil
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
