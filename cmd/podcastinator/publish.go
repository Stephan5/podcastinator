package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Stephan5/podcastinator/internal/feedrss"
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
	"feed.xml":  "application/rss+xml; charset=utf-8",
	"image.jpg": "image/jpeg",
}

func newPublishCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:     "publish [feed...]",
		GroupID: "host",
		Short:   "Upload feeds and cover art to the bucket",
		Long: `Uploads each feed's feed.xml and image.jpg to <prefix>/<feed>/ in the bucket
in hosting.json, alongside the self-hosted episodes.

Refuses to upload anything if a feed's feed.csv, feed.json or hosting.json has
changed since its feed.xml was built; run build first. Finally checks that a
published feed.xml is reachable at its public URL, so a broken domain shows up
here rather than in podcast apps.

Files whose contents already match the bucket (by ETag) are skipped, so it is
safe to run repeatedly. Doesn't touch episode audio; see "audio upload".

Examples:
  podcastinator publish
  podcastinator publish example-show
  podcastinator publish --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			host, err := resolveHosting(feedDirAbs)
			if err != nil {
				return err
			}
			return runPublish(cmd.Context(), feedDirAbs, host, args, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be uploaded without uploading")
	return cmd
}

func runPublish(ctx context.Context, feedDirAbs string, host *hosting.Config, slugs []string, dryRun bool) error {
	feeds, err := loadLocalFeeds(feedDirAbs)
	if err != nil {
		return err
	}

	targets := feeds
	if len(slugs) > 0 {
		bySlug := map[string]feed{}
		for _, f := range feeds {
			bySlug[f.slug] = f
		}
		targets = nil
		for _, slug := range slugs {
			f, ok := bySlug[slug]
			if !ok {
				return fmt.Errorf("feed %q not found (needs feed.json and feed.xml in %s)",
					slug, filepath.Join(feedDirAbs, slug))
			}
			targets = append(targets, f)
		}
	}

	if err := checkBuilt(feedDirAbs, targets); err != nil {
		return err
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

	if dryRun || len(targets) == 0 {
		return nil
	}

	// The bucket accepted the uploads; make sure the public URL serves them.
	feedURL := host.URL(targets[0].slug + "/" + xmlFilename)
	if err := s3util.ValidateURL(feedURL); err != nil {
		return fmt.Errorf("uploaded, but %s isn't reachable (%v); fix hosting before rebuilding feeds for it, "+
			"or feeds will point subscribers at it", feedURL, err)
	}

	fmt.Println("Published.")
	return nil
}

// loadLocalFeeds returns every feed with a generated feed.xml, sorted by slug.
func loadLocalFeeds(feedDirAbs string) ([]feed, error) {
	all, err := allFeeds(feedDirAbs)
	if err != nil {
		return nil, err
	}

	var feeds []feed
	for _, f := range all {
		if _, err := os.Stat(f.xmlPath()); os.IsNotExist(err) {
			fmt.Printf("Skipping %q: no feed.xml; run podcastinator build %s first.\n", f.slug, f.slug)
			continue
		}
		feeds = append(feeds, f)
	}
	return feeds, nil
}

// checkBuilt fails unless every feed's feed.xml was built from its current
// inputs, by a version that builds them the same way, so a CSV edited without
// a rebuild can't be published stale.
func checkBuilt(feedDirAbs string, feeds []feed) error {
	var stale []string
	for _, f := range feeds {
		want, err := inputsHash(feedDirAbs, f)
		if err != nil {
			return fmt.Errorf("feed %q: %w", f.slug, err)
		}
		xmlBytes, err := os.ReadFile(f.xmlPath())
		if err != nil {
			return err
		}
		if feedrss.StampedInputs(xmlBytes) != want {
			stale = append(stale, f.slug)
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("feed.xml is out of date for %s; run podcastinator build %s first",
			strings.Join(stale, ", "), strings.Join(stale, " "))
	}
	return nil
}

type publisher struct {
	client *s3util.Client
	bucket string
	dryRun bool
}

// put uploads body to key unless the bucket already holds identical content.
func (p publisher) put(ctx context.Context, name, key string, body []byte, cacheControl string) error {
	localETag := etag(body)

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

// etag is the ETag S3 gives an object uploaded in one part: its MD5.
func etag(body []byte) string {
	sum := md5.Sum(body)
	return hex.EncodeToString(sum[:])
}
