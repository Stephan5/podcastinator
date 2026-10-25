// Package feedconfig handles per-feed configuration loaded from feed.json.
package feedconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
)

const Filename = "feed.json"

// Config holds the podcast-level metadata for a feed directory.
// Omitted fields use their defaults.
type Config struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	// Delimiter is a single character. Defaults to \x1F if empty.
	// Use the JSON unicode escape \u001f to represent the unit separator.
	Delimiter  string `json:"delimiter,omitempty"`
	WebsiteURL string `json:"website,omitempty"`
	ImageURL   string `json:"image,omitempty"`
	FeedURL    string `json:"feed,omitempty"`
	// Source is the RSS feed the episodes were imported from, which update
	// re-fetches for new episodes.
	Source string `json:"source,omitempty"`
}

// DelimiterRune returns the configured delimiter as a rune,
// falling back to feedcsv.DefaultDelimiter if not set.
func (c *Config) DelimiterRune() rune {
	if c.Delimiter == "" {
		return feedcsv.DefaultDelimiter
	}
	return []rune(c.Delimiter)[0]
}

// Load reads feed.json from dir. Returns nil, nil if the file does not exist.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, Filename)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Title == "" {
		return nil, fmt.Errorf("%s: \"title\" is required", path)
	}
	return &cfg, nil
}

// Save writes cfg to feed.json in dir.
func Save(dir string, cfg *Config) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // titles like "Bill & Ben" stay readable
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, Filename), buf.Bytes(), 0644)
}
