package feedcsv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
)

func TestReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.csv")

	records := []feedcsv.Record{
		{Title: "Episode 1", Description: "First ep", Date: "Sat, 29 Jun 2002 03:00:00 GMT", URL: "https://example.com/1.mp3", GUID: "6f1c1e0a-3b5d-4c2e-9f7a-2d8b4e6a1c3f", Duration: "3723"},
		{Title: "Episode 2", Description: "", Date: "Sun, 30 Jun 2002 19:07:00 +0100", URL: "https://example.com/2.mp3"},
		{Title: "Ep. 3 – Special: \"Quoted\"", Description: "Desc", Date: "Mon, 01 Jul 2002 20:33:05 GMT", URL: "https://example.com/3.mp3"},
	}

	if err := feedcsv.Write(path, feedcsv.DefaultDelimiter, records); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := feedcsv.Read(path, feedcsv.DefaultDelimiter)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != len(records) {
		t.Fatalf("got %d records; want %d", len(got), len(records))
	}
	for i, r := range records {
		if got[i] != r {
			t.Errorf("record[%d]: got %+v; want %+v", i, got[i], r)
		}
	}
}

func TestReadWriteCustomDelimiter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.csv")

	records := []feedcsv.Record{
		{Title: "Episode 1", Description: "Desc 1", Date: "Jun 1, 2023", URL: "http://example.com/1"},
		{Title: "Episode 2", Description: "Desc 2", Date: "Jul 2, 2023", URL: "http://example.com/2"},
	}

	if err := feedcsv.Write(path, ';', records); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify raw file uses ; delimiter
	data, _ := os.ReadFile(path)
	content := string(data)
	if len(content) == 0 || content[0] == '\x1f' {
		t.Error("expected ; delimiter in raw file, got something else")
	}

	got, err := feedcsv.Read(path, ';')
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("got %d records; want %d", len(got), len(records))
	}
	for i, r := range records {
		if got[i] != r {
			t.Errorf("record[%d]: got %+v; want %+v", i, got[i], r)
		}
	}
}

func TestReadEmptyDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.csv")

	// Write manually with unit separator, empty description field
	content := "title\x1fdescription\x1fdate\x1furl\n" +
		"My Episode\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1fhttps://example.com/1.mp3\n"
	os.WriteFile(path, []byte(content), 0644)

	records, err := feedcsv.Read(path, feedcsv.DefaultDelimiter)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Title != "My Episode" {
		t.Errorf("unexpected title: %q", records[0].Title)
	}
	if records[0].Description != "" {
		t.Errorf("expected empty description, got %q", records[0].Description)
	}
}

// TestReadDescriptionStartingWithQuote is a regression test for the bug where
// encoding/csv mis-parsed rows when a description field began with a double-quote
// (e.g. `"Episode Name" is the final...`). encoding/csv entered quoted-field mode
// and consumed the following newline, merging two rows into one and shifting all
// column values for the subsequent row.
func TestReadDescriptionStartingWithQuote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.csv")

	// Row 1: description starts with a double-quote — the triggering pattern.
	// Row 2: a completely different episode that must NOT be merged into row 1.
	content := "title\x1fdescription\x1fdate\x1furl\n" +
		"LONER LEGENDS III\x1f\"LONER LEGENDS III\" is the final episode of Season Five\x1fTue, 17 May 2022 14:00:00 -0000\x1fhttps://example.com/50.mp3\n" +
		"BONUS EPISODE\x1fRon & Emil recently began a ritual\x1fTue, 14 Jun 2022 14:00:00 -0000\x1fhttps://example.com/51.mp3\n"
	os.WriteFile(path, []byte(content), 0644)

	records, err := feedcsv.Read(path, feedcsv.DefaultDelimiter)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d (rows may have been merged)", len(records))
	}

	// Row 1 must have the correct date — NOT the description of row 2.
	if records[0].Title != "LONER LEGENDS III" {
		t.Errorf("record[0].Title = %q; want %q", records[0].Title, "LONER LEGENDS III")
	}
	if records[0].Date != "Tue, 17 May 2022 14:00:00 -0000" {
		t.Errorf("record[0].Date = %q; want RFC 2822 date (got description instead?)", records[0].Date)
	}
	if records[0].URL != "https://example.com/50.mp3" {
		t.Errorf("record[0].URL = %q", records[0].URL)
	}

	// Row 2 must be fully intact and not shifted.
	if records[1].Title != "BONUS EPISODE" {
		t.Errorf("record[1].Title = %q; want %q", records[1].Title, "BONUS EPISODE")
	}
	if records[1].Date != "Tue, 14 Jun 2022 14:00:00 -0000" {
		t.Errorf("record[1].Date = %q", records[1].Date)
	}
}

// TestReadTitleStartingWithQuote checks that a title field containing double-quotes
// (e.g. BONUS EPISODE - "RON GETS FAN MAIL!") is read verbatim.
func TestReadTitleStartingWithQuote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.csv")

	os.WriteFile(path, []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"BONUS EPISODE - \"RON GETS FAN MAIL!\"\x1fSome description\x1fSat, 29 Jun 2002 03:00:00 GMT\x1fhttps://example.com/ep.mp3\n",
	), 0644)

	records, err := feedcsv.Read(path, feedcsv.DefaultDelimiter)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	want := `BONUS EPISODE - "RON GETS FAN MAIL!"`
	if records[0].Title != want {
		t.Errorf("Title = %q; want %q", records[0].Title, want)
	}
}

func TestReadRawWriteRaw(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.csv")

	header := []string{"title", "description", "date", "url"}
	rows := [][]string{
		{"Ep1", "Desc1", "Mon, 01 Jan 2024 03:00:00 GMT", "https://example.com/1.mp3"},
		{"Ep2", "Desc2", "Tue, 02 Jan 2024 03:00:00 GMT", "https://example.com/2.mp3"},
	}

	if err := feedcsv.WriteRaw(path, ';', header, rows); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	gotHeader, gotRows, err := feedcsv.ReadRaw(path, ';')
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	if len(gotHeader) != 4 || gotHeader[0] != "title" {
		t.Errorf("unexpected header: %v", gotHeader)
	}
	if len(gotRows) != 2 || gotRows[0][0] != "Ep1" {
		t.Errorf("unexpected rows: %v", gotRows)
	}
}

// TestReadWithoutGUIDColumn checks CSVs written before the guid and duration
// columns existed still read, with those fields empty.
func TestReadWithoutGUIDColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feed.csv")
	os.WriteFile(path, []byte("title\x1fdescription\x1fdate\x1furl\n"+
		"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1fhttps://example.com/1.mp3\n"), 0644)

	got, err := feedcsv.Read(path, feedcsv.DefaultDelimiter)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := feedcsv.Record{Title: "Ep1", Date: "Sat, 29 Jun 2002 03:00:00 GMT", URL: "https://example.com/1.mp3"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v; want [%+v]", got, want)
	}

	// Rewriting it adds the columns.
	if err := feedcsv.Write(path, feedcsv.DefaultDelimiter, got); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, _ := os.ReadFile(path)
	if want := "title\x1fdescription\x1fdate\x1furl\x1fguid\x1fduration\n"; string(data[:len(want)]) != want {
		t.Errorf("header = %q", data)
	}
}
