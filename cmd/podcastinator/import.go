package main

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Stephan5/podcastinator/internal/dateutil"
	"github.com/Stephan5/podcastinator/internal/feedconfig"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/feedrss"
	"github.com/Stephan5/podcastinator/internal/urlutil"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var (
		slug  string
		force bool
	)

	cmd := &cobra.Command{
		Use:     "import <url|file>",
		GroupID: "import",
		Short:   "Create a feed from an existing podcast's RSS feed",
		Long: `Fetches a podcast's RSS feed (a URL or a local file) and creates a feed
directory from it:

  <feed>/feed.csv    every episode, oldest first, with durations where the
                     source has them
  <feed>/feed.json   the show's title, description, author and website, and
                     the source, which update fetches new episodes from
  <feed>/image.jpg   the cover art (PNGs are converted to JPEG)

The feed is named after the show's title unless --slug says otherwise.

Every episode gets a new UUID as its GUID. --force overwrites an existing
feed, giving every episode a new GUID, so subscribers would see them all
again.

Examples:
  podcastinator import https://example.com/podcast.rss
  podcastinator import ~/Downloads/show.xml --slug my-show`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			return runImport(feedDirAbs, args[0], slug, force)
		},
	}

	cmd.Flags().StringVar(&slug, "slug", "", "Name of the feed directory (default: from the show's title)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing feed")
	return cmd
}

func runImport(feedDirAbs, source, slug string, force bool) error {
	source = absSource(source)
	parsed, err := fetchFeed(source)
	if err != nil {
		return err
	}

	if slug == "" {
		slug = urlutil.Slugify(parsed.Title)
		if slug == "" {
			return fmt.Errorf("can't make a feed name from the title %q; pass --slug", parsed.Title)
		}
	}
	dir := filepath.Join(feedDirAbs, slug)
	if !force {
		for _, name := range []string{feedconfig.Filename, csvFilename} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return fmt.Errorf("feed %q already exists; pass --force to overwrite it, or --slug to import under another name", slug)
			}
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	cfg := &feedconfig.Config{
		Title:       parsed.Title,
		Description: parsed.Description,
		Author:      parsed.Author,
		WebsiteURL:  parsed.Link,
		Source:      source,
	}
	if cfg.Title == "" {
		cfg.Title = slug
	}
	records := cleanRecords(parsed.Records, cfg.DelimiterRune())

	if err := feedcsv.Write(filepath.Join(dir, csvFilename), cfg.DelimiterRune(), records); err != nil {
		return err
	}
	if err := feedconfig.Save(dir, cfg); err != nil {
		return err
	}

	fmt.Printf("Title:    %q\n", cfg.Title)
	fmt.Printf("Author:   %q\n", cfg.Author)
	fmt.Printf("Website:  %q\n", cfg.WebsiteURL)
	fmt.Printf("Source:   %q\n", cfg.Source)
	fmt.Printf("Episodes: %d\n\n", len(records))

	switch {
	case parsed.ImageURL == "":
		fmt.Print("Warning: the source has no cover art; add image.jpg yourself.\n\n")
	default:
		if err := saveCoverArt(parsed.ImageURL, filepath.Join(dir, "image.jpg")); err != nil {
			fmt.Printf("Warning: couldn't save the cover art from %s: %v\n\n", parsed.ImageURL, err)
		}
	}

	fmt.Printf("Imported %s. Next:\n", dir)
	fmt.Printf("  podcastinator audio upload %s   # if hosting the audio\n", slug)
	fmt.Printf("  podcastinator build %s\n", slug)
	fmt.Printf("  podcastinator publish %s\n", slug)
	return nil
}

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update [feed...]",
		GroupID: "import",
		Short:   "Add new episodes from each feed's source",
		Long: `Re-fetches the "source" feed in each feed's feed.json and appends the
episodes dated after the newest one already in feed.csv. Feeds without a
source are skipped.

Episodes are matched by date rather than URL, because once the audio is
self-hosted the URLs no longer match the source's. Run "audio upload" and
build afterwards.

Examples:
  podcastinator update
  podcastinator update example-show`,
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			feeds, err := selectFeeds(feedDirAbs, args, nil)
			if err != nil {
				return err
			}
			for _, f := range feeds {
				if f.cfg.Source == "" {
					if len(args) > 0 {
						return fmt.Errorf("feed %q has no \"source\" in feed.json", f.slug)
					}
					continue
				}
				if err := updateFeed(f); err != nil {
					return fmt.Errorf("feed %q: %w", f.slug, err)
				}
			}
			return nil
		},
	}
	return cmd
}

