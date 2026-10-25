package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/urlutil"
	"github.com/spf13/cobra"
)

// defaultProbeConcurrency is how many episodes durations probes at once.
const defaultProbeConcurrency = 8

// probeTimeout stops one unresponsive host from hanging durations.
const probeTimeout = 2 * time.Minute

func newDurationsCmd() *cobra.Command {
	var exclude []string
	var concurrency int

	cmd := &cobra.Command{
		Use:     "durations [feed...]",
		GroupID: "generate",
		Short:   "Fill in each episode's duration in feed.csv with ffprobe",
		Long: `Runs ffprobe on every episode in feed.csv without a duration and writes
what it finds, in seconds, to the CSV's duration column, which build writes
out as itunes:duration so podcast apps can show how long episodes are before
they're downloaded.

ffprobe, part of ffmpeg, has to be installed. It only reads as much of each
file as it needs, using range requests, so it doesn't download whole
episodes. Episodes it can't probe are listed and left without a duration; the
rest are still written. import and update copy durations from the source feed
where it has them. Run build afterwards.

Examples:
  podcastinator durations
  podcastinator durations example-show
  podcastinator durations --concurrency 2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if concurrency < 1 {
				return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
			}
			ffprobe, err := exec.LookPath("ffprobe")
			if err != nil {
				return fmt.Errorf("durations needs ffprobe, which comes with ffmpeg (https://ffmpeg.org/download.html)")
			}
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			feeds, err := selectFeeds(feedDirAbs, args, exclude)
			if err != nil {
				return err
			}
			failed := 0
			for _, f := range feeds {
				n, err := fillDurations(cmd.Context(), f, ffprobe, concurrency)
				if err != nil {
					return fmt.Errorf("feed %q: %w", f.slug, err)
				}
				failed += n
			}
			if failed > 0 {
				return fmt.Errorf("%d episode(s) still have no duration", failed)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Feeds to skip when filling in every feed")
	cmd.Flags().IntVar(&concurrency, "concurrency", defaultProbeConcurrency, "Episodes to probe at once")
	return cmd
}

// fillDurations probes f's episodes that have no duration, concurrency at a
// time, and writes the ones it finds to the CSV. It returns how many it
// couldn't find.
func fillDurations(ctx context.Context, f feed, ffprobe string, concurrency int) (int, error) {
	records, err := f.records()
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("%s: no %s, skipping\n", f.slug, csvFilename)
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read CSV: %w", err)
	}
	var missing []int
	for i, rec := range records {
		if rec.Duration == "" {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		fmt.Printf("%s: every episode has a duration\n", f.slug)
		return 0, nil
	}
	fmt.Printf("%s: probing %d episode(s)\n", f.slug, len(missing))

	var (
		mu     sync.Mutex
		done   int
		failed int
		wg     sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)
	for _, i := range missing {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			rec := records[i]
			secs, err := probeDuration(ctx, ffprobe, f, rec)

			mu.Lock()
			defer mu.Unlock()
			done++
			if err != nil {
				failed++
				fmt.Printf("  [%d/%d] episode %d %q: %v\n", done, len(missing), i+1, rec.Title, err)
				return
			}
			records[i].Duration = strconv.Itoa(secs)
			fmt.Printf("  [%d/%d] episode %d %q: %ds\n", done, len(missing), i+1, rec.Title, secs)
		})
	}
	wg.Wait()

	if failed < len(missing) {
		if err := feedcsv.Write(f.csvPath(), f.cfg.DelimiterRune(), records); err != nil {
			return 0, err
		}
	}
	fmt.Printf("%s: %d duration(s) added, %d failed\n\n", f.slug, len(missing)-failed, failed)
	return failed, nil
}

// probeDuration asks ffprobe for rec's duration in whole seconds. A file://
// URL is relative to the feed's directory, as with audio upload.
func probeDuration(ctx context.Context, ffprobe string, f feed, rec feedcsv.Record) (int, error) {
	args := []string{"-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1"}
	switch {
	case isHTTPURL(rec.URL):
		encoded, err := urlutil.Encode(rec.URL)
		if err != nil {
			return 0, fmt.Errorf("encode URL: %w", err)
		}
		// Some hosts turn away unknown user agents, as with import.
		args = append(args, "-user_agent", "podcastinator/"+version+" (+https://github.com/Stephan5/podcastinator)", encoded)
	case strings.HasPrefix(rec.URL, "file://"):
		path := strings.TrimPrefix(rec.URL, "file://")
		if !filepath.IsAbs(path) {
			path = filepath.Join(f.dir, path)
		}
		args = append(args, "file:"+path)
	default:
		return 0, fmt.Errorf("unknown URL scheme: %q", rec.URL)
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffprobe, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(bytes.TrimSpace(exitErr.Stderr)) > 0 {
			msg := strings.ReplaceAll(string(bytes.TrimSpace(exitErr.Stderr)), "\n", "; ")
			return 0, fmt.Errorf("ffprobe: %s", msg)
		}
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || secs <= 0 {
		return 0, fmt.Errorf("ffprobe found no duration (%q)", strings.TrimSpace(string(out)))
	}
	return int(math.Round(secs)), nil
}
