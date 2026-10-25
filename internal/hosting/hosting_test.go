package hosting_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Stephan5/podcastinator/internal/hosting"
)

func TestLoadMissingFile(t *testing.T) {
	cfg, err := hosting.Load(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for missing file, got: %+v", cfg)
	}
}

func TestLoadRequiresBucket(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, hosting.Filename), []byte(`{"region":"us-east-1"}`), 0644)

	if _, err := hosting.Load(dir); err == nil {
		t.Fatal("expected error when bucket is missing")
	}
}

func TestLoadDefaultsAndNormalizes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, hosting.Filename),
		[]byte(`{"bucket":"my-bucket","prefix":"/rss/","public_url":"https://podcasts.example.com/"}`), 0644)

	cfg, err := hosting.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Region != hosting.DefaultRegion {
		t.Errorf("Region = %q; want %q", cfg.Region, hosting.DefaultRegion)
	}
	if cfg.Prefix != "rss" {
		t.Errorf("Prefix = %q; want %q", cfg.Prefix, "rss")
	}
	if cfg.PublicURL != "https://podcasts.example.com" {
		t.Errorf("PublicURL = %q; want trailing slash trimmed", cfg.PublicURL)
	}
}

func TestURLs(t *testing.T) {
	tests := []struct {
		name     string
		cfg      hosting.Config
		keyRoot  string
		feedURL  string
		indexURL string
	}{
		{
			name:     "s3 default",
			cfg:      hosting.Config{Bucket: "my-bucket", Region: "eu-west-2"},
			keyRoot:  "show/",
			feedURL:  "https://s3.eu-west-2.amazonaws.com/my-bucket/show/feed.xml",
			indexURL: "https://s3.eu-west-2.amazonaws.com/my-bucket/index.json",
		},
		{
			name:     "public url",
			cfg:      hosting.Config{Bucket: "my-bucket", Region: "eu-west-2", PublicURL: "https://podcasts.example.com"},
			keyRoot:  "show/",
			feedURL:  "https://podcasts.example.com/show/feed.xml",
			indexURL: "https://podcasts.example.com/index.json",
		},
		{
			name:     "prefix and public url",
			cfg:      hosting.Config{Bucket: "my-bucket", Region: "eu-west-2", Prefix: "rss", PublicURL: "https://podcasts.example.com"},
			keyRoot:  "rss/show/",
			feedURL:  "https://podcasts.example.com/rss/show/feed.xml",
			indexURL: "https://podcasts.example.com/rss/index.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.KeyRoot("show"); got != tt.keyRoot {
				t.Errorf("KeyRoot = %q; want %q", got, tt.keyRoot)
			}
			if got := tt.cfg.URL("show/feed.xml"); got != tt.feedURL {
				t.Errorf("URL = %q; want %q", got, tt.feedURL)
			}
			if got := tt.cfg.URL("index.json"); got != tt.indexURL {
				t.Errorf("URL(index) = %q; want %q", got, tt.indexURL)
			}
		})
	}
}

func TestKeyForURL(t *testing.T) {
	cdn := &hosting.Config{Bucket: "my-bucket", Region: "eu-west-2", PublicURL: "https://podcasts.example.com"}
	plain := &hosting.Config{Bucket: "my-bucket", Region: "eu-west-2"}
	cases := []struct {
		cfg     *hosting.Config
		url     string
		wantKey string
		wantOK  bool
	}{
		{cdn, "https://podcasts.example.com/show/001-ep.mp3", "show/001-ep.mp3", true},
		{cdn, "https://s3.eu-west-2.amazonaws.com/my-bucket/show/001-ep.mp3", "show/001-ep.mp3", true},
		{cdn, "https://podcasts.example.com/show/my%20ep.mp3?x=1", "show/my ep.mp3", true},
		{cdn, "https://s3.eu-west-2.amazonaws.com/other-bucket/show/001-ep.mp3", "", false},
		{cdn, "https://podcasts.example.com.evil.com/show/001-ep.mp3", "", false},
		{cdn, "https://cdn.example.com/show/001-ep.mp3", "", false},
		{cdn, "https://podcasts.example.com/", "", false},
		{plain, "https://s3.eu-west-2.amazonaws.com/my-bucket/show/001-ep.mp3", "show/001-ep.mp3", true},
		{plain, "https://podcasts.example.com/show/001-ep.mp3", "", false},
	}
	for _, c := range cases {
		key, ok := c.cfg.KeyForURL(c.url)
		if key != c.wantKey || ok != c.wantOK {
			t.Errorf("KeyForURL(%q) = %q, %v; want %q, %v", c.url, key, ok, c.wantKey, c.wantOK)
		}
	}
}
