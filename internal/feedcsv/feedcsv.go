// Package feedcsv handles reading and writing podcast feed CSV files.
// The CSV format uses a configurable delimiter (default \x1F unit separator)
// with columns: title, description, date, url.
//
// No CSV quoting is used — fields are split/joined on the raw delimiter
// character, matching the behaviour of the original bash scripts. This avoids
// encoding/csv mis-parsing field values that begin with a double-quote (e.g.
// descriptions like `"Episode Title" is the...`) when the delimiter is a
// non-standard character such as the unit separator.
package feedcsv

import (
	"fmt"
	"os"
	"strings"
)

const DefaultDelimiter = '\x1f'

// Record represents one episode row in a feed CSV.
type Record struct {
	Title       string
	Description string
	Date        string
	URL         string
}

// Read reads a CSV feed file and returns the records (excluding the header).
// Fields are split on delim without any quoting interpretation.
func Read(path string, delim rune) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	d := string(delim)
	var records []Record
	for i, line := range splitLines(string(data)) {
		if i == 0 {
			continue // skip header
		}
		fields := strings.Split(line, d)
		for len(fields) < 4 {
			fields = append(fields, "")
		}
		records = append(records, Record{
			Title:       fields[0],
			Description: fields[1],
			Date:        fields[2],
			URL:         fields[3],
		})
	}
	return records, nil
}

// Write writes a header + records to path using the given delimiter.
// Fields are joined on delim without any quoting.
func Write(path string, delim rune, records []Record) error {
	d := string(delim)
	var sb strings.Builder
	sb.WriteString(strings.Join([]string{"title", "description", "date", "url"}, d))
	sb.WriteByte('\n')
	for _, rec := range records {
		sb.WriteString(strings.Join([]string{rec.Title, rec.Description, rec.Date, rec.URL}, d))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// WriteRaw writes a full CSV (including header) to path.
// Each line is joined by delim and terminated with \n.
func WriteRaw(path string, delim rune, header []string, rows [][]string) error {
	var sb strings.Builder
	writeRow := func(fields []string) {
		sb.WriteString(strings.Join(fields, string(delim)))
		sb.WriteByte('\n')
	}
	writeRow(header)
	for _, row := range rows {
		writeRow(row)
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// ReadRaw reads all rows including the header, returning raw string slices.
func ReadRaw(path string, delim rune) (header []string, rows [][]string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	d := string(delim)
	for i, line := range splitLines(string(data)) {
		fields := strings.Split(line, d)
		if i == 0 {
			header = fields
		} else {
			rows = append(rows, fields)
		}
	}
	return header, rows, nil
}

// splitLines splits s on \n, trimming a trailing empty element.
func splitLines(s string) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	// Filter out any residual empty lines (e.g. from \r\n line endings)
	var out []string
	for _, l := range lines {
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}
