package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stephan5/podcastinator/internal/feedconfig"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/feedrss"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newCSV2RSSCmd() *cobra.Command {
	var (
		title       string
		description string
		author      string
		websiteURL  string
		imageURL    string
		feedURL     string
		delimiter   string
	)

	cmd := &cobra.Command{
		Use:   "csv2rss <input.csv>",
		Short: "Convert a CSV feed to an RSS XML podcast feed",
		Long: `Reads a CSV file (columns: title, description, date, url) and generates
a podcast RSS XML feed file alongside the CSV.

If a feed.json config file exists in the same directory as the CSV, its values
are used as defaults. Any flags provided on the command line take precedence.

The feed and image URLs come from the flags, feed.json, or hosting.json in the
parent feed directory (their location in the bucket), in that order. One of
them has to supply both.

The date column must be in RFC 2822 format (e.g. "Mon, 01 Jan 2024 03:00:00 GMT").
Default CSV delimiter is the ASCII unit separator (0x1F).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Collect only the flags that were explicitly set by the user.
			changed := map[string]bool{}
			cmd.Flags().Visit(func(f *pflag.Flag) { changed[f.Name] = true })

			return runCSV2RSS(args[0], title, description, author, websiteURL, imageURL, feedURL,
				runeFromString(delimiter), changed)
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Podcast title (overrides feed.json)")
	cmd.Flags().StringVar(&description, "description", "", "Podcast description (overrides feed.json)")
	cmd.Flags().StringVar(&author, "author", "", "Podcast author (overrides feed.json)")
	cmd.Flags().StringVar(&websiteURL, "website", "", "Podcast website URL (default: this tool's GitHub repo)")
	cmd.Flags().StringVar(&imageURL, "image-url", "", "Podcast cover image URL (default: image.jpg in the bucket, from hosting.json)")
	cmd.Flags().StringVar(&feedURL, "feed-url", "", "Podcast feed self-link URL (default: feed.xml in the bucket, from hosting.json)")
	cmd.Flags().StringVar(&delimiter, "delimiter", string(rune(feedcsv.DefaultDelimiter)), "CSV field delimiter (overrides feed.json)")

	return cmd
}

// runCSV2RSS is the shared implementation used by both the csv2rss command and regenerate.
// changed is the set of flag names explicitly provided; values from feed.json fill any gaps.
func runCSV2RSS(inputFile, title, description, author, websiteURL, imageURL, feedURL string,
	delim rune, changed map[string]bool) error {

	inputAbs, err := filepath.Abs(inputFile)
	if err != nil {
		return fmt.Errorf("resolve input path: %w", err)
	}
	if _, err := os.Stat(inputAbs); err != nil {
		return fmt.Errorf("input file %q not found", inputAbs)
	}

	feedDir := filepath.Dir(inputAbs)
	repoDir := filepath.Base(feedDir)

	// A zero rune means "no delimiter supplied by caller — use the default unless
	// feed.json overrides it."
	if delim == 0 {
		delim = feedcsv.DefaultDelimiter
	}

	// Load feed.json if present; fill in any values not explicitly set by flags.
	cfg, err := feedconfig.Load(feedDir)
	if err != nil {
		return err
	}
	if cfg != nil {
		if title == "" || !changed["title"] {
			title = cfg.Title
		}
		if (description == "" || !changed["description"]) && cfg.Description != "" {
			description = cfg.Description
		}
		if (author == "" || !changed["author"]) && cfg.Author != "" {
			author = cfg.Author
		}
		if (websiteURL == "" || !changed["website"]) && cfg.WebsiteURL != "" {
			websiteURL = cfg.WebsiteURL
		}
		if (imageURL == "" || !changed["image-url"]) && cfg.ImageURL != "" {
			imageURL = cfg.ImageURL
		}
		if (feedURL == "" || !changed["feed-url"]) && cfg.FeedURL != "" {
			feedURL = cfg.FeedURL
		}
		if !changed["delimiter"] && cfg.Delimiter != "" {
			delim = cfg.DelimiterRune()
		}
	}

	// Self-hosted feeds default to their URLs in the bucket.
	host, err := hosting.Load(filepath.Dir(feedDir))
	if err != nil {
		return err
	}
	if host != nil {
		if feedURL == "" {
			feedURL = host.URL(repoDir + "/feed.xml")
		}
		if imageURL == "" {
			imageURL = host.URL(repoDir + "/image.jpg")
		}
	}

	if feedURL == "" || imageURL == "" {
		return fmt.Errorf("no feed URL or image URL for %q: add %s to the feed directory, "+
			"set \"feed_url\" and \"image_url\" in feed.json, or pass --feed-url and --image-url",
			repoDir, hosting.Filename)
	}

	if title == "" {
		return fmt.Errorf("podcast title is required (pass --title or add \"title\" to feed.json)")
	}

	outputFile := replaceExt(inputAbs, ".xml")

	opts := feedrss.FeedOptions{
		Title:       title,
		Description: description,
		Author:      author,
		WebsiteURL:  websiteURL,
		ImageURL:    imageURL,
		FeedURL:     feedURL,
		RepoDir:     repoDir,
	}
	opts.SetDefaults()

	fmt.Printf("Podcast Title:       %q\n", opts.Title)
	fmt.Printf("Podcast Description: %q\n", opts.Description)
	fmt.Printf("Podcast Author:      %q\n", opts.Author)
	fmt.Printf("Podcast Website:     %q\n", opts.WebsiteURL)
	fmt.Printf("Podcast Image URL:   %q\n", opts.ImageURL)
	fmt.Printf("Podcast Feed URL:    %q\n", opts.FeedURL)
	fmt.Printf("\nInput File:    %q\n", inputAbs)
	fmt.Printf("Repo Dir:      %q\n", repoDir)
	fmt.Printf("Output File:   %q\n", outputFile)
	fmt.Println()

	records, err := feedcsv.Read(inputAbs, delim)
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	xmlBytes, err := feedrss.Build(opts, records, os.Stdout)
	if err != nil {
		return fmt.Errorf("build RSS: %w", err)
	}

	newHash := feedrss.HashContent(xmlBytes)
	if existing, err := os.ReadFile(outputFile); err == nil {
		if feedrss.HashContent(existing) == newHash {
			fmt.Println("No changes detected. Skipping update.")
			return nil
		}
	}

	if err := os.WriteFile(outputFile, xmlBytes, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputFile, err)
	}

	fmt.Printf("Created podcast RSS XML feed: %s\n", outputFile)
	fmt.Printf("Once deployed, check feed by entering %s into https://validator.livewire.io\n", opts.FeedURL)
	return nil
}

func replaceExt(path, newExt string) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)] + newExt
}

func runeFromString(s string) rune {
	if len(s) == 0 {
		return feedcsv.DefaultDelimiter
	}
	return []rune(s)[0]
}
