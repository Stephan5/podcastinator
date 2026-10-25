package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/Stephan5/podcastinator/internal/s3util"
	"github.com/Stephan5/podcastinator/internal/urlutil"
	"github.com/spf13/cobra"
)

func newSelfhostOrphansCmd() *cobra.Command {
	var (
		bucket        string
		prefix        string
		region        string
		delimiter     string
		feedDir       string
		failOnOrphans bool
		deleteOrphans bool
	)

	cmd := &cobra.Command{
		Use:   "selfhost-orphans <feed-slug>",
		Short: "Find S3 files no longer referenced by a CSV feed",
		Long: `Lists S3 objects under the feed's S3 prefix and compares them to URLs
referenced in the CSV. Reports any orphaned files (present in S3 but not
referenced in the CSV). The feed files that publish uploads (feed.xml,
image.jpg) are never reported.

The bucket, region and prefix come from feed/hosting.json; flags override them.

The feed directory is located automatically from the working directory (looks
for feed/<slug>/feed.csv). Override with --feed-dir.

Exit codes:
  0 = success, no orphans (or orphans found but --fail-on-orphans not set)
  2 = orphans found and --fail-on-orphans is set`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir(feedDir)
			if err != nil {
				return err
			}
			host, err := resolveHosting(cmd, feedDirAbs, bucket, region, prefix)
			if err != nil {
				return err
			}
			slug := args[0]
			csvPath := filepath.Join(feedDirAbs, slug, "feed.csv")
			return runSelfhostOrphans(cmd.Context(), csvPath, host,
				runeFromString(delimiter), failOnOrphans, deleteOrphans)
		},
	}

	addHostingFlags(cmd, &bucket, &region, &prefix)
	cmd.Flags().StringVar(&delimiter, "delimiter", string(rune(feedcsv.DefaultDelimiter)), "CSV field delimiter")
	cmd.Flags().StringVar(&feedDir, "feed-dir", "", "Path to the feed directory (default: <repo>/feed)")
	cmd.Flags().BoolVar(&failOnOrphans, "fail-on-orphans", false, "Exit with code 2 if orphans are found")
	cmd.Flags().BoolVar(&deleteOrphans, "delete-orphans", false, "Delete orphaned S3 objects")

	return cmd
}

func runSelfhostOrphans(ctx context.Context, inputFile string, host *hosting.Config,
	delim rune, failOnOrphans, deleteOrphans bool) error {

	inputAbs, err := filepath.Abs(inputFile)
	if err != nil {
		return err
	}
	if _, err := os.Stat(inputAbs); err != nil {
		return fmt.Errorf("input file %q not found", inputAbs)
	}

	repoDir := filepath.Base(filepath.Dir(inputAbs))
	bucket, prefix, region := host.Bucket, host.Prefix, host.Region
	keyRoot := host.KeyRoot(repoDir)

	fmt.Printf("Input File:  %q\n", inputAbs)
	fmt.Printf("Bucket:      %q\n", bucket)
	fmt.Printf("Prefix:      %q\n", prefix)
	fmt.Printf("Region:      %q\n", region)
	fmt.Printf("Repo Dir:    %q\n", repoDir)
	fmt.Printf("Key Root:    %q\n", keyRoot)
	fmt.Printf("Delete:      %v\n\n", deleteOrphans)

	// Parse CSV and collect referenced keys
	records, err := feedcsv.Read(inputAbs, delim)
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	referenced := map[string]bool{}
	for _, file := range hosting.PublishedFiles {
		referenced[keyRoot+file] = true
	}
	for _, rec := range records {
		if rec.URL == "" {
			continue
		}
		decoded := urlutil.Decode(rec.URL)
		clean := urlutil.StripQuery(decoded)

		if b, key, err := s3util.HTTPToS3URL(clean, bucket); err == nil && b == bucket {
			if strings.HasPrefix(key, keyRoot) {
				referenced[key] = true
			}
		}
	}

	fmt.Printf("Referenced keys in CSV: %d\n", len(referenced)-len(hosting.PublishedFiles))

	// List remote keys
	s3Client, err := s3util.New(ctx, region)
	if err != nil {
		return err
	}

	remoteKeys, err := s3Client.List(ctx, bucket, keyRoot)
	if err != nil {
		return err
	}

	sort.Strings(remoteKeys)
	fmt.Printf("Remote keys in S3: %d\n", len(remoteKeys))

	// Find orphans
	var orphans []string
	for _, key := range remoteKeys {
		if !referenced[key] {
			orphans = append(orphans, key)
		}
	}

	if len(orphans) == 0 {
		fmt.Println("\nNo orphaned self-hosted files found.")
		return nil
	}

	fmt.Printf("\nFound %d orphaned self-hosted file(s):\n", len(orphans))
	for _, key := range orphans {
		fmt.Printf("  ORPHAN s3://%s/%s\n", bucket, key)
	}

	if deleteOrphans {
		fmt.Println("\nDeleting orphaned self-hosted file(s)...")
		for _, key := range orphans {
			fmt.Printf("  DELETE s3://%s/%s\n", bucket, key)
			if err := s3Client.Delete(ctx, bucket, key); err != nil {
				return fmt.Errorf("delete s3://%s/%s: %w", bucket, key, err)
			}
		}
	}

	if failOnOrphans {
		os.Exit(2)
	}
	return nil
}
