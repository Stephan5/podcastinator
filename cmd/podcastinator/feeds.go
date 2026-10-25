package main

import (
	"crypto/rand"
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
	"github.com/Stephan5/podcastinator/internal/feedrss"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/Stephan5/podcastinator/internal/urlutil"
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

// inputsHash is a digest of everything that decides a feed's feed.xml: its
// CSV, its feed.json, hosting.json and feedrss.FormatVersion. build stamps it
// into feed.xml, and publish refuses a feed.xml whose stamp doesn't match.
func inputsHash(feedDirAbs string, f feed) (string, error) {
	h := sha256.New()
	binary.Write(h, binary.BigEndian, int64(feedrss.FormatVersion)) //nolint:errcheck
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

// ensureGUIDs gives every episode in f's CSV a GUID, writing the CSV back if
// any were missing. An episode already in feed.xml keeps the GUID it was
// published with, which before the guid column was its URL, so podcast apps
// don't see it as new; any other episode gets a new UUID.
func ensureGUIDs(f feed) error {
	records, err := f.records()
	if err != nil {
		return err
	}

	var published map[string]string // encoded enclosure URL → GUID
	missing := 0
	for i := range records {
		if records[i].GUID != "" {
			continue
		}
		if published == nil {
			if published, err = publishedGUIDs(f); err != nil {
				return err
			}
		}
		encoded, _ := urlutil.Encode(records[i].URL)
		if guid, ok := published[encoded]; ok {
			records[i].GUID = guid
		} else {
			records[i].GUID = newUUID()
		}
		missing++
	}
	if missing == 0 {
		return nil
	}

	if err := feedcsv.Write(f.csvPath(), f.cfg.DelimiterRune(), records); err != nil {
		return err
	}
	fmt.Printf("Gave %d episode(s) a GUID in %s.\n\n", missing, csvFilename)
	return nil
}

// publishedGUIDs maps the episodes in f's existing feed.xml, by enclosure
// URL, to their GUIDs.
func publishedGUIDs(f feed) (map[string]string, error) {
	guids := map[string]string{}
	xmlFile, err := os.Open(f.xmlPath())
	if os.IsNotExist(err) {
		return guids, nil
	}
	if err != nil {
		return nil, err
	}
	defer xmlFile.Close()

	parsed, err := feedrss.Parse(xmlFile)
	if err != nil {
		return nil, fmt.Errorf("read published GUIDs from %s: %w", f.xmlPath(), err)
	}
	for _, rec := range parsed.Records {
		if rec.GUID != "" {
			guids[rec.URL] = rec.GUID
		}
	}
	return guids, nil
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	rand.Read(b[:]) //nolint:errcheck // never fails; see crypto/rand.Read
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
