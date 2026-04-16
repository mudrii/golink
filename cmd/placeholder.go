package cmd

import "strings"

// trimmedText returns the input with leading and trailing whitespace stripped.
func trimmedText(raw string) string {
	return strings.TrimSpace(raw)
}

// fallback returns value when non-empty, otherwise the default.
func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
