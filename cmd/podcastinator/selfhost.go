package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/hosting"
	"github.com/Stephan5/podcastinator/internal/s3util"
	"github.com/Stephan5/podcastinator/internal/urlutil"
	"github.com/spf13/cobra"
)

func newSelfhostCmd() *cobra.Command {
	var (
		bucket    string
		prefix    string
		region    string
		delimiter string
		feedDir   string
	)

	cmd := &cobra.Command{
		Use:   "selfhost <feed-slug>",
		Short: "Upload podcast episode files to AWS S3 and update the CSV",
		Long: `Reads the feed.csv for the given feed slug and uploads each episode's
audio file to S3.

Episodes are named: NNN-slugified-title.ext (zero-padded to item count digits).
The CSV is updated in-place with the new S3 HTTPS URLs.

The feed directory is located automatically from the working directory (looks
for feed/<slug>/feed.csv). Override with --feed-dir.

The bucket, region and prefix come from feed/hosting.json; flags override them.

Handles:
  - Already at destination URL: skipped
  - Already in bucket at different key: moved (S3 copy + delete)
  - Remote HTTP/HTTPS URL: downloaded and uploaded
  - Local file:// URL: uploaded directly`,
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
			return runSelfhost(cmd.Context(), csvPath, host, runeFromString(delimiter))
		},
	}

	addHostingFlags(cmd, &bucket, &region, &prefix)
	cmd.Flags().StringVar(&delimiter, "delimiter", string(rune(feedcsv.DefaultDelimiter)), "CSV field delimiter")
	cmd.Flags().StringVar(&feedDir, "feed-dir", "", "Path to the feed directory (default: <repo>/feed)")

	return cmd
}

