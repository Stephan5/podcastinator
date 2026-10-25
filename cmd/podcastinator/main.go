package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// feedDirFlag is the root --feed-dir flag, shared by every command.
var feedDirFlag string

var rootCmd = &cobra.Command{
	Use:     "podcastinator",
	Version: version,
	Short:   "Build podcast RSS feeds from CSVs and host them yourself",
	Long: `podcastinator keeps each podcast feed as a directory of episodes (feed.csv)
and metadata (feed.json), builds feed.xml from them, and hosts the feeds and
their audio in an S3 bucket.

  import → build → publish

Feeds are named by their directory under the feed directory, ./feed unless
--feed-dir says otherwise. Commands that take [feed...] act on every feed when
none are named.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func main() {
	rootCmd.PersistentFlags().StringVar(&feedDirFlag, "feed-dir", "", "Path to the feed directory (default: ./feed)")

	rootCmd.AddGroup(
		&cobra.Group{ID: "import", Title: "Import:"},
		&cobra.Group{ID: "generate", Title: "Generate:"},
		&cobra.Group{ID: "host", Title: "Self-host:"},
	)
	rootCmd.AddCommand(
		newInitCmd(),
		newImportCmd(),
		newUpdateCmd(),
		newBuildCmd(),
		newDatesCmd(),
		newAudioCmd(),
		newPublishCmd(),
		newStatusCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
