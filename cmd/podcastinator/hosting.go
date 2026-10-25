package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/spf13/cobra"
)

// addHostingFlags registers the --bucket/--region/--prefix overrides for hosting.json.
func addHostingFlags(cmd *cobra.Command, bucket, region, prefix *string) {
	cmd.Flags().StringVar(bucket, "bucket", "", "S3 bucket name (overrides hosting.json)")
	cmd.Flags().StringVar(region, "region", hosting.DefaultRegion, "AWS region (overrides hosting.json)")
	cmd.Flags().StringVar(prefix, "prefix", "", "S3 key prefix, no leading/trailing slash (overrides hosting.json)")
}

// resolveHosting loads hosting.json from the feed directory and applies any
// --bucket/--region/--prefix flags the user set explicitly.
func resolveHosting(cmd *cobra.Command, feedDirAbs, bucket, region, prefix string) (*hosting.Config, error) {
	cfg, err := hosting.Load(feedDirAbs)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &hosting.Config{}
	}
	if cmd.Flags().Changed("bucket") {
		cfg.Bucket = bucket
	}
	if cmd.Flags().Changed("region") || cfg.Region == "" {
		cfg.Region = region
	}
	if cmd.Flags().Changed("prefix") {
		cfg.Prefix = prefix
	}
	cfg.Normalize()
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("no bucket configured: add %q to %s or pass --bucket",
			"bucket", filepath.Join(feedDirAbs, hosting.Filename))
	}
	return cfg, nil
}

// audioContentType is the Content-Type S3 serves an episode file with.
func audioContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".m4b":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	}
	return ""
}
