package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/urlutil"
	"github.com/spf13/cobra"
)

func newArchiveCmd() *cobra.Command {
	var delimiter string

	cmd := &cobra.Command{
		Use:   "archive <input_dir> <output_dir>",
		Short: "Download podcast episodes locally for archival",
		Long: `Downloads all episode audio files from URLs in the CSV feed to a local
directory structure:

  output_dir/
    <feed_name>/
      items/          ← downloaded audio files
      feed.csv        ← original CSV
      local.csv       ← CSV with file://items/<filename> URLs

Input directory must contain exactly one .csv file.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArchive(args[0], args[1], runeFromString(delimiter))
		},
	}

	cmd.Flags().StringVar(&delimiter, "delimiter", string(rune(feedcsv.DefaultDelimiter)), "CSV field delimiter")
	return cmd
}

func runArchive(inputDir, outputDir string, delim rune) error {
	inputDirAbs, err := filepath.Abs(inputDir)
	if err != nil {
		return err
	}
	outputDirAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}

	if _, err := os.Stat(inputDirAbs); err != nil {
		return fmt.Errorf("input directory %q not found", inputDirAbs)
	}
	if _, err := os.Stat(outputDirAbs); err != nil {
		return fmt.Errorf("output directory %q not found", outputDirAbs)
	}

	// Find CSV file
	csvFiles, err := filepath.Glob(filepath.Join(inputDirAbs, "*.csv"))
	if err != nil {
		return err
	}
	if len(csvFiles) == 0 {
		return fmt.Errorf("no CSV files found in %q", inputDirAbs)
	}
	if len(csvFiles) > 1 {
		return fmt.Errorf("multiple CSV files found in %q; expected exactly one", inputDirAbs)
	}
	inputCSV := csvFiles[0]

	feedName := filepath.Base(inputDirAbs)
	feedDir := filepath.Join(outputDirAbs, feedName)
	mediaDir := filepath.Join(feedDir, "items")

	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		return fmt.Errorf("create media dir: %w", err)
	}

	fmt.Printf("Input Dir:   %q\n", inputDirAbs)
	fmt.Printf("Output Dir:  %q\n", outputDirAbs)
	fmt.Printf("Feed Dir:    %q\n", feedDir)
	fmt.Printf("Media Dir:   %q\n", mediaDir)
	fmt.Printf("CSV:         %q\n\n", inputCSV)

	records, err := feedcsv.Read(inputCSV, delim)
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	localRecords := make([]feedcsv.Record, 0, len(records))

	for i, rec := range records {
		srcURL := rec.URL
		cleanURL := urlutil.StripQuery(srcURL)
		filename := filepath.Base(cleanURL)

		if !isSupportedAudioExt(filepath.Ext(filename)) {
			return fmt.Errorf("item %d (%q): unsupported extension in %q", i+1, rec.Title, filename)
		}

		encodedURL, _ := urlutil.Encode(srcURL)

		fmt.Printf("Item %d: %q\n  Downloading from: %s\n", i+1, rec.Title, encodedURL)

		dstPath := filepath.Join(mediaDir, filename)
		if err := downloadFile(encodedURL, dstPath); err != nil {
			return fmt.Errorf("download item %d (%q): %w", i+1, rec.Title, err)
		}

		localURL := "file://items/" + filename
		fmt.Printf("  Saved to: %q → local URL: %q\n\n", dstPath, localURL)

		localRecords = append(localRecords, feedcsv.Record{
			Title:       rec.Title,
			Description: rec.Description,
			Date:        rec.Date,
			URL:         localURL,
		})
	}

	// Write local.csv
	localCSVPath := filepath.Join(feedDir, "local.csv")
	if err := feedcsv.Write(localCSVPath, delim, localRecords); err != nil {
		return fmt.Errorf("write local.csv: %w", err)
	}

	// Copy original feed files to output (excluding cmd.sh)
	entries, err := os.ReadDir(inputDirAbs)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "cmd.sh" {
			continue
		}
		src := filepath.Join(inputDirAbs, entry.Name())
		dst := filepath.Join(feedDir, entry.Name())
		if err := copyFile(src, dst); err != nil {
			fmt.Printf("Warning: could not copy %q: %v\n", entry.Name(), err)
		}
	}

	fmt.Printf("Archive completed: %s\n", feedDir)
	return nil
}

func downloadFile(url, dstPath string) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
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
