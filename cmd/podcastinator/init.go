package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var cfg hosting.Config

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a feed directory, and hosting.json if given a bucket",
		Long: `Creates the feed directory (./feed, or --feed-dir) and, with --bucket, a
hosting.json saying where the feeds are hosted. Won't overwrite an existing
hosting.json.

Examples:
  podcastinator init
  podcastinator init --bucket my-podcast-bucket --public-url https://podcasts.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := feedDirFlag
			if dir == "" {
				dir = "feed"
			}
			return runInit(dir, cfg)
		},
	}

	cmd.Flags().StringVar(&cfg.Bucket, "bucket", "", "S3 bucket to host feeds and audio in")
	cmd.Flags().StringVar(&cfg.Region, "region", hosting.DefaultRegion, "The bucket's AWS region")
	cmd.Flags().StringVar(&cfg.Prefix, "prefix", "", "S3 key prefix to put everything under")
	cmd.Flags().StringVar(&cfg.PublicURL, "public-url", "", "URL that serves the bucket root (default: its S3 URL)")
	return cmd
}

func runInit(dir string, cfg hosting.Config) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	fmt.Printf("Feed directory: %s\n", dir)

	path := filepath.Join(dir, hosting.Filename)
	if _, err := os.Stat(path); err == nil {
		if cfg.Bucket != "" {
			return fmt.Errorf("%s already exists; edit it instead", path)
		}
		fmt.Printf("Hosting:        %s\n", path)
		return nil
	}
	if cfg.Bucket == "" {
		fmt.Println("\nNo --bucket, so no hosting.json. Feeds need one, or \"feed\" and \"image\"")
		fmt.Println("URLs in their feed.json, before they can be built.")
		return nil
	}

	cfg.Normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("Hosting:        %s → %s\n", path, cfg.BaseURL())
	fmt.Println("\nNext: podcastinator import <url>")
	return nil
}
