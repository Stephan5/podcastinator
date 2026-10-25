package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stephan5/podcastinator/internal/feedrss"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/spf13/cobra"
)

func newBuildCmd() *cobra.Command {
	var exclude []string
	var concurrency int

	cmd := &cobra.Command{
		Use:     "build [feed...]",
		GroupID: "generate",
		Short:   "Build feed.xml from each feed's feed.csv and feed.json",
		Long: `Builds <feed>/feed.xml from its feed.csv (one row per episode) and feed.json
(the show's title, description, author and website).

Every episode gets a GUID, which podcast apps identify it by, the first time
it's built: a new UUID, or the GUID it was already published with. They're
stored in feed.csv's guid column and never change, so audio can move without
apps seeing its episodes as new.

The feed and image URLs come from "feed" and "image" in feed.json, or default
to the feed's location in the bucket from hosting.json. One of them has to
supply both.

Sends a HEAD request for every episode to fill in its length, so the audio has
to be reachable. --concurrency sets how many run at once; lower it if a host
rate-limits you. Leaves feed.xml alone if nothing in it has changed.

feed.xml is stamped with a hash of feed.csv, feed.json and hosting.json, so
publish can refuse a feed that wasn't rebuilt after they changed.

Examples:
  podcastinator build
  podcastinator build example-show
  podcastinator build --exclude some-feed,another-feed
  podcastinator build --concurrency 4`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if concurrency < 1 {
				return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
			}
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			feeds, err := selectFeeds(feedDirAbs, args, exclude)
			if err != nil {
				return err
			}
			for _, f := range feeds {
				if _, err := os.Stat(f.csvPath()); os.IsNotExist(err) {
					if len(args) > 0 {
						return fmt.Errorf("feed %q has no %s", f.slug, csvFilename)
					}
					fmt.Printf("Skipping %q — no %s.\n\n", f.slug, csvFilename)
					continue
				}
				printHeading("Building: " + f.slug)
				if err := buildFeed(feedDirAbs, f, concurrency); err != nil {
					return fmt.Errorf("feed %q: %w", f.slug, err)
				}
				fmt.Println()
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Feeds to skip when building every feed")
	cmd.Flags().IntVar(&concurrency, "concurrency", feedrss.DefaultConcurrency, "HEAD requests to make at once")
	return cmd
}

// buildFeed writes f's feed.xml.
func buildFeed(feedDirAbs string, f feed, concurrency int) error {
	feedURL, imageURL := f.cfg.FeedURL, f.cfg.ImageURL

	// Self-hosted feeds default to their URLs in the bucket.
	host, err := hosting.Load(feedDirAbs)
	if err != nil {
		return err
	}
	if host != nil {
		if feedURL == "" {
			feedURL = host.URL(f.slug + "/" + xmlFilename)
		}
		if imageURL == "" {
			imageURL = host.URL(f.slug + "/image.jpg")
		}
	}
	if feedURL == "" || imageURL == "" {
		return fmt.Errorf("no feed URL or image URL: add %s to %s, or set \"feed\" and \"image\" in feed.json",
			hosting.Filename, feedDirAbs)
	}

	if err := ensureGUIDs(f); err != nil {
		return err
	}
	hash, err := inputsHash(feedDirAbs, f)
	if err != nil {
		return err
	}

	opts := feedrss.FeedOptions{
		Title:       f.cfg.Title,
		Description: f.cfg.Description,
		Author:      f.cfg.Author,
		WebsiteURL:  f.cfg.WebsiteURL,
		ImageURL:    imageURL,
		FeedURL:     feedURL,
		RepoDir:     f.slug,
		Type:        f.cfg.Type,
		InputsHash:  hash,
		Concurrency: concurrency,
	}
	opts.SetDefaults()

	fmt.Printf("Title:       %q\n", opts.Title)
	fmt.Printf("Description: %q\n", opts.Description)
	fmt.Printf("Author:      %q\n", opts.Author)
	fmt.Printf("Website:     %q\n", opts.WebsiteURL)
	fmt.Printf("Image URL:   %q\n", opts.ImageURL)
	fmt.Printf("Feed URL:    %q\n\n", opts.FeedURL)

	records, err := f.records()
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	xmlBytes, err := feedrss.Build(opts, records, os.Stdout)
	if err != nil {
		return fmt.Errorf("build RSS: %w", err)
	}

	outputFile := f.xmlPath()
	if existing, err := os.ReadFile(outputFile); err == nil {
		if feedrss.HashContent(existing) == feedrss.HashContent(xmlBytes) {
			fmt.Println("No changes detected. Skipping update.")
			return nil
		}
	}
	if err := os.WriteFile(outputFile, xmlBytes, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputFile, err)
	}

	rel, _ := filepath.Rel(feedDirAbs, outputFile)
	fmt.Printf("Wrote %s (%d episodes).\n", rel, len(records))
	fmt.Printf("Once published, check it at https://validator.livewire.io with %s\n", opts.FeedURL)
	return nil
}
