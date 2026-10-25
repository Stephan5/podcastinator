package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Stephan5/podcastinator/internal/feedrss"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/Stephan5/podcastinator/internal/s3util"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var remote bool

	cmd := &cobra.Command{
		Use:   "status [feed...]",
		Short: "Show where each feed is between import and publish",
		Long: `Prints one line per feed:

  EPISODES   rows in feed.csv, and the newest episode's date
  BUILT      ok, stale (feed.csv, feed.json or hosting.json changed since the
             last build) or missing
  AUDIO      how many episodes are hosted in the bucket in hosting.json
  SOURCE     whether feed.json has a source for update

With --remote it also checks the bucket (which needs AWS credentials):

  PUBLISHED  yes if the bucket's feed.xml and image.jpg match the local ones`,
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			feeds, err := selectFeeds(feedDirAbs, args, nil)
			if err != nil {
				return err
			}
			return runStatus(cmd.Context(), feedDirAbs, feeds, remote)
		},
	}

	cmd.Flags().BoolVar(&remote, "remote", false, "Also compare each feed with what's published in the bucket")
	return cmd
}

func runStatus(ctx context.Context, feedDirAbs string, feeds []feed, remote bool) error {
	host, err := hosting.Load(feedDirAbs)
	if err != nil {
		return err
	}

	var client *s3util.Client
	if remote {
		if host == nil {
			return fmt.Errorf("--remote needs %s", filepath.Join(feedDirAbs, hosting.Filename))
		}
		if client, err = s3util.New(ctx, host.Region); err != nil {
			return err
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	header := "FEED\tEPISODES\tLATEST\tBUILT\tAUDIO\tSOURCE"
	if remote {
		header += "\tPUBLISHED"
	}
	fmt.Fprintln(w, header)

	for _, f := range feeds {
		records, err := f.records()
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("feed %q: %w", f.slug, err)
		}

		latest := "-"
		if t := latestDate(records); !t.IsZero() {
			latest = t.UTC().Format(time.RFC3339)
		}

		built, err := buildState(feedDirAbs, f)
		if err != nil {
			return fmt.Errorf("feed %q: %w", f.slug, err)
		}

		audio := "-"
		if host != nil {
			keyRoot := host.KeyRoot(f.slug)
			hosted := 0
			for _, rec := range records {
				if key, ok := host.KeyForURL(rec.URL); ok && strings.HasPrefix(key, keyRoot) {
					hosted++
				}
			}
			audio = fmt.Sprintf("%d/%d hosted", hosted, len(records))
		}

		source := "-"
		if f.cfg.Source != "" {
			source = "yes"
		}

		row := fmt.Sprintf("%s\t%d\t%s\t%s\t%s\t%s", f.slug, len(records), latest, built, audio, source)
		if remote {
			published, err := publishedState(ctx, client, host, f)
			if err != nil {
				return fmt.Errorf("feed %q: %w", f.slug, err)
			}
			row += "\t" + published
		}
		fmt.Fprintln(w, row)
	}
	return w.Flush()
}

// buildState says whether f's feed.xml is up to date with its inputs.
func buildState(feedDirAbs string, f feed) (string, error) {
	xmlBytes, err := os.ReadFile(f.xmlPath())
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	want, err := inputsHash(feedDirAbs, f)
	if err != nil {
		return "", err
	}
	if feedrss.StampedInputs(xmlBytes) != want {
		return "stale", nil
	}
	return "ok", nil
}

// publishedState says whether the bucket holds f's current feed.xml and image.jpg.
func publishedState(ctx context.Context, client *s3util.Client, host *hosting.Config, f feed) (string, error) {
	for _, file := range hosting.PublishedFiles {
		body, err := os.ReadFile(filepath.Join(f.dir, file))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		remote, err := client.ETag(ctx, host.Bucket, host.Key(f.slug+"/"+file))
		if err != nil {
			return "", err
		}
		if remote != etag(body) {
			return "no", nil
		}
	}
	return "yes", nil
}
