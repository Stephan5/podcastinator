package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

func newAudioCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "audio",
		GroupID: "host",
		Short:   "Upload, prune and download episode audio",
		Long: `Moves a feed's episode audio in and out of the bucket in hosting.json.

These need AWS credentials that can write audio to the bucket, unlike publish,
which only writes feeds, cover art and the index.`,
	}
	cmd.AddCommand(newAudioUploadCmd(), newAudioPruneCmd(), newAudioDownloadCmd())
	return cmd
}

func newAudioUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <feed>",
		Short: "Copy a feed's episodes into the bucket and point its CSV at them",
		Long: `Uploads each episode in <feed>/feed.csv to <prefix>/<feed>/ in the bucket,
named NNN-slugified-title.ext, and rewrites the CSV with the new URLs.

  - already at its destination: skipped
  - elsewhere in the bucket: moved
  - a remote http(s) URL: downloaded and uploaded
  - a local file:// URL (relative to the feed directory): uploaded

Episodes keep their GUIDs, so subscribers don't see moved episodes as new.
Run build afterwards.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			host, err := resolveHosting(feedDirAbs)
			if err != nil {
				return err
			}
			f, err := loadFeed(feedDirAbs, args[0])
			if err != nil {
				return err
			}
			return runAudioUpload(cmd.Context(), f, host)
		},
	}
}

func runAudioUpload(ctx context.Context, f feed, host *hosting.Config) error {
	bucket, region := host.Bucket, host.Region
	keyRoot := host.KeyRoot(f.slug)
	baseHTTPDst := host.S3URL() + "/" + keyRoot

	// Fix each episode's GUID before its URL changes.
	if err := ensureGUIDs(f); err != nil {
		return err
	}
	records, err := f.records()
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	itemCount := len(records)
	paddingWidth := len(fmt.Sprintf("%d", itemCount))

	fmt.Printf("Feed:          %q\n", f.slug)
	fmt.Printf("Bucket:        %q\n", bucket)
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

		case strings.HasPrefix(encodedSrc, host.S3URL()):
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
				localPath = filepath.Join(f.dir, localPath)
			}
			if err := uploadFile(ctx, s3Client, localPath, bucket, dstKey, audioContentType(ext)); err != nil {
				return err
			}
			if err := s3util.ValidateURL(httpDstURL); err != nil {
				return fmt.Errorf("validate dst URL after upload: %w", err)
			}
			newURL = httpDstURL

		default:
			return fmt.Errorf("item %d: unknown URL scheme: %q", num, srcURL)
		}

		rec.URL = newURL
		updated[i] = rec
		fmt.Printf("  → New URL: %q\n\n", newURL)
	}

	if err := feedcsv.Write(f.csvPath(), f.cfg.DelimiterRune(), updated); err != nil {
		return fmt.Errorf("write updated CSV: %w", err)
	}

	// Warn about any non-self-hosted URLs remaining
	for _, rec := range updated {
		if isHTTPURL(rec.URL) && !strings.HasPrefix(rec.URL, baseHTTPDst) {
			fmt.Printf("Warning: not fully self-hosted: %q\n", rec.URL)
		}
	}

	fmt.Printf("Updated %s. Run podcastinator build %s next.\n", f.csvPath(), f.slug)
	return nil
}

func uploadFile(ctx context.Context, client *s3util.Client, path, bucket, key, contentType string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open local file %q: %w", path, err)
	}
	defer file.Close()
	if err := client.UploadReader(ctx, bucket, key, contentType, file); err != nil {
		return fmt.Errorf("upload local file %q: %w", path, err)
	}
	return nil
}

func newAudioPruneCmd() *cobra.Command {
	var (
		del           bool
		failOnOrphans bool
	)

	cmd := &cobra.Command{
		Use:   "prune <feed>",
		Short: "Find audio in the bucket that the feed's CSV no longer points at",
		Long: `Compares the objects under <prefix>/<feed>/ in the bucket with the URLs in
<feed>/feed.csv and lists the ones nothing points at. feed.xml and image.jpg
are never listed. Only reports unless --delete is passed.

Exits 2 when orphans are found and --fail-on-orphans is set.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			host, err := resolveHosting(feedDirAbs)
			if err != nil {
				return err
			}
			f, err := loadFeed(feedDirAbs, args[0])
			if err != nil {
				return err
			}
			return runAudioPrune(cmd.Context(), f, host, del, failOnOrphans)
		},
	}

	cmd.Flags().BoolVar(&del, "delete", false, "Delete the orphaned objects")
	cmd.Flags().BoolVar(&failOnOrphans, "fail-on-orphans", false, "Exit with code 2 if orphans are found")
	return cmd
}

