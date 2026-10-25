package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Stephan5/podcastinator/internal/dateutil"
	"github.com/Stephan5/podcastinator/internal/feedconfig"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/hosting"
)

const (
	csvFilename = "feed.csv"
	xmlFilename = "feed.xml"
)

// resolveFeedDir returns the absolute path to the feed directory: --feed-dir,
// or ./feed.
func resolveFeedDir() (string, error) {
	dir := feedDirFlag
	if dir == "" {
		dir = "feed"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		if feedDirFlag == "" {
			return "", fmt.Errorf("no feed directory at %s; run podcastinator init, or pass --feed-dir", abs)
		}
		return "", fmt.Errorf("feed directory %q not found", abs)
	}
	return abs, nil
}

// feed is a feed directory with a feed.json.
type feed struct {
	slug string
	dir  string
	cfg  *feedconfig.Config
}

func (f feed) csvPath() string { return filepath.Join(f.dir, csvFilename) }
func (f feed) xmlPath() string { return filepath.Join(f.dir, xmlFilename) }

// records reads the feed's CSV, which may not exist yet.
func (f feed) records() ([]feedcsv.Record, error) {
	return feedcsv.Read(f.csvPath(), f.cfg.DelimiterRune())
}

// loadFeed loads the feed named slug, which must have a feed.json.
func loadFeed(feedDirAbs, slug string) (feed, error) {
	dir := filepath.Join(feedDirAbs, slug)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return feed{}, fmt.Errorf("no feed %q: %s isn't a directory", slug, dir)
	}
	cfg, err := feedconfig.Load(dir)
	if err != nil {
		return feed{}, fmt.Errorf("feed %q: %w", slug, err)
	}
	if cfg == nil {
		return feed{}, fmt.Errorf("feed %q has no %s", slug, feedconfig.Filename)
	}
	return feed{slug: slug, dir: dir, cfg: cfg}, nil
}

// allFeeds returns every directory under the feed directory with a feed.json,
// sorted by slug.
func allFeeds(feedDirAbs string) ([]feed, error) {
	entries, err := os.ReadDir(feedDirAbs)
	if err != nil {
		return nil, fmt.Errorf("read feed dir %q: %w", feedDirAbs, err)
	}
	var feeds []feed
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(feedDirAbs, e.Name())
		cfg, err := feedconfig.Load(dir)
		if err != nil {
			return nil, fmt.Errorf("feed %q: %w", e.Name(), err)
		}
		if cfg == nil {
			continue
		}
		feeds = append(feeds, feed{slug: e.Name(), dir: dir, cfg: cfg})
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].slug < feeds[j].slug })
	return feeds, nil
}

// selectFeeds returns the named feeds, or every feed but the excluded ones
// when none are named.
func selectFeeds(feedDirAbs string, slugs []string, exclude []string) ([]feed, error) {
	if len(slugs) > 0 {
		if len(exclude) > 0 {
			return nil, fmt.Errorf("--exclude only applies when no feeds are named")
		}
		feeds := make([]feed, 0, len(slugs))
		for _, slug := range slugs {
			f, err := loadFeed(feedDirAbs, strings.TrimSuffix(slug, "/"))
			if err != nil {
				return nil, err
			}
			feeds = append(feeds, f)
		}
		return feeds, nil
	}

	all, err := allFeeds(feedDirAbs)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	for _, slug := range exclude {
		skip[slug] = true
	}
	var feeds []feed
	for _, f := range all {
		if !skip[f.slug] {
			feeds = append(feeds, f)
		}
	}
	return feeds, nil
}

// inputsHash is a digest of everything build reads to make a feed's
// feed.xml: its CSV, its feed.json and hosting.json. build stamps it into
// feed.xml and publish refuses a feed.xml whose stamp doesn't match, so a
// feed can't be published without rebuilding after its inputs change.
func inputsHash(feedDirAbs string, f feed) (string, error) {
	h := sha256.New()
	for _, path := range []string{
		f.csvPath(),
		filepath.Join(f.dir, feedconfig.Filename),
		filepath.Join(feedDirAbs, hosting.Filename),
	} {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		// Length-prefixed, so bytes can't move between files without changing the hash.
		binary.Write(h, binary.BigEndian, int64(len(data))) //nolint:errcheck
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// latestDate returns the newest parseable episode date, or the zero time.
func latestDate(records []feedcsv.Record) time.Time {
	var latest time.Time
	for _, rec := range records {
		if t, err := dateutil.ParseRFC2822(rec.Date); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

func printHeading(name string) {
	sep := strings.Repeat("-", 51)
	fmt.Printf("%s\n--- %s\n%s\n\n", sep, name, sep)
}
