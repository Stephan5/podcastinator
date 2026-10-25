package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Stephan5/podcastinator/internal/feedconfig"
	"github.com/spf13/cobra"
)

func newRegenerateCmd() *cobra.Command {
	var (
		feed    string
		exclude string
		feedDir string
	)

	cmd := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate podcast RSS feeds from their feed.json config and feed.csv",
		Long: `Regenerates one or all podcast feeds by reading each feed's feed.json config
and running csv2rss against the accompanying feed.csv.

By default, regenerates all feeds found under the feed directory.
Use --feed to regenerate a single feed by its directory name.
Use --exclude to skip specific feeds when regenerating all.

The feed directory defaults to <repo-root>/feed, detected relative to the
working directory. Override with --feed-dir.

Examples:
  podcastinator regenerate
  podcastinator regenerate --feed example-show
  podcastinator regenerate --exclude "some-feed,another-feed"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegenerate(feedDir, feed, exclude)
		},
	}

	cmd.Flags().StringVar(&feed, "feed", "", "Regenerate only this feed (directory name under feed/)")
	cmd.Flags().StringVar(&exclude, "exclude", "", "Comma-separated feed directory names to skip (when regenerating all)")
	cmd.Flags().StringVar(&feedDir, "feed-dir", "", "Path to the feed directory (default: <repo>/feed)")

	return cmd
}

func runRegenerate(feedDirFlag, singleFeed, excludeList string) error {
	feedDirAbs, err := resolveFeedDir(feedDirFlag)
	if err != nil {
		return err
	}

	// Build target list of feed directory names.
	var targets []string

	if singleFeed != "" {
		// Single feed mode.
		if excludeList != "" {
			return fmt.Errorf("--exclude cannot be used together with --feed")
		}
		targets = []string{singleFeed}
	} else {
		// All feeds, minus exclusions.
		excluded := parseCommaSep(excludeList)
		if len(excluded) > 0 {
			fmt.Printf("Excluding: %s\n\n", strings.Join(keys(excluded), ", "))
		}

		entries, err := os.ReadDir(feedDirAbs)
		if err != nil {
			return fmt.Errorf("read feed dir %q: %w", feedDirAbs, err)
		}
		for _, e := range entries {
			if !e.IsDir() || excluded[e.Name()] {
				continue
			}
			targets = append(targets, e.Name())
		}
		sort.Strings(targets)
	}

	for _, name := range targets {
		dir := filepath.Join(feedDirAbs, name)

		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("feed directory not found: %q", dir)
		}

		// Must have a feed.json
		cfg, err := feedconfig.Load(dir)
		if err != nil {
			return fmt.Errorf("feed %q: %w", name, err)
		}
		if cfg == nil {
			fmt.Printf("Skipping %q — no feed.json found.\n\n", name)
			continue
		}

		// Must have a feed.csv
		csvPath := filepath.Join(dir, "feed.csv")
		if _, err := os.Stat(csvPath); os.IsNotExist(err) {
			fmt.Printf("Skipping %q — no feed.csv found.\n\n", name)
			continue
		}

		sep := strings.Repeat("-", 51)
		fmt.Printf("%s\n--- Regenerating: %s\n%s\n\n", sep, name, sep)

		if err := runCSV2RSS(csvPath, "", "", "", "", "", "", 0, map[string]bool{}); err != nil {
			return fmt.Errorf("feed %q: %w", name, err)
		}

		fmt.Printf("\n%s\n--- Done: %s\n%s\n\n\n", sep, name, sep)
	}

	fmt.Println("Regeneration complete!")
	return nil
}

// resolveFeedDir returns the absolute path to the feed directory.
func resolveFeedDir(flag string) (string, error) {
	if flag != "" {
		abs, err := filepath.Abs(flag)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("feed directory %q not found", abs)
		}
		return abs, nil
	}

	// Auto-detect: look for feed/ relative to cwd, then walk up.
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(cwd, "feed")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("could not locate feed directory; use --feed-dir to specify it")
}

func parseCommaSep(s string) map[string]bool {
	result := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result[part] = true
		}
	}
	return result
}

func keys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
