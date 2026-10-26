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
which only writes feeds and cover art.`,
	}
	cmd.AddCommand(newAudioUploadCmd(), newAudioPruneCmd(), newAudioDownloadCmd())
	return cmd
}

func newAudioUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <feed>",
		Short: "Copy a feed's episodes into the bucket and point its CSV at them",
		Long: `Uploads each episode in <feed>/feed.csv to <prefix>/<feed>/ in the bucket,
named NNN-slugified-title.ext, and rewrites the CSV with the new URLs, which
are under public_url from hosting.json.

  - already at its destination: skipped
  - at its destination, but by its S3 URL: pointed at its public URL
  - elsewhere in the bucket: moved
  - a remote http(s) URL: downloaded and uploaded, unless already uploaded
  - a local file:// URL (relative to the feed directory): uploaded

Episodes keep their GUIDs, so subscribers don't see moved episodes as new.
The CSV is saved after each episode, so a failed run can be resumed by
running it again. Run build afterwards.`,
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
	baseHTTPDst := host.BaseURL() + "/" + keyRoot

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
	fmt.Printf("Key root:      %q\n", keyRoot)
	fmt.Printf("Episodes:      %d\n\n", itemCount)

	s3Client, err := s3util.New(ctx, region)
	if err != nil {
		return err
	}

	for i, rec := range records {
		num := i + 1
		paddedNum := fmt.Sprintf("%0*d", paddingWidth, num)
		srcURL := rec.URL

		cleanURL := urlutil.StripQuery(srcURL)
		filename := filepath.Base(cleanURL)

		ext := filepath.Ext(filename)
		if !isSupportedAudioExt(ext) {
			return fmt.Errorf("item %d (%q): unsupported extension %q in %q", num, rec.Title, ext, filename)
		}

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

		encodedSrc, _ := urlutil.Encode(srcURL)

		var newURL string
		srcKey, inBucket := host.KeyForURL(srcURL)

		switch {
		case encodedSrc == httpDstURL:
			fmt.Println("  → Already at destination. Skipping.")
			newURL = encodedSrc

		case inBucket && srcKey == dstKey:
			// Uploaded when audio was served from its S3 URL.
			fmt.Println("  → Already in bucket. Pointing at its public URL.")
			if err := s3util.ValidateURL(httpDstURL); err != nil {
				return fmt.Errorf("validate public URL %s: %w", httpDstURL, err)
			}
			newURL = httpDstURL

		case inBucket:
			fmt.Println("  → Already in bucket, moving to new location")
			if err := s3util.ValidateURL(encodedSrc); err != nil {
				return fmt.Errorf("validate src URL %s: %w", encodedSrc, err)
			}
			if err := s3Client.Move(ctx, bucket, srcKey, bucket, dstKey); err != nil {
				fmt.Printf("  Warning: move failed: %v. Keeping original URL.\n", err)
				newURL = encodedSrc
			} else {
				if err := s3util.ValidateURL(httpDstURL); err != nil {
					return fmt.Errorf("validate dst URL after move: %w", err)
				}
				newURL = httpDstURL
			}

		case isHTTPURL(srcURL):
			srcSize, err := s3util.HeadSize(encodedSrc)
			if err != nil {
				return fmt.Errorf("validate src URL %s: %w", encodedSrc, err)
			}
			dstSize, err := s3Client.Size(ctx, bucket, dstKey)
			if err != nil {
				return err
			}
			// Uploaded by an earlier run that stopped before rewriting the CSV.
			if dstSize > 0 && (srcSize < 0 || srcSize == dstSize) {
				fmt.Println("  → Remote URL, already uploaded to S3. Pointing at it.")
			} else {
				fmt.Println("  → Remote URL, downloading and uploading to S3")
				if err := s3Client.DownloadAndUpload(ctx, encodedSrc, bucket, dstKey, audioContentType(ext)); err != nil {
					return fmt.Errorf("download+upload item %d: %w", num, err)
				}
			}
			if err := s3util.ValidateURL(httpDstURL); err != nil {
				return fmt.Errorf("validate dst URL after upload: %w", err)
			}
			newURL = httpDstURL

		case strings.HasPrefix(srcURL, "file://"):
			fmt.Println("  → Local file URL, uploading to S3")
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

		fmt.Printf("  → New URL: %q\n\n", newURL)
		if newURL == srcURL {
			continue
		}
		// Saved after every episode, so a run that fails partway keeps what it uploaded.
		records[i].URL = newURL
		if err := feedcsv.Write(f.csvPath(), f.cfg.DelimiterRune(), records); err != nil {
			return fmt.Errorf("write updated CSV: %w", err)
		}
	}

	for _, rec := range records {
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
	fmt.Printf("Key root:    %q\n", keyRoot)
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
		if key, ok := host.KeyForURL(rec.URL); ok && strings.HasPrefix(key, keyRoot) {
			referenced[key] = true
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
	fmt.Printf("Output dir:  %q\n", dstDir)
	fmt.Printf("Media dir:   %q\n\n", mediaDir)

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
