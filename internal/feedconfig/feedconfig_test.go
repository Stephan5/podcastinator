package feedconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Stephan5/podcastinator/internal/feedconfig"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
)

func TestLoadMissingFile(t *testing.T) {
	cfg, err := feedconfig.Load(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for missing file, got: %+v", cfg)
	}
}

func TestLoadValidFile(t *testing.T) {
	dir := t.TempDir()
	content := `{"title":"Test Show","description":"A test","author":"Test Author"}`
	os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(content), 0644)

	cfg, err := feedconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Title != "Test Show" {
		t.Errorf("Title = %q; want %q", cfg.Title, "Test Show")
	}
	if cfg.Description != "A test" {
		t.Errorf("Description = %q; want %q", cfg.Description, "A test")
	}
	if cfg.Author != "Test Author" {
		t.Errorf("Author = %q; want %q", cfg.Author, "Test Author")
	}
}

func TestLoadMissingTitle(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(`{"description":"no title here"}`), 0644)

	_, err := feedconfig.Load(dir)
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(`{bad json`), 0644)

	_, err := feedconfig.Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := &feedconfig.Config{
		Title:       "Two Friends' Secret Podcast",
		Description: "Grab onto this fast moving train",
		Author:      "Jane Doe & John Smith",
		Source:      "https://example.com/feed.rss?a=1&b=2",
	}

	if err := feedconfig.Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Written for people to read and edit, so & isn't escaped to \u0026.
	data, _ := os.ReadFile(filepath.Join(dir, feedconfig.Filename))
	if !strings.Contains(string(data), `"author": "Jane Doe & John Smith"`) {
		t.Errorf("feed.json:\n%s", data)
	}

	loaded, err := feedconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if loaded.Title != cfg.Title {
		t.Errorf("Title = %q; want %q", loaded.Title, cfg.Title)
	}
	if loaded.Author != cfg.Author {
		t.Errorf("Author = %q; want %q", loaded.Author, cfg.Author)
	}
	if loaded.Source != cfg.Source {
		t.Errorf("Source = %q; want %q", loaded.Source, cfg.Source)
	}
}

func TestDelimiterRune(t *testing.T) {
	cases := []struct {
		delimiter string
		want      rune
	}{
		{"", feedcsv.DefaultDelimiter},
		{";", ';'},
		{"\t", '\t'},
		{"\u001f", '\x1f'},
	}
	for _, tc := range cases {
		cfg := &feedconfig.Config{Title: "x", Delimiter: tc.delimiter}
		if got := cfg.DelimiterRune(); got != tc.want {
			t.Errorf("DelimiterRune(%q) = %q; want %q", tc.delimiter, got, tc.want)
		}
	}
}

func TestLoadWithCustomDelimiter(t *testing.T) {
	dir := t.TempDir()
	content := `{"title":"Show","delimiter":";"}`
	os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(content), 0644)

	cfg, err := feedconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DelimiterRune() != ';' {
		t.Errorf("DelimiterRune = %q; want ';'", cfg.DelimiterRune())
	}
}

func TestLoadURLs(t *testing.T) {
	dir := t.TempDir()
	content := `{"title":"Test Show","website":"https://example.com/show","image":"https://example.com/show.jpg","feed":"https://example.com/show.xml"}`
	os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(content), 0644)

	cfg, err := feedconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebsiteURL != "https://example.com/show" {
		t.Errorf("WebsiteURL = %q; want %q", cfg.WebsiteURL, "https://example.com/show")
	}
	if cfg.ImageURL != "https://example.com/show.jpg" {
		t.Errorf("ImageURL = %q; want %q", cfg.ImageURL, "https://example.com/show.jpg")
	}
	if cfg.FeedURL != "https://example.com/show.xml" {
		t.Errorf("FeedURL = %q; want %q", cfg.FeedURL, "https://example.com/show.xml")
	}
}

func TestLoadType(t *testing.T) {
	for _, typ := range []string{"", "episodic", "serial"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(`{"title":"Show","type":"`+typ+`"}`), 0644)
		cfg, err := feedconfig.Load(dir)
		if err != nil {
			t.Errorf("type %q: %v", typ, err)
		} else if cfg.Type != typ {
			t.Errorf("Type = %q; want %q", cfg.Type, typ)
		}
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, feedconfig.Filename), []byte(`{"title":"Show","type":"Serial"}`), 0644)
	if _, err := feedconfig.Load(dir); err == nil || !strings.Contains(err.Error(), `"type" must be "episodic" or "serial"`) {
		t.Errorf("expected an invalid type to be rejected, got %v", err)
	}
}
