package durationutil_test

import (
	"testing"

	"github.com/Stephan5/podcastinator/internal/durationutil"
)

func TestParse(t *testing.T) {
	valid := map[string]int{
		"0":        0,
		"95":       95,
		" 95 ":     95,
		"3723.4":   3723,
		"3723.5":   3724,
		"1:30":     90,
		"62:03":    3723,
		"1:02:03":  3723,
		"01:02:03": 3723,
		"100:00":   6000,
	}
	for in, want := range valid {
		got, err := durationutil.Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}

	for _, in := range []string{"", "abc", "-5", "1:60", "1:-1", "1:2:3:4", "1.5:00", "1::00", ":30", "NaN", "Inf"} {
		if got, err := durationutil.Parse(in); err == nil {
			t.Errorf("Parse(%q) = %d; want an error", in, got)
		}
	}
}
