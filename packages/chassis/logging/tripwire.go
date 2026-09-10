package logging

import "strings"

// SecretPrefixes returns the literal credential prefixes scanned for in message
// text. The list itself is in vocab.gen.go.
func SecretPrefixes() []string { return append([]string(nil), secretPrefixes...) }

// ScanMessage reports the first credential shape found in msg.
func ScanMessage(msg string) (pattern string, hit bool) {
	for _, p := range secretPrefixes {
		if strings.Contains(msg, p) {
			return p, true
		}
	}
	return "", false
}
