// Package urlutil provides URL and HTML encoding/decoding helpers.
package urlutil

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

var encodedPattern = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

// IsAlreadyEncoded returns true if the string contains percent-encoded sequences.
func IsAlreadyEncoded(s string) bool {
	return encodedPattern.MatchString(s)
}

// Encode percent-encodes a URL, preserving safe characters :/()?&=
// If the URL already has encoded sequences, it is returned unchanged.
func Encode(rawURL string) (string, error) {
	if IsAlreadyEncoded(rawURL) {
		return rawURL, nil
	}
	// Decode first (in case of mixed state) then re-encode
	decoded, err := url.QueryUnescape(strings.ReplaceAll(rawURL, "+", "%2B"))
	if err != nil {
		decoded = rawURL
	}
	u, err := url.Parse(decoded)
	if err != nil {
		return rawURL, err
	}
	// Re-assemble with encoded path/query
	return u.String(), nil
}

// Decode percent-decodes a URL.
func Decode(rawURL string) string {
	decoded, err := url.QueryUnescape(strings.ReplaceAll(rawURL, "+", "%2B"))
	if err != nil {
		return rawURL
	}
	return decoded
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
	idx := strings.IndexByte(rawURL, '?')
	if idx == -1 {
		return rawURL
	}
	return rawURL[:idx]
}

// ConvertHTTPToS3 converts an S3 HTTPS URL to an s3:// URI.
// e.g. https://s3.eu-west-2.amazonaws.com/mybucket/key → s3://mybucket/key
func ConvertHTTPToS3(rawURL string) string {
	clean := StripQuery(rawURL)
	// Match https://s3*.amazonaws.com/<bucket>/...
	const awsPrefix = "amazonaws.com/"
	idx := strings.Index(clean, awsPrefix)
	if idx == -1 {
		return clean
	}
	rest := clean[idx+len(awsPrefix):]
	return "s3://" + rest
}

// Slugify converts a string to a lowercase hyphen-separated slug.
func Slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevHyphen := true // to avoid leading hyphen
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else {
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	result := strings.TrimRight(b.String(), "-")
	return result
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
