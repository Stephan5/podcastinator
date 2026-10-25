// Package dateutil provides date parsing and formatting utilities for podcast feeds.
package dateutil

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// normalizeMonths replaces full English month names with their RFC 2822
// abbreviations (e.g. "June" → "Jun", "July" → "Jul"). Go's mail.ParseDate
// only accepts the three-letter form; the bash tests include both.
func normalizeMonths(s string) string {
	replacer := strings.NewReplacer(
		"January", "Jan",
		"February", "Feb",
		"March", "Mar",
		"April", "Apr",
		// "May" already three letters
		"June", "Jun",
		"July", "Jul",
		"August", "Aug",
		"September", "Sep",
		"October", "Oct",
		"November", "Nov",
		"December", "Dec",
	)
	return replacer.Replace(s)
}

// ValidateRFC2822 returns an error if s is not a valid RFC 2822 date string.
func ValidateRFC2822(s string) error {
	// net/mail.ParseDate understands RFC 2822 dates
	_, err := mail.ParseDate(normalizeMonths(s))
	if err != nil {
		return fmt.Errorf("invalid RFC 2822 date %q: %w", s, err)
	}
	return nil
}

// ParseRFC2822 parses an RFC 2822 date string and returns a time.Time.
func ParseRFC2822(s string) (time.Time, error) {
	t, err := mail.ParseDate(normalizeMonths(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse RFC 2822 date %q: %w", s, err)
	}
	return t, nil
}

// FormatRFC2822 formats a time.Time as an RFC 2822 date string.
func FormatRFC2822(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

// strftimeToGoLayout converts a strftime-style format string to a Go time layout.
// Only the most common specifiers used in podcast feeds are supported.
func strftimeToGoLayout(strftime string) (string, error) {
	replacements := []struct{ from, to string }{
		{"%Y", "2006"},
		{"%m", "01"},
		{"%d", "2"}, // flexible: matches "1", "01", "02" etc.
		{"%e", "2"},
		{"%H", "15"},
		{"%M", "04"},
		{"%S", "05"},
		{"%b", "Jan"},
		{"%B", "January"},
		{"%a", "Mon"},
		{"%A", "Monday"},
		{"%Z", "MST"},
		{"%T", "15:04:05"},
		{"%p", "PM"},
		{"%I", "03"},
		{"%j", ""},
	}

	layout := strftime
	for _, r := range replacements {
		layout = strings.ReplaceAll(layout, r.from, r.to)
	}

	// Check for unsupported specifiers
	if strings.Contains(layout, "%") {
		return "", fmt.Errorf("unsupported strftime specifier in: %q", strftime)
	}

	return layout, nil
}

// ReformatDate parses dateStr using strftime inputFormat and returns the date
// formatted as RFC 2822 with time set to 03:00:00 GMT.
func ReformatDate(dateStr, strftimeInputFormat string) (string, error) {
	// Normalize: remove trailing periods after abbreviated month names (e.g. "Jan." → "Jan")
	dateStr = strings.ReplaceAll(dateStr, ".", "")

	goLayout, err := strftimeToGoLayout(strftimeInputFormat)
	if err != nil {
		return "", err
	}

	t, err := time.Parse(goLayout, dateStr)
	if err != nil {
		return "", fmt.Errorf("parse date %q with layout %q (from strftime %q): %w",
			dateStr, goLayout, strftimeInputFormat, err)
	}

	// Format as RFC 2822 with 03:00:00 GMT
	return t.Format("Mon, 02 Jan 2006") + " 03:00:00 GMT", nil
}
