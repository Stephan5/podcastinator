// Package feedcsv reads and writes feed.csv, which has one episode per line
// and the columns title, description, date, url and guid, separated by the
// unit separator unless the feed says otherwise. Files from before the guid
// column still read, with empty GUIDs.
//
// Fields are split on the delimiter with no quoting, so a field can start
// with a double quote, which encoding/csv would take as a quoted field.
package feedcsv

import (
	"fmt"
	"os"
	"strings"
)

const DefaultDelimiter = '\x1f'

// Record is one episode.
type Record struct {
	Title       string
	Description string
	Date        string
	URL         string
	// GUID identifies the episode to podcast apps, so it must never change.
	// Empty means not yet assigned.
	GUID string
}

// Header is the CSV's header row.
var Header = []string{"title", "description", "date", "url", "guid"}

// Read returns the episodes in the CSV at path.
func Read(path string, delim rune) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	d := string(delim)
	var records []Record
	for i, line := range splitLines(string(data)) {
		if i == 0 {
			continue
		}
		fields := strings.Split(line, d)
		for len(fields) < len(Header) {
			fields = append(fields, "")
		}
		records = append(records, Record{
			Title:       fields[0],
			Description: fields[1],
			Date:        fields[2],
			URL:         fields[3],
			GUID:        fields[4],
		})
	}
	return records, nil
}

// Write writes the header and records to path.
func Write(path string, delim rune, records []Record) error {
	d := string(delim)
	var sb strings.Builder
	sb.WriteString(strings.Join(Header, d))
	sb.WriteByte('\n')
	for _, rec := range records {
		sb.WriteString(strings.Join([]string{rec.Title, rec.Description, rec.Date, rec.URL, rec.GUID}, d))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// WriteRaw writes header and rows to path as they are.
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

// ReadRaw returns the CSV's header and rows as they are, whatever its columns.
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

// splitLines splits s into lines, ignoring a trailing newline and any \r.
func splitLines(s string) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, "\r")
	}
	return lines
}
