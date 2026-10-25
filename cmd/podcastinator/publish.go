package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Stephan5/podcastinator/internal/dateutil"
	"github.com/Stephan5/podcastinator/internal/feedconfig"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/feedindex"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/Stephan5/podcastinator/internal/s3util"
	"github.com/spf13/cobra"
)

// Feeds are re-fetched often by podcast apps, so keep them fresh; cover art rarely changes.
const (
	feedCacheControl  = "public, max-age=300"
	imageCacheControl = "public, max-age=86400"
)

var publishedContentTypes = map[string]string{
	"feed.xml":             "application/rss+xml; charset=utf-8",
	"image.jpg":            "image/jpeg",
	feedindex.JSONFilename: "application/json; charset=utf-8",
	feedindex.OPMLFilename: "text/x-opml; charset=utf-8",
}

func newPublishCmd() *cobra.Command {
	var (
		feed    string
		feedDir string
		bucket  string
		region  string
		prefix  string
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Upload generated feeds, cover art and the feed index to the hosting bucket",
		Long: `Uploads each feed's feed.xml and image.jpg to <prefix>/<slug>/ in the
bucket configured in feed/hosting.json, alongside the self-hosted episodes.

Also writes index.json (every feed's title, URLs and episode count) and
podcasts.opml (to subscribe to every feed at once) to <prefix>/. These
always list every feed, even when publishing a single one.

Finally checks that index.json is reachable at its public URL, and fails if
not, so a broken domain is caught before any feed points subscribers at it.

Files whose contents already match the bucket (by ETag) are skipped, so it is
safe to run repeatedly. publish uploads the committed feed.xml as-is; run
regenerate first if the CSV has changed.

Examples:
  podcastinator publish
  podcastinator publish --feed example-show
  podcastinator publish --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir(feedDir)
			if err != nil {
				return err
			}
			host, err := resolveHosting(cmd, feedDirAbs, bucket, region, prefix)
			if err != nil {
				return err
			}
			return runPublish(cmd.Context(), feedDirAbs, host, feed, dryRun)
		},
	}

	cmd.Flags().StringVar(&feed, "feed", "", "Publish only this feed (directory name under feed/)")
	cmd.Flags().StringVar(&feedDir, "feed-dir", "", "Path to the feed directory (default: <repo>/feed)")
	addHostingFlags(cmd, &bucket, &region, &prefix)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be uploaded without uploading")

	return cmd
}

// localFeed is a feed directory that has been generated and can be published.
type localFeed struct {
	slug  string
	dir   string
	entry feedindex.Feed
}

func runPublish(ctx context.Context, feedDirAbs string, host *hosting.Config, singleFeed string, dryRun bool) error {
	feeds, err := loadLocalFeeds(feedDirAbs, host)
	if err != nil {
		return err
	}

	targets := feeds
	if singleFeed != "" {
		targets = nil
		for _, f := range feeds {
			if f.slug == singleFeed {
				targets = append(targets, f)
			}
		}
		if len(targets) == 0 {
			return fmt.Errorf("feed %q not found (needs feed.json and feed.xml in %s)",
				singleFeed, filepath.Join(feedDirAbs, singleFeed))
		}
	}

	fmt.Printf("Bucket:      %q\n", host.Bucket)
	fmt.Printf("Prefix:      %q\n", host.Prefix)
	fmt.Printf("Region:      %q\n", host.Region)
	fmt.Printf("Public URL:  %q\n", host.BaseURL())
	fmt.Printf("Dry run:     %v\n\n", dryRun)

	client, err := s3util.New(ctx, host.Region)
	if err != nil {
		return err
	}
	pub := publisher{client: client, bucket: host.Bucket, dryRun: dryRun}

	for _, f := range targets {
		fmt.Printf("%s:\n", f.slug)
		for _, file := range hosting.PublishedFiles {
			body, err := os.ReadFile(filepath.Join(f.dir, file))
			if os.IsNotExist(err) {
				fmt.Printf("  %-10s missing locally, skipping\n", file)
				continue
			}
			if err != nil {
				return err
			}
			if file == "feed.xml" {
				warnIfSelfLinkDiffers(body, f.entry.FeedURL)
			}
			cacheControl := feedCacheControl
			if file == "image.jpg" {
				cacheControl = imageCacheControl
			}
			if err := pub.put(ctx, file, host.Key(f.slug+"/"+file), body, cacheControl); err != nil {
				return fmt.Errorf("feed %q: %w", f.slug, err)
			}
		}
		fmt.Println()
	}

	entries := make([]feedindex.Feed, len(feeds))
	for i, f := range feeds {
		entries[i] = f.entry
	}
	indexJSON, err := feedindex.JSON(entries)
	if err != nil {
		return fmt.Errorf("build %s: %w", feedindex.JSONFilename, err)
	}
	opml, err := feedindex.OPML("Podcasts", entries)
	if err != nil {
		return fmt.Errorf("build %s: %w", feedindex.OPMLFilename, err)
	}

	fmt.Println("index:")
	if err := pub.put(ctx, feedindex.JSONFilename, host.Key(feedindex.JSONFilename), indexJSON, feedCacheControl); err != nil {
		return err
	}
	if err := pub.put(ctx, feedindex.OPMLFilename, host.Key(feedindex.OPMLFilename), opml, feedCacheControl); err != nil {
		return err
	}

	if dryRun {
		return nil
	}

	// The bucket accepted the uploads; make sure the public URL serves them.
	indexURL := host.URL(feedindex.JSONFilename)
	if err := s3util.ValidateURL(indexURL); err != nil {
		return fmt.Errorf("uploaded, but %s isn't reachable (%v); fix hosting before running regenerate, "+
			"or feeds will point subscribers at it", indexURL, err)
	}

	fmt.Printf("\nPublished. Subscribe to everything at once with %s\n", host.URL(feedindex.OPMLFilename))
	return nil
}

// loadLocalFeeds returns every feed directory with a feed.json and a generated
// feed.xml, sorted by slug, along with its index entry.
func loadLocalFeeds(feedDirAbs string, host *hosting.Config) ([]localFeed, error) {
	dirEntries, err := os.ReadDir(feedDirAbs)
	if err != nil {
		return nil, fmt.Errorf("read feed dir %q: %w", feedDirAbs, err)
	}

	var feeds []localFeed
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		dir := filepath.Join(feedDirAbs, slug)

		cfg, err := feedconfig.Load(dir)
		if err != nil {
			return nil, fmt.Errorf("feed %q: %w", slug, err)
		}
		if cfg == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "feed.xml")); os.IsNotExist(err) {
			fmt.Printf("Skipping %q — no feed.xml; run regenerate first.\n", slug)
			continue
		}

		entry := feedindex.Feed{
			Slug:        slug,
			Title:       cfg.Title,
			Description: cfg.Description,
			Author:      cfg.Author,
			FeedURL:     cfg.FeedURL,
			ImageURL:    cfg.ImageURL,
		}
		if entry.FeedURL == "" {
			entry.FeedURL = host.URL(slug + "/feed.xml")
		}
		if entry.ImageURL == "" {
			entry.ImageURL = host.URL(slug + "/image.jpg")
		}

		records, err := feedcsv.Read(filepath.Join(dir, "feed.csv"), cfg.DelimiterRune())
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("feed %q: read CSV: %w", slug, err)
		}
		entry.EpisodeCount = len(records)
		var latest time.Time
		for _, rec := range records {
			if t, err := dateutil.ParseRFC2822(rec.Date); err == nil && t.After(latest) {
				latest = t
			}
		}
		if !latest.IsZero() {
			entry.LatestEpisode = feedindex.FormatDate(latest)
		}

		feeds = append(feeds, localFeed{slug: slug, dir: dir, entry: entry})
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].slug < feeds[j].slug })
	return feeds, nil
}

// warnIfSelfLinkDiffers flags a feed.xml generated before hosting.json existed
// (or with a different public URL), whose self link points somewhere else.
func warnIfSelfLinkDiffers(feedXML []byte, wantURL string) {
	if !strings.Contains(string(feedXML), `<atom:link href="`+wantURL+`"`) {
		fmt.Printf("  Warning: feed.xml's self link isn't %s — run regenerate to point subscribers here\n", wantURL)
	}
}

type publisher struct {
	client *s3util.Client
	bucket string
	dryRun bool
}

// put uploads body to key unless the bucket already holds identical content.
func (p publisher) put(ctx context.Context, name, key string, body []byte, cacheControl string) error {
	sum := md5.Sum(body)
	localETag := hex.EncodeToString(sum[:])

	remoteETag, err := p.client.ETag(ctx, p.bucket, key)
	if err != nil {
		return err
	}
	dst := fmt.Sprintf("s3://%s/%s", p.bucket, key)
	switch {
	case remoteETag == localETag:
		fmt.Printf("  %-14s unchanged\n", name)
		return nil
	case p.dryRun:
		fmt.Printf("  %-14s would upload → %s\n", name, dst)
		return nil
	}
	if err := p.client.PutBytes(ctx, p.bucket, key, body, publishedContentTypes[name], cacheControl); err != nil {
		return err
	}
	fmt.Printf("  %-14s uploaded → %s\n", name, dst)
	return nil
}
