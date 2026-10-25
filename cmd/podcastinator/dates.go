package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Stephan5/podcastinator/internal/dateutil"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/spf13/cobra"
)

func newDatesCmd() *cobra.Command {
	var from string

	cmd := &cobra.Command{
		Use:     "dates <feed> --from <format>",
		GroupID: "generate",
		Short:   "Rewrite a feed's episode dates as RFC 2822",
		Long: `Parses the date column of <feed>/feed.csv with a strftime format and
rewrites it as RFC 2822, which build requires, at 03:00:00 GMT. For CSVs
written by hand; imported feeds already have RFC 2822 dates.

The original is backed up to feed.csv.old.

Supported strftime specifiers: %Y %m %d %e %H %M %S %b %B %a %A %Z %T %p %I

Example:
  podcastinator dates example-show --from "%b %d, %Y"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedDirAbs, err := resolveFeedDir()
			if err != nil {
				return err
			}
			f, err := loadFeed(feedDirAbs, args[0])
			if err != nil {
				return err
			}
			return runDates(f, from)
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "strftime format of the existing dates")
	cmd.MarkFlagRequired("from") //nolint:errcheck
	return cmd
}

func runDates(f feed, inputFormat string) error {
	path := f.csvPath()
	delim := f.cfg.DelimiterRune()

	header, rows, err := feedcsv.ReadRaw(path, delim)
	if err != nil {
		return err
	}

	dateIdx := 2 // the third column, unless the header says otherwise
	for i, col := range header {
		if strings.EqualFold(col, "date") {
			dateIdx = i
			break
		}
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
		newRow := append([]string(nil), row...)
		newRow[dateIdx] = reformatted
		updated = append(updated, newRow)
	}

	backupPath := path + ".old"
	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("back up original: %w", err)
	}
	if err := feedcsv.WriteRaw(path, delim, header, updated); err != nil {
		os.Rename(backupPath, path) //nolint:errcheck
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Printf("\nDone. Original backed up to: %s\n", backupPath)
	return nil
}
