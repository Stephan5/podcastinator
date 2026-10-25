package dateutil_test

import (
	"testing"

	"github.com/Stephan5/podcastinator/internal/dateutil"
)

func TestValidateRFC2822(t *testing.T) {
	valid := []string{
		"Sat, 29 Jun 2002 03:00:00 GMT",
		"Sun, 30 Jun 2002 19:07:00 +0100",
		"Mon, 01 Jul 2002 20:33:05 GMT",
		"Tue, 15 Nov 2016 03:00:00 GMT",
		"Thu, 04 Jul 2002 03:00:00 GMT",
		// Full month names as used in the bash test CSV inputs
		"Sun, 30 June 2002 19:07:00 +0100",
		"Mon, 01 July 2002 20:33:05 GMT",
		"Wed, 03 July 2002 18:50:00 GMT",
	}
	for _, d := range valid {
		if err := dateutil.ValidateRFC2822(d); err != nil {
			t.Errorf("expected valid: %q, got error: %v", d, err)
		}
	}

	invalid := []string{
		"",
		"not a date",
		"2024-01-15",
		"June 1, 2023",
		"01/06/2023",
	}
	for _, d := range invalid {
		if err := dateutil.ValidateRFC2822(d); err == nil {
			t.Errorf("expected invalid: %q, but got no error", d)
		}
	}
}

func TestParseRFC2822(t *testing.T) {
	t.Run("parses GMT", func(t *testing.T) {
		tm, err := dateutil.ParseRFC2822("Sat, 29 Jun 2002 03:00:00 GMT")
		if err != nil {
			t.Fatal(err)
		}
		if tm.Year() != 2002 || tm.Month() != 6 || tm.Day() != 29 {
			t.Errorf("unexpected time: %v", tm)
		}
	})

	t.Run("parses +0100 offset", func(t *testing.T) {
		tm, err := dateutil.ParseRFC2822("Sun, 30 Jun 2002 19:07:00 +0100")
		if err != nil {
			t.Fatal(err)
		}
		if tm.Year() != 2002 || tm.Month() != 6 || tm.Day() != 30 {
			t.Errorf("unexpected time: %v", tm)
		}
	})
}

func TestReformatDate(t *testing.T) {
	cases := []struct {
		input       string
		format      string
		wantContain string // substring that must appear in the result
	}{
		// From pubdate bash test: "Jun 1, 2023" → "Thu, 01 Jun 2023 03:00:00 GMT"
		{"Jun 1, 2023", "%b %d, %Y", "01 Jun 2023 03:00:00 GMT"},
		{"Jul 2, 2023", "%b %d, %Y", "02 Jul 2023 03:00:00 GMT"},
		// Period-normalisation: "Jul. 3, 2023" → same as "Jul 3, 2023"
		{"Jul. 3, 2023", "%b %d, %Y", "03 Jul 2023 03:00:00 GMT"},
		// Four-digit year formats
		{"2023 06 01", "%Y %m %d", "01 Jun 2023 03:00:00 GMT"},
	}

	for _, tc := range cases {
		got, err := dateutil.ReformatDate(tc.input, tc.format)
		if err != nil {
			t.Errorf("ReformatDate(%q, %q): %v", tc.input, tc.format, err)
			continue
		}
		if tc.wantContain != "" {
			found := false
			for i := 0; i <= len(got)-len(tc.wantContain); i++ {
				if got[i:i+len(tc.wantContain)] == tc.wantContain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ReformatDate(%q, %q) = %q; want it to contain %q",
					tc.input, tc.format, got, tc.wantContain)
			}
		}
	}
}

func TestReformatDateError(t *testing.T) {
	_, err := dateutil.ReformatDate("not-a-date", "%b %d, %Y")
	if err == nil {
		t.Error("expected error for unparseable date")
	}
}
