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
		{"%y", "06"},
		{"%m", "1"}, // matches "1" and "01"
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

	if !strings.Contains(strftime, "%") {
		err := fmt.Errorf("%q isn't a strftime format: it has no %% specifiers", strftime)
		if hint := strftimeHint(strftime); hint != "" {
			err = fmt.Errorf("%w; try %q", err, hint)
		}
		return "", err
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

// CheckFormat returns an error if strftime isn't a format ReformatDate can use.
func CheckFormat(strftime string) error {
	_, err := strftimeToGoLayout(strftime)
	return err
}

// strftimeHint translates a pattern like "DD/MM/yyyy" or "dd/mm/yy", as
// spreadsheets and other languages write them, into strftime. Returns "" if
// any letter in it isn't part of a recognised token.
func strftimeHint(pattern string) string {
	hasHour := strings.ContainsAny(pattern, "Hh")
	var b strings.Builder
	for i := 0; i < len(pattern); {
		c := pattern[i]
		j := i
		for j < len(pattern) && pattern[j] == c {
			j++
		}
		n := j - i
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			b.WriteString(pattern[i:j])
			i = j
			continue
		}
		spec := patternSpec(c, n, hasHour)
		if spec == "" {
			return ""
		}
		b.WriteString(spec)
		i = j
	}
	if b.String() == pattern {
		return "" // no letters, so nothing to translate
	}
	return b.String()
}

// patternSpec returns the strftime specifier for n repeats of c, or "".
func patternSpec(c byte, n int, hasHour bool) string {
	switch {
	case (c == 'Y' || c == 'y') && n == 4:
		return "%Y"
	case (c == 'Y' || c == 'y') && n == 2:
		return "%y"
	case c == 'M' && n <= 2:
		return "%m"
	case c == 'M' && n == 3:
		return "%b"
	case c == 'M' && n == 4:
		return "%B"
	case (c == 'D' || c == 'd') && n <= 2:
		return "%d"
	case c == 'd' && n == 3:
		return "%a"
	case c == 'd' && n == 4:
		return "%A"
	case c == 'H' && n <= 2:
		return "%H"
	case c == 'h' && n <= 2:
		return "%I"
	case c == 'm' && n <= 2 && hasHour: // minutes next to an hour
		return "%M"
	case c == 'm' && n <= 2: // otherwise the month, as in dd/mm/yyyy
		return "%m"
	case c == 's' && n <= 2:
		return "%S"
	}
	return ""
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
