package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/feedrss"
	"github.com/spf13/cobra"
)

func newRSS2CSVCmd() *cobra.Command {
	var (
		repoDir   string
		delimiter string
		feedDir   string
	)

	cmd := &cobra.Command{
		Use:   "rss2csv <input.xml>",
		Short: "Convert an RSS podcast feed to a CSV file",
		Long: `Parses an RSS XML podcast feed and writes a CSV file with columns:
title, description, date, url.

Episodes are sorted by publication date (ascending).
The CSV is written to <feed-dir>/<repo-dir>/feed.csv. The input XML is not
copied; regenerate builds feed.xml from the CSV and feed.json.

The feed directory defaults to <repo-root>/feed, detected relative to the
working directory. Override with --feed-dir.

--repo-dir defaults to the input's directory name when the input is already
in <feed-dir>/<slug>/.

Examples:
  podcastinator rss2csv feed/my-show/feed.xml
  podcastinator rss2csv ~/Downloads/their.xml --repo-dir my-show`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir(feedDir)
			if err != nil {
				return err
			}
			return runRSS2CSV(args[0], feedDirAbs, repoDir, runeFromString(delimiter))
		},
	}

	cmd.Flags().StringVar(&repoDir, "repo-dir", "", "Feed subdirectory name under feed/ (default: the input's directory, if it is in feed/)")
	cmd.Flags().StringVar(&delimiter, "delimiter", string(rune(feedcsv.DefaultDelimiter)), "CSV field delimiter")
	cmd.Flags().StringVar(&feedDir, "feed-dir", "", "Path to the feed directory (default: <repo>/feed)")

	return cmd
}

func runRSS2CSV(inputFile, feedDirAbs, repoDir string, delim rune) error {
	inputAbs, err := filepath.Abs(inputFile)
	if err != nil {
		return fmt.Errorf("resolve input path: %w", err)
	}
	if _, err := os.Stat(inputAbs); err != nil {
		return fmt.Errorf("input file %q not found", inputAbs)
	}

	if repoDir == "" {
		repoDir, err = repoDirFromInput(inputAbs, feedDirAbs)
		if err != nil {
			return err
		}
	}

	feedRepoPath := filepath.Join(feedDirAbs, repoDir)
	if err := os.MkdirAll(feedRepoPath, 0755); err != nil {
		return fmt.Errorf("create feed directory: %w", err)
	}

	outputFile := filepath.Join(feedRepoPath, "feed.csv")

	fmt.Printf("Input File:   %q\n", inputAbs)
	fmt.Printf("Repo Dir:     %q\n", repoDir)
	fmt.Printf("Output File:  %q\n", outputFile)
	fmt.Println()

	f, err := os.Open(inputAbs)
	if err != nil {
		return fmt.Errorf("open XML: %w", err)
	}
	defer f.Close()

	parsed, err := feedrss.Parse(f)
	if err != nil {
		return fmt.Errorf("parse RSS: %w", err)
	}

	fmt.Printf("Feed Title:       %q\n", parsed.Title)
	fmt.Printf("Feed Description: %q\n", parsed.Description)
	fmt.Printf("Feed Link:        %q\n", parsed.Link)
	fmt.Printf("Image URL:        %q\n", parsed.ImageURL)
	fmt.Println()

	for _, rec := range parsed.Records {
		fmt.Printf("Title:   %q\nPubDate: %q\nURL:     %q\n\n", rec.Title, rec.Date, rec.URL)
	}

	if err := feedcsv.Write(outputFile, delim, parsed.Records); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}

	fmt.Printf("Created CSV from podcast XML feed: %s\n", outputFile)
	return nil
}

// repoDirFromInput returns the name of the input's directory when that
// directory sits directly under feedDirAbs, i.e. the input is feed/<slug>/*.xml.
func repoDirFromInput(inputAbs, feedDirAbs string) (string, error) {
	inputDir := filepath.Dir(inputAbs)
	parent, err := os.Stat(filepath.Dir(inputDir))
	if err != nil {
		return "", err
	}
	feedDir, err := os.Stat(feedDirAbs)
	if err != nil {
		return "", err
	}
	// SameFile rather than comparing paths, so symlinks (e.g. macOS /tmp) match.
	if !os.SameFile(parent, feedDir) {
		return "", fmt.Errorf("--repo-dir is required when the input is not in %s/<slug>/", feedDirAbs)
	}
	return filepath.Base(inputDir), nil
}
