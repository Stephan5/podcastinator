package urlutil_test

import (
	"testing"

	"github.com/Stephan5/podcastinator/internal/urlutil"
)

func TestIsAlreadyEncoded(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/foo%20bar", true},
		{"https://example.com/foo bar", false},
		{"https://example.com/path?id=1", false},
		{"https://s3.amazonaws.com/bucket/001-foo%20bar.mp3", true},
	}
	for _, tc := range cases {
		got := urlutil.IsAlreadyEncoded(tc.in)
		if got != tc.want {
			t.Errorf("IsAlreadyEncoded(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestEncode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			"https://example.com/foo%20bar.mp3",
			"https://example.com/foo%20bar.mp3",
		},
		{
			"https://example.com/path/file.mp3",
			"https://example.com/path/file.mp3",
		},
		{
			"https://example.com/file.mp3?id=1",
			"https://example.com/file.mp3?id=1",
		},
	}
	for _, tc := range cases {
		got, err := urlutil.Encode(tc.in)
		if err != nil {
			t.Errorf("Encode(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Encode(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestHTMLEscapeUnescape(t *testing.T) {
	cases := []struct {
		raw     string
		escaped string
	}{
		{"Bill & Ben", "Bill &amp; Ben"},
		{"<title>", "&lt;title&gt;"},
		{`"quoted"`, "&#34;quoted&#34;"},
		{"plain text", "plain text"},
	}
	for _, tc := range cases {
		if got := urlutil.HTMLEscape(tc.raw); got != tc.escaped {
			t.Errorf("HTMLEscape(%q) = %q; want %q", tc.raw, got, tc.escaped)
		}
		if got := urlutil.HTMLUnescape(tc.escaped); got != tc.raw {
			t.Errorf("HTMLUnescape(%q) = %q; want %q", tc.escaped, got, tc.raw)
		}
	}
}

func TestStripQuery(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://example.com/file.mp3?id=1&foo=bar", "https://example.com/file.mp3"},
		{"https://example.com/file.mp3", "https://example.com/file.mp3"},
		{"https://example.com/file.mp3?", "https://example.com/file.mp3"},
	}
	for _, tc := range cases {
		got := urlutil.StripQuery(tc.in)
		if got != tc.want {
			t.Errorf("StripQuery(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hello World", "hello-world"},
		{"Bill & Ben's Secret Podcast", "bill-ben-s-secret-podcast"},
		{"001-my episode title", "001-my-episode-title"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"ALLCAPS", "allcaps"},
		{"Inaugral Business", "inaugral-business"},
		{"2006 01 03 - Tuesday", "2006-01-03-tuesday"},
	}
	for _, tc := range cases {
		got := urlutil.Slugify(tc.in)
		if got != tc.want {
			t.Errorf("Slugify(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestMimeTypeForExtension(t *testing.T) {
	cases := []struct{ ext, want string }{
		{".mp3", "audio/mpeg"},
		{".MP3", "audio/mpeg"},
		{".m4a", "audio/x-m4a"},
		{".M4A", "audio/x-m4a"},
		{"m4a", "audio/x-m4a"},
		{".m4b", "audio/x-m4b"},
		{".ogg", "audio/ogg"},
		{".wav", "audio/wav"},
		{".unknown", "audio/mpeg"},
		{"", "audio/mpeg"},
		{"mp3", "audio/mpeg"},
	}
	for _, tc := range cases {
		got := urlutil.MimeTypeForExtension(tc.ext)
		if got != tc.want {
			t.Errorf("MimeTypeForExtension(%q) = %q; want %q", tc.ext, got, tc.want)
		}
	}
}
