// Package durationutil parses episode durations as podcast feeds write them.
package durationutil

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parse returns the whole number of seconds in s, which is seconds ("3723",
// "3723.4") or clock time ("62:03", "1:02:03"), the forms itunes:duration
// takes. Fractions of a second are rounded.
func Parse(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if s == "" || len(parts) > 3 {
		return 0, fmt.Errorf("invalid duration %q: want seconds, MM:SS or HH:MM:SS", s)
	}

	var total float64
	for i, part := range parts {
		last := i == len(parts)-1
		var v float64
		var err error
		if last {
			v, err = strconv.ParseFloat(part, 64)
		} else {
			var n int
			n, err = strconv.Atoi(part)
			v = float64(n)
		}
		// Only the first part may be 60 or more: 90 seconds is "90" or "1:30".
		if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) || (i > 0 && v >= 60) {
			return 0, fmt.Errorf("invalid duration %q: want seconds, MM:SS or HH:MM:SS", s)
		}
		total = total*60 + v
	}
	return int(math.Round(total)), nil
}
