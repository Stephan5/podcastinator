package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "podcastinator",
	Short: "Podcast RSS feed management tool",
	Long: `podcastinator is a CLI tool for managing podcast RSS feeds.

It can convert between CSV and RSS XML formats, self-host episode files
on AWS S3, publish feeds to the hosting bucket, archive episodes locally,
and regenerate all feeds.`,
}

func main() {
	rootCmd.AddCommand(
		newCSV2RSSCmd(),
		newRSS2CSVCmd(),
		newSelfhostCmd(),
		newSelfhostOrphansCmd(),
		newArchiveCmd(),
		newPubdateCmd(),
		newRegenerateCmd(),
		newPublishCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