func updateFeed(f feed) error {
	existing, err := f.records()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	parsed, err := fetchFeed(f.cfg.Source)
	if err != nil {
		return err
	}

	latest := latestDate(existing)
	var added []feedcsv.Record
	for _, rec := range cleanRecords(parsed.Records, f.cfg.DelimiterRune()) {
		if t, err := dateutil.ParseRFC2822(rec.Date); err == nil && t.After(latest) {
			added = append(added, rec)
		}
	}
	if len(added) == 0 {
		fmt.Printf("%s: no new episodes\n", f.slug)
		return nil
	}

	if err := feedcsv.Write(f.csvPath(), f.cfg.DelimiterRune(), append(existing, added...)); err != nil {
		return err
	}
	fmt.Printf("%s: %d new episode(s)\n", f.slug, len(added))
	for _, rec := range added {
		fmt.Printf("  + %s  %s\n", rec.Date, rec.Title)
	}
	return nil
}

// absSource makes a local source path absolute, so feed.json's source still
// works from another directory. URLs are returned unchanged.
func absSource(source string) string {
	if isHTTPURL(source) {
		return source
	}
	if abs, err := filepath.Abs(source); err == nil {
		return abs
	}
	return source
}

// fetchFeed fetches and parses an RSS feed from a URL or a local file.
func fetchFeed(source string) (*feedrss.ParsedFeed, error) {
	var (
		data []byte
		err  error
	)
	if isHTTPURL(source) {
		data, err = httpGet(source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", source, err)
	}
	parsed, err := feedrss.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return parsed, nil
}

var httpClient = &http.Client{Timeout: 2 * time.Minute}

// httpGet returns the body of a successful GET. Some podcast hosts turn away
// Go's default user agent, so it names itself.
func httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "podcastinator/"+version+" (+https://github.com/Stephan5/podcastinator)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// cleanRecords makes parsed episodes safe to store in a CSV, which has no
// quoting: runs of whitespace, newlines included, and the delimiter become
// single spaces. Descriptions are stored
// HTML-escaped, as build writes them into the feed verbatim. Each episode
// gets a new UUID for its GUID, rather than the source's. Episodes with no
// audio URL are dropped.
func cleanRecords(records []feedcsv.Record, delim rune) []feedcsv.Record {
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	clean := func(s string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(s, string(delim), " ")), " ")
	}

	out := make([]feedcsv.Record, 0, len(records))
	for _, rec := range records {
		rec.URL = clean(rec.URL)
		if rec.URL == "" {
			fmt.Printf("Warning: skipping %q, which has no audio URL\n", rec.Title)
			continue
		}
		out = append(out, feedcsv.Record{
			Title:       clean(rec.Title),
			Description: escape.Replace(clean(rec.Description)),
			Date:        clean(rec.Date),
			URL:         rec.URL,
			GUID:        newUUID(),
			Duration:    rec.Duration,
		})
	}
	return out
}

// saveCoverArt downloads the cover art to dst as a JPEG, converting a PNG.
func saveCoverArt(url, dst string) error {
	data, err := httpGet(url)
	if err != nil {
		return err
	}
	switch ct := http.DetectContentType(data); ct {
	case "image/jpeg":
	case "image/png":
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("decode PNG: %w", err)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return fmt.Errorf("convert PNG to JPEG: %w", err)
		}
		data = buf.Bytes()
	default:
		return fmt.Errorf("it's %s, and only JPEG and PNG are supported; convert it to image.jpg yourself", ct)
	}
	return os.WriteFile(dst, data, 0644)
}