func runAudioPrune(ctx context.Context, f feed, host *hosting.Config, del, failOnOrphans bool) error {
	bucket := host.Bucket
	keyRoot := host.KeyRoot(f.slug)

	fmt.Printf("Feed:        %q\n", f.slug)
	fmt.Printf("Bucket:      %q\n", bucket)
	fmt.Printf("Key Root:    %q\n", keyRoot)
	fmt.Printf("Delete:      %v\n\n", del)

	records, err := f.records()
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
		clean := urlutil.StripQuery(urlutil.Decode(rec.URL))
		if b, key, err := s3util.HTTPToS3URL(clean, bucket); err == nil && b == bucket {
			if strings.HasPrefix(key, keyRoot) {
				referenced[key] = true
			}
		}
	}
	fmt.Printf("Referenced keys in CSV: %d\n", len(referenced)-len(hosting.PublishedFiles))

	s3Client, err := s3util.New(ctx, host.Region)
	if err != nil {
		return err
	}
	remoteKeys, err := s3Client.List(ctx, bucket, keyRoot)
	if err != nil {
		return err
	}
	sort.Strings(remoteKeys)
	fmt.Printf("Remote keys in S3: %d\n", len(remoteKeys))

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

	if del {
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

func newAudioDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download <feed> <dir>",
		Short: "Download a feed's episodes for safekeeping",
		Long: `Downloads every episode in <feed>/feed.csv to <dir>/<feed>/items/, copies the
feed's files alongside, and writes a local.csv pointing at the downloads with
file://items/<filename> URLs.

  <dir>/<feed>/
    items/        the episodes
    feed.csv      the feed's files, copied
    local.csv     feed.csv with file:// URLs

Doesn't need the bucket or AWS credentials.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			f, err := loadFeed(feedDirAbs, args[0])
			if err != nil {
				return err
			}
			return runAudioDownload(f, args[1])
		},
	}
}

func runAudioDownload(f feed, outputDir string) error {
	outputDirAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(outputDirAbs); err != nil {
		return fmt.Errorf("output directory %q not found", outputDirAbs)
	}

	dstDir := filepath.Join(outputDirAbs, f.slug)
	mediaDir := filepath.Join(dstDir, "items")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		return fmt.Errorf("create media dir: %w", err)
	}

	fmt.Printf("Feed:        %q\n", f.slug)
	fmt.Printf("Output Dir:  %q\n", dstDir)
	fmt.Printf("Media Dir:   %q\n\n", mediaDir)

	records, err := f.records()
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	localRecords := make([]feedcsv.Record, 0, len(records))
	for i, rec := range records {
		filename := filepath.Base(urlutil.StripQuery(rec.URL))
		if !isSupportedAudioExt(filepath.Ext(filename)) {
			return fmt.Errorf("item %d (%q): unsupported extension in %q", i+1, rec.Title, filename)
		}

		encodedURL, _ := urlutil.Encode(rec.URL)
		fmt.Printf("Item %d: %q\n  Downloading from: %s\n", i+1, rec.Title, encodedURL)

		dstPath := filepath.Join(mediaDir, filename)
		if err := downloadFile(encodedURL, dstPath); err != nil {
			return fmt.Errorf("download item %d (%q): %w", i+1, rec.Title, err)
		}

		localURL := "file://items/" + filename
		fmt.Printf("  Saved to: %q → local URL: %q\n\n", dstPath, localURL)

		rec.URL = localURL
		localRecords = append(localRecords, rec)
	}

	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(f.dir, entry.Name()), filepath.Join(dstDir, entry.Name())); err != nil {
			fmt.Printf("Warning: could not copy %q: %v\n", entry.Name(), err)
		}
	}

	if err := feedcsv.Write(filepath.Join(dstDir, "local.csv"), f.cfg.DelimiterRune(), localRecords); err != nil {
		return fmt.Errorf("write local.csv: %w", err)
	}

	fmt.Printf("Downloaded %d episodes to %s\n", len(records), dstDir)
	return nil
}

func downloadFile(url, dstPath string) error {
	// No client timeout: episodes can be large and slow to download.
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
