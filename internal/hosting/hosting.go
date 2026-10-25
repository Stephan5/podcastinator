// Package hosting handles the repo-wide self-hosting configuration loaded from
// hosting.json at the root of the feed directory.
package hosting

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const Filename = "hosting.json"

const DefaultRegion = "eu-west-2"

// PublishedFiles are the per-feed files that `publish` uploads alongside the
// episode audio under <prefix>/<slug>/.
var PublishedFiles = []string{"feed.xml", "image.jpg"}

// Config describes where feeds and episodes are hosted.
type Config struct {
	Bucket string `json:"bucket"`
	Region string `json:"region,omitempty"`
	// Prefix is an optional S3 key prefix (no leading/trailing slash).
	Prefix string `json:"prefix,omitempty"`
	// PublicURL is the base URL that serves the bucket root, used for
	// everything in it: feeds, cover art and episode audio.
	// Defaults to the path-style S3 URL.
	PublicURL string `json:"public_url,omitempty"`
}

// Load reads hosting.json from feedDir. Returns nil, nil if the file does not exist.
func Load(feedDir string) (*Config, error) {
	path := filepath.Join(feedDir, Filename)
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
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%s: \"bucket\" is required", path)
	}
	cfg.Normalize()
	return &cfg, nil
}

// Normalize applies defaults and trims stray slashes.
func (c *Config) Normalize() {
	if c.Region == "" {
		c.Region = DefaultRegion
	}
	c.Prefix = strings.Trim(c.Prefix, "/")
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
}

// Key returns the S3 key for a path under the prefix,
// e.g. Key("example-show/feed.xml").
func (c *Config) Key(rel string) string {
	if c.Prefix == "" {
		return rel
	}
	return c.Prefix + "/" + rel
}

// KeyRoot returns the S3 key prefix for a feed, with a trailing slash.
func (c *Config) KeyRoot(slug string) string {
	return c.Key(slug + "/")
}

// S3URL is the path-style HTTPS URL of the bucket root.
func (c *Config) S3URL() string {
	return fmt.Sprintf("https://s3.%s.amazonaws.com/%s", c.Region, c.Bucket)
}

// BaseURL is the public URL of the bucket root.
func (c *Config) BaseURL() string {
	if c.PublicURL != "" {
		return c.PublicURL
	}
	return c.S3URL()
}

// URL returns the public URL for a path under the prefix,
// e.g. URL("example-show/feed.xml").
func (c *Config) URL(rel string) string {
	return c.BaseURL() + "/" + c.Key(rel)
}

// KeyForURL returns the key of the object in this bucket that rawURL points
// at, by either its public URL or its S3 URL, and false for a URL anywhere
// else. Episodes uploaded before audio was served from the public URL still
// have their S3 URL.
func (c *Config) KeyForURL(rawURL string) (string, bool) {
	u, _, _ := strings.Cut(rawURL, "?")
	for _, base := range []string{c.BaseURL(), c.S3URL()} {
		if rest, ok := strings.CutPrefix(u, base+"/"); ok && rest != "" {
			if key, err := url.PathUnescape(rest); err == nil {
				return key, true
			}
			return rest, true
		}
	}
	return "", false
}
