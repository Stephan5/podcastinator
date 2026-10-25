// Package dateutil parses and formats episode dates.
package dateutil

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// normalizeMonths abbreviates full month names, as in "30 June 2002", which
// mail.ParseDate doesn't accept.
func normalizeMonths(s string) string {
	replacer := strings.NewReplacer(
		"January", "Jan",
		"February", "Feb",
		"March", "Mar",
		"April", "Apr",
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
	_, err := mail.ParseDate(normalizeMonths(s))
	if err != nil {
		return fmt.Errorf("invalid RFC 2822 date %q: %w", s, err)
	}
	return nil
}

// ParseRFC2822 parses an RFC 2822 date.
func ParseRFC2822(s string) (time.Time, error) {
	t, err := mail.ParseDate(normalizeMonths(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse RFC 2822 date %q: %w", s, err)
	}
	return t, nil
}

// FormatRFC2822 formats t as an RFC 2822 date in GMT.
func FormatRFC2822(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

// strftimeToGoLayout converts a strftime format to a Go time layout.
func strftimeToGoLayout(strftime string) (string, error) {
	replacements := []struct{ from, to string }{
		{"%Y", "2006"},
		{"%m", "01"},
		{"%d", "2"}, // matches "1" and "01"
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

	if strings.Contains(layout, "%") {
		return "", fmt.Errorf("unsupported strftime specifier in: %q", strftime)
	}

	return layout, nil
}

// ReformatDate parses dateStr with a strftime format and returns it as an
// RFC 2822 date at 03:00:00 GMT.
func ReformatDate(dateStr, strftimeInputFormat string) (string, error) {
	// Drop the period from abbreviations like "Jan."
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

	return t.Format("Mon, 02 Jan 2006") + " 03:00:00 GMT", nil
}