func runSelfhost(ctx context.Context, inputFile string, host *hosting.Config, delim rune) error {
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
	baseHTTPDst := host.S3URL() + "/" + keyRoot

	records, err := feedcsv.Read(inputAbs, delim)
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	itemCount := len(records)
	paddingWidth := len(fmt.Sprintf("%d", itemCount))

	fmt.Printf("Input File:    %q\n", inputAbs)
	fmt.Printf("Repo Dir:      %q\n", repoDir)
	fmt.Printf("Bucket:        %q\n", bucket)
	fmt.Printf("Prefix:        %q\n", prefix)
	fmt.Printf("Region:        %q\n", region)
	fmt.Printf("Key Root:      %q\n", keyRoot)
	fmt.Printf("Item Count:    %d (requires %d digits of padding)\n\n", itemCount, paddingWidth)

	s3Client, err := s3util.New(ctx, region)
	if err != nil {
		return err
	}

	updated := make([]feedcsv.Record, len(records))
	for i, rec := range records {
		num := i + 1
		paddedNum := fmt.Sprintf("%0*d", paddingWidth, num)
		srcURL := rec.URL

		// Strip query and get filename/extension
		cleanURL := urlutil.StripQuery(srcURL)
		filename := filepath.Base(cleanURL)

		ext := filepath.Ext(filename)
		if !isSupportedAudioExt(ext) {
			return fmt.Errorf("item %d (%q): unsupported extension %q in %q", num, rec.Title, ext, filename)
		}

		// Build destination filename
		slug := urlutil.Slugify(fmt.Sprintf("%s-%s", paddedNum, rec.Title))
		dstFilename := slug + ext
		dstKey := keyRoot + dstFilename
		httpDstURL, _ := urlutil.Encode(baseHTTPDst + dstFilename)
		s3DstURL := "s3://" + bucket + "/" + dstKey

		fmt.Printf("Item %d: %q\n", num, rec.Title)
		fmt.Printf("  Src URL: %q\n", srcURL)
		fmt.Printf("  Dst URL: %q\n", httpDstURL)
		fmt.Printf("  Dst S3:  %q\n", s3DstURL)
		fmt.Printf("  File:    %q\n", dstFilename)

		// Encode src URL
		encodedSrc, _ := urlutil.Encode(srcURL)

		var newURL string

		switch {
		case encodedSrc == httpDstURL:
			fmt.Println("  → Already at destination. Skipping.")
			newURL = encodedSrc

		case isS3URL(encodedSrc, bucket, region):
			fmt.Printf("  → Already in bucket, moving to new location\n")
			if err := s3util.ValidateURL(encodedSrc); err != nil {
				return fmt.Errorf("validate src URL %s: %w", encodedSrc, err)
			}
			decodedSrc := urlutil.Decode(srcURL)
			srcBucket, srcKey, err := s3util.HTTPToS3URL(decodedSrc, bucket)
			if err != nil {
				return fmt.Errorf("parse src S3 URL: %w", err)
			}
			if err := s3Client.Move(ctx, srcBucket, srcKey, bucket, dstKey); err != nil {
				fmt.Printf("  Warning: move failed: %v. Keeping original URL.\n", err)
				newURL = encodedSrc
			} else {
				if err := s3util.ValidateURL(httpDstURL); err != nil {
					return fmt.Errorf("validate dst URL after move: %w", err)
				}
				newURL = httpDstURL
			}

		case isHTTPURL(srcURL):
			fmt.Printf("  → Remote URL, downloading and uploading to S3\n")
			if err := s3util.ValidateURL(encodedSrc); err != nil {
				return fmt.Errorf("validate src URL %s: %w", encodedSrc, err)
			}
			if err := s3Client.DownloadAndUpload(ctx, encodedSrc, bucket, dstKey, audioContentType(ext)); err != nil {
				return fmt.Errorf("download+upload item %d: %w", num, err)
			}
			if err := s3util.ValidateURL(httpDstURL); err != nil {
				return fmt.Errorf("validate dst URL after upload: %w", err)
			}
			newURL = httpDstURL

		case strings.HasPrefix(srcURL, "file://"):
			fmt.Printf("  → Local file URL, uploading to S3\n")
			localPath := strings.TrimPrefix(srcURL, "file://")
			if !filepath.IsAbs(localPath) {
				localPath = filepath.Join(filepath.Dir(inputAbs), localPath)
			}
			f, err := os.Open(localPath)
			if err != nil {
				return fmt.Errorf("open local file %q: %w", localPath, err)
			}
			defer f.Close()
			if err := s3Client.UploadReader(ctx, bucket, dstKey, audioContentType(ext), f); err != nil {
				return fmt.Errorf("upload local file %q: %w", localPath, err)
			}
			if err := s3util.ValidateURL(httpDstURL); err != nil {
				return fmt.Errorf("validate dst URL after upload: %w", err)
			}
			newURL = httpDstURL

		default:
			return fmt.Errorf("item %d: unknown URL scheme: %q", num, srcURL)
		}

		updated[i] = feedcsv.Record{
			Title:       rec.Title,
			Description: rec.Description,
			Date:        rec.Date,
			URL:         newURL,
		}
		fmt.Printf("  → New URL: %q\n\n", newURL)
	}

	if err := feedcsv.Write(inputAbs, delim, updated); err != nil {
		return fmt.Errorf("write updated CSV: %w", err)
	}

	// Warn about any non-self-hosted URLs remaining
	for _, rec := range updated {
		if isHTTPURL(rec.URL) && !strings.HasPrefix(rec.URL, baseHTTPDst) {
			fmt.Printf("Warning: not fully self-hosted: %q\n", rec.URL)
		}
	}

	fmt.Printf("Updated CSV written: %s\n", inputAbs)
	return nil
}

func isSupportedAudioExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp3", ".m4a", ".m4b", ".ogg", ".wav":
		return true
	}
	return false
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func isS3URL(u, bucket, region string) bool {
	prefix := fmt.Sprintf("https://s3.%s.amazonaws.com/%s", region, bucket)
	return strings.HasPrefix(u, prefix)
}

func fetchHTTP(url string) (*http.Response, error) {
	return http.Get(url) //nolint:noctx
}

// keep fetchHTTP reachable
var _ = fetchHTTP
