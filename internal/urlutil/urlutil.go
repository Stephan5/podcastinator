// Package urlutil encodes, escapes and slugifies URLs.
package urlutil

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

var encodedPattern = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

// IsAlreadyEncoded reports whether s has any percent-encoded sequences.
func IsAlreadyEncoded(s string) bool {
	return encodedPattern.MatchString(s)
}

// Encode percent-encodes rawURL, unless it has percent-encoded sequences
// already.
func Encode(rawURL string) (string, error) {
	if IsAlreadyEncoded(rawURL) {
		return rawURL, nil
	}
	decoded, err := url.QueryUnescape(strings.ReplaceAll(rawURL, "+", "%2B"))
	if err != nil {
		decoded = rawURL
	}
	u, err := url.Parse(decoded)
	if err != nil {
		return rawURL, err
	}
	return u.String(), nil
}

// HTMLEscape escapes special HTML characters in s.
func HTMLEscape(s string) string {
	return html.EscapeString(s)
}

// HTMLUnescape unescapes HTML entities in s.
func HTMLUnescape(s string) string {
	return html.UnescapeString(s)
}

// StripQuery removes the query string from a URL.
func StripQuery(rawURL string) string {
	before, _, ok := strings.Cut(rawURL, "?")
	if !ok {
		return rawURL
	}
	return before
}

// Slugify lowercases s and joins its runs of ASCII letters and digits with
// hyphens.
func Slugify(s string) string {
	var b strings.Builder
	prevHyphen := true // no leading hyphen
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// MimeTypeForExtension returns the enclosure MIME type for a podcast file
// extension. .m4a uses audio/x-m4a, the type Apple Podcasts lists for it.
// Unknown extensions get audio/mpeg.
func MimeTypeForExtension(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "m4a":
		return "audio/x-m4a"
	case "m4b":
		return "audio/x-m4b"
	case "ogg":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	}
	return "audio/mpeg"
}
