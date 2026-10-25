package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Stephan5/podcastinator/internal/dateutil"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/spf13/cobra"
)

func newPubdateCmd() *cobra.Command {
	var (
		inputFormat string
		delimiter   string
	)

	cmd := &cobra.Command{
		Use:   "pubdate <input.csv>",
		Short: "Reformat the date column in a CSV feed to RFC 2822",
		Long: `Reads a CSV feed file, parses the date column using the given strftime
input format, and rewrites the dates as RFC 2822 strings with time 03:00:00 GMT.

The original file is backed up with a .old extension.

Supported strftime specifiers: %Y %m %d %e %H %M %S %b %B %a %A %Z %T %p %I

Example:
  podcastinator pubdate feed.csv --input-format "%b %d, %Y"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPubdate(args[0], inputFormat, runeFromString(delimiter))
		},
	}

	cmd.Flags().StringVar(&inputFormat, "input-format", "%Y %m %d", "strftime format of the input date column")
	cmd.Flags().StringVar(&delimiter, "delimiter", string(rune(feedcsv.DefaultDelimiter)), "CSV field delimiter")

	return cmd
}

func runPubdate(inputFile, inputFormat string, delim rune) error {
	inputAbs, err := filepath.Abs(inputFile)
	if err != nil {
		return err
	}
	if _, err := os.Stat(inputAbs); err != nil {
		return fmt.Errorf("input file %q not found", inputAbs)
	}

	fmt.Printf("Input File:    %q\n", inputAbs)
	fmt.Printf("Input Format:  %q\n", inputFormat)
	fmt.Println()

	header, rows, err := feedcsv.ReadRaw(inputAbs, delim)
	if err != nil {
		return err
	}

	// Find date column index
	dateIdx := -1
	for i, col := range header {
		if strings.EqualFold(col, "date") {
			dateIdx = i
			break
		}
	}
	if dateIdx == -1 {
		dateIdx = 2 // default: 3rd column (0-indexed)
	}

	updated := make([][]string, 0, len(rows))
	for _, row := range rows {
		if len(row) <= dateIdx {
			updated = append(updated, row)
			continue
		}
		rawDate := row[dateIdx]
		reformatted, err := dateutil.ReformatDate(rawDate, inputFormat)
		if err != nil {
			return fmt.Errorf("reformat date %q: %w", rawDate, err)
		}
		fmt.Printf("  %q → %q\n", rawDate, reformatted)
		newRow := make([]string, len(row))
		copy(newRow, row)
		newRow[dateIdx] = reformatted
		updated = append(updated, newRow)
	}

	// Backup original
	backupPath := inputAbs + ".old"
	if err := os.Rename(inputAbs, backupPath); err != nil {
		return fmt.Errorf("backup original: %w", err)
	}

	if err := feedcsv.WriteRaw(inputAbs, delim, header, updated); err != nil {
		// Try to restore backup
		os.Rename(backupPath, inputAbs) //nolint:errcheck
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Printf("\nDone. Original backed up to: %s\n", backupPath)
	return nil
}
