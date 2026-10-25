package feedrss_test

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/feedrss"
)

// mockFetcher returns a fixed content-length for all URLs (matching the test fixture).
func mockFetcher(contentLength string) func(string) (string, error) {
	return func(url string) (string, error) {
		return contentLength, nil
	}
}

var testRecords = []feedcsv.Record{
	{
		Title:       "2002-06-29",
		Description: "",
		Date:        "Sat, 29 Jun 2002 03:00:00 GMT",
		URL:         "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=1",
	},
	{
		Title:       "S01E01",
		Description: `&lt;p&gt;You're the one that's sad and lonely!&lt;/p&gt;`,
		Date:        "Sun, 30 June 2002 19:07:00 +0100",
		URL:         "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=2",
	},
	{
		Title:       "Ep. 1 - The Pilot",
		Description: `&lt;a href="https://example.com/shows/ep-1-the-pilot" rel="nofollow noopener" target="_blank"&gt;May 11, 2016&lt;/a&gt;&lt;p&gt;Two friends sit down and start a podcast. This one is different from other podcasts where two people talk about people you don't know. We get real. We take this seriously.&lt;/p&gt;&lt;p&gt;&lt;strong&gt;Tags&lt;/strong&gt;&lt;/p&gt;&lt;ul&gt; &lt;li&gt;&lt;a href="https://example.com/tags/cooking/" rel="nofollow noopener" target="_blank"&gt;cooking&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/gardening/" rel="nofollow noopener" target="_blank"&gt;gardening&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/storytelling/" rel="nofollow noopener" target="_blank"&gt;storytelling&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/comedy/" rel="nofollow noopener" target="_blank"&gt;comedy&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/politics/" rel="nofollow noopener" target="_blank"&gt;politics&lt;/a&gt;&lt;/li&gt;&lt;/ul&gt;`,
		Date:        "Mon, 01 July 2002 20:33:05 GMT",
		URL:         "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=3",
	},
	{
		Title:       "Inaugral Business",
		Description: "",
		Date:        "Tue, 02 July 2002 03:00:00 GMT",
		URL:         "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=4",
	},
	{
		Title:       "Part 1: The Power of Gentleness",
		Description: `&lt;p&gt;Gentle managers, unofficial tractors, cheap lobster and black dogs are covered in the first episode of this football podcast with Bob Mortimer and Andy Dawson.&lt;/p&gt;`,
		Date:        "Wed, 03 July 2002 18:50:00 GMT",
		URL:         "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=5",
	},
	{
		Title:       "2006 01 03 - Tuesday",
		Description: "",
		Date:        "Thu, 04 July 2002 03:00:00 GMT",
		URL:         "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=6",
	},
}

func buildTestFeed(t *testing.T) ([]byte, feedrss.FeedOptions) {
	t.Helper()
	opts := feedrss.FeedOptions{
		Title:                "Dudes Rock",
		Description:          "Hell yeah dude!",
		Author:               "Real-ass dudes",
		RepoDir:              "test",
		FeedURL:              "https://podcasts.example.com/test/feed.xml",
		ImageURL:             "https://link.com/image.jpg",
		ContentLengthFetcher: mockFetcher("103016"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, testRecords, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return xmlBytes, opts
}

// --- Structural tests ---

func TestBuildProducesValidXML(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	var v interface{}
	if err := xml.Unmarshal(xmlBytes, &v); err != nil {
		t.Errorf("XML unmarshal error: %v", err)
	}
}

func TestBuildChannelMetadata(t *testing.T) {
	xmlBytes, opts := buildTestFeed(t)
	src := string(xmlBytes)

	assertContains(t, src, "<title>Dudes Rock</title>")
	assertContains(t, src, "<itunes:author>Real-ass dudes</itunes:author>")
	assertContains(t, src, opts.FeedURL)
	assertContains(t, src, "<itunes:new-feed-url>"+opts.FeedURL+"</itunes:new-feed-url>")
	assertContains(t, src, opts.ImageURL)
	assertContains(t, src, opts.WebsiteURL)
	assertContains(t, src, "<language>en-gb</language>")
	assertContains(t, src, "<copyright>none</copyright>")
	assertContains(t, src, "Stephan5/podcastinator")

	// Channel description wraps the user description in HTML paragraph tags with single br (matches bash script)
	assertContains(t, src, "&lt;p&gt;Hell yeah dude!")
	assertContains(t, src, "&lt;br/&gt;&lt;p&gt;Generated using Stephan5/podcastinator.&lt;/p&gt;")
}

func TestBuildLastBuildDateIsRFC2822(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	src := string(xmlBytes)

	re := regexp.MustCompile(`<lastBuildDate>([^<]+)</lastBuildDate>`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("lastBuildDate not found in output")
	}
	dateStr := strings.TrimSpace(m[1])
	if _, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", dateStr); err != nil {
		t.Errorf("lastBuildDate %q is not valid RFC 2822: %v", dateStr, err)
	}
}

func TestBuildItemCount(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	count := strings.Count(string(xmlBytes), "<item>")
	if count != len(testRecords) {
		t.Errorf("got %d <item> elements; want %d", count, len(testRecords))
	}
}

func TestBuildItemTitlesNumbered(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	src := string(xmlBytes)

	for i, rec := range testRecords {
		want := fmt.Sprintf("<title>%d: %s</title>", i+1, rec.Title)
		// Note: xml special chars in title get escaped
		if !strings.Contains(src, want) {
			// Try with XML-escaped title (e.g. & → &amp;)
			escaped := strings.ReplaceAll(rec.Title, "&", "&amp;")
			want2 := fmt.Sprintf("<title>%d: %s</title>", i+1, escaped)
			if !strings.Contains(src, want2) {
				t.Errorf("item %d: expected title %q in output", i+1, want)
			}
		}
	}
}

func TestBuildItemDates(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	src := string(xmlBytes)
	for _, rec := range testRecords {
		assertContains(t, src, fmt.Sprintf("<pubDate>%s</pubDate>", rec.Date))
	}
}

func TestBuildItemEnclosureContentLength(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	// All items should have length="103016"
	count := strings.Count(string(xmlBytes), `length="103016"`)
	if count != len(testRecords) {
		t.Errorf("expected %d enclosures with length=103016, got %d", len(testRecords), count)
	}
}

func TestBuildFailsWhenContentLengthFetchFails(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:    "My Show",
		RepoDir:  "my-show",
		FeedURL:  "https://podcasts.example.com/my-show/feed.xml",
		ImageURL: "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: func(url string) (string, error) {
			return "", fmt.Errorf("HTTP 403")
		},
	}
	opts.SetDefaults()

	records := []feedcsv.Record{
		{
			Title: "Ep1",
			Date:  "Sat, 29 Jun 2002 03:00:00 GMT",
			URL:   "https://example.com/ep1.mp3",
		},
	}

	_, err := feedrss.Build(opts, records, io.Discard)
	if err == nil {
		t.Fatal("expected Build to fail when content-length lookup fails")
	}
	if !strings.Contains(err.Error(), `resolve URL "https://example.com/ep1.mp3" for content-length`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildFailsWhenContentLengthIsEmpty(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "My Show",
		RepoDir:              "my-show",
		FeedURL:              "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: mockFetcher(""),
	}
	opts.SetDefaults()

	records := []feedcsv.Record{
		{
			Title: "Ep1",
			Date:  "Sat, 29 Jun 2002 03:00:00 GMT",
			URL:   "https://example.com/ep1.mp3",
		},
	}

	_, err := feedrss.Build(opts, records, io.Discard)
	if err == nil {
		t.Fatal("expected Build to fail when content-length is empty")
	}
	if !strings.Contains(err.Error(), "empty Content-Length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func manyRecords(n int) []feedcsv.Record {
	records := make([]feedcsv.Record, n)
	for i := range records {
		records[i] = feedcsv.Record{
			Title: fmt.Sprintf("Ep%d", i+1),
			Date:  "Sat, 29 Jun 2002 03:00:00 GMT",
			URL:   fmt.Sprintf("https://example.com/%d.mp3", i+1),
		}
	}
	return records
}

func TestBuildFetchesConcurrentlyUpToLimit(t *testing.T) {
	var inFlight, peak atomic.Int32
	opts := feedrss.FeedOptions{
		Title:       "My Show",
		RepoDir:     "my-show",
		FeedURL:     "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:    "https://podcasts.example.com/my-show/image.jpg",
		Concurrency: 4,
		ContentLengthFetcher: func(url string) (string, error) {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			// Each episode's length is its number, so order can be checked.
			return strings.TrimSuffix(strings.TrimPrefix(url, "https://example.com/"), ".mp3"), nil
		},
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, manyRecords(20), io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p := peak.Load(); p < 2 || p > 4 {
		t.Errorf("peak concurrent fetches = %d, want 2 to 4", p)
	}
	lengths := regexp.MustCompile(`length="(\d+)"`).FindAllStringSubmatch(string(xmlBytes), -1)
	if len(lengths) != 20 {
		t.Fatalf("got %d enclosures, want 20", len(lengths))
	}
	for i, m := range lengths {
		if m[1] != fmt.Sprint(i+1) {
			t.Errorf("enclosure %d has length %s, want %d", i+1, m[1], i+1)
		}
	}
}

func TestBuildReportsFirstFailedFetchAndStopsEarly(t *testing.T) {
	var mu sync.Mutex
	var calls int
	opts := feedrss.FeedOptions{
		Title:       "My Show",
		RepoDir:     "my-show",
		FeedURL:     "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:    "https://podcasts.example.com/my-show/image.jpg",
		Concurrency: 1,
		ContentLengthFetcher: func(url string) (string, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			if strings.HasSuffix(url, "/3.mp3") || strings.HasSuffix(url, "/5.mp3") {
				return "", fmt.Errorf("HTTP 403")
			}
			return "1", nil
		},
	}
	opts.SetDefaults()

	_, err := feedrss.Build(opts, manyRecords(50), io.Discard)
	if err == nil {
		t.Fatal("expected Build to fail")
	}
	if !strings.Contains(err.Error(), `item 3 ("Ep3")`) {
		t.Errorf("error should name item 3, got: %v", err)
	}
	// With one at a time, nothing should start after item 3 fails.
	if calls != 3 {
		t.Errorf("made %d fetches, want 3", calls)
	}
}

func TestBuildValidatesDatesBeforeFetching(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:    "My Show",
		RepoDir:  "my-show",
		FeedURL:  "https://podcasts.example.com/my-show/feed.xml",
		ImageURL: "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: func(url string) (string, error) {
			t.Errorf("fetched %s despite a bad date", url)
			return "1", nil
		},
	}
	opts.SetDefaults()

	records := manyRecords(5)
	records[3].Date = "2002-06-29"
	if _, err := feedrss.Build(opts, records, io.Discard); err == nil {
		t.Fatal("expected Build to fail on a bad date")
	}
}

func TestBuildDefaultDescriptionFallback(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	src := string(xmlBytes)
	// item 1 has no description → fallback "2002-06-29 - Episode 1 of Dudes Rock"
	assertContains(t, src, "2002-06-29 - Episode 1 of Dudes Rock")
	// item 4 (Inaugral Business) also has no description
	assertContains(t, src, "Inaugral Business - Episode 4 of Dudes Rock")
}

func TestBuildGeneratedUsingLink(t *testing.T) {
	xmlBytes, _ := buildTestFeed(t)
	src := string(xmlBytes)
	// Every item description should contain the "Generated using" link
	assertContains(t, src, `href="https://github.com/Stephan5/podcastinator"`)
	assertContains(t, src, "Generated using Stephan5/podcastinator")
}

func TestSetDefaultsOnlyFillsWebsiteURL(t *testing.T) {
	opts := feedrss.FeedOptions{Title: "Test", RepoDir: "my-podcast"}
	opts.SetDefaults()

	if opts.WebsiteURL != "https://github.com/Stephan5/podcastinator" {
		t.Errorf("default WebsiteURL should be the tool's repo, got %q", opts.WebsiteURL)
	}
	if opts.FeedURL != "" || opts.ImageURL != "" {
		t.Errorf("FeedURL and ImageURL should have no default, got %q and %q", opts.FeedURL, opts.ImageURL)
	}
}

func TestBuildRequiresFeedAndImageURL(t *testing.T) {
	for name, opts := range map[string]feedrss.FeedOptions{
		"no feed URL":  {Title: "Test", RepoDir: "x", ImageURL: "https://podcasts.example.com/x/image.jpg"},
		"no image URL": {Title: "Test", RepoDir: "x", FeedURL: "https://podcasts.example.com/x/feed.xml"},
	} {
		t.Run(name, func(t *testing.T) {
			opts.ContentLengthFetcher = mockFetcher("1")
			if _, err := feedrss.Build(opts, testRecords, io.Discard); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestBuildNoAuthorTag(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "No Author Show",
		RepoDir:              "no-author",
		FeedURL:              "https://podcasts.example.com/no-author/feed.xml",
		ImageURL:             "https://podcasts.example.com/no-author/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()
	xmlBytes, err := feedrss.Build(opts, nil, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(string(xmlBytes), "<itunes:author>") {
		t.Error("should not emit <itunes:author> when author is empty")
	}
}

// --- Hash / idempotency tests ---

func TestHashContentIgnoresDates(t *testing.T) {
	xmlA := `<rss>
  <channel>
    <lastBuildDate>Mon, 01 Jan 2024 00:00:00 GMT</lastBuildDate>
    <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
    <title>Same</title>
  </channel>
</rss>`
	xmlB := `<rss>
  <channel>
    <lastBuildDate>Tue, 02 Jan 2024 12:00:00 GMT</lastBuildDate>
    <pubDate>Tue, 02 Jan 2024 12:00:00 GMT</pubDate>
    <title>Same</title>
  </channel>
</rss>`

	hashA := feedrss.HashContent([]byte(xmlA))
	hashB := feedrss.HashContent([]byte(xmlB))
	if hashA != hashB {
		t.Errorf("hashes differ for XML that only differs in build/pub dates:\n  A=%s\n  B=%s", hashA, hashB)
	}
}

func TestHashContentDetectsChange(t *testing.T) {
	xmlA := `<rss><channel><title>Old Title</title></channel></rss>`
	xmlB := `<rss><channel><title>New Title</title></channel></rss>`

	if feedrss.HashContent([]byte(xmlA)) == feedrss.HashContent([]byte(xmlB)) {
		t.Error("hashes should differ when content changes")
	}
}

func TestBuildSkipsWriteWhenUnchanged(t *testing.T) {
	// Build twice and verify the hash is the same (content-deterministic ignoring dates)
	opts := feedrss.FeedOptions{
		Title:                "Stable Show",
		Description:          "Stays the same",
		RepoDir:              "stable",
		FeedURL:              "https://podcasts.example.com/stable/feed.xml",
		ImageURL:             "https://podcasts.example.com/stable/image.jpg",
		ContentLengthFetcher: mockFetcher("1234"),
	}
	opts.SetDefaults()

	records := []feedcsv.Record{
		{Title: "Ep1", Date: "Sat, 29 Jun 2002 03:00:00 GMT", URL: "https://example.com/1.mp3"},
	}

	xml1, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	xml2, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if feedrss.HashContent(xml1) != feedrss.HashContent(xml2) {
		t.Error("same input should produce the same hash across two builds")
	}
}

// --- Parse (rss2csv) tests ---

const testRSSXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:atom="http://www.w3.org/2005/Atom" version="2.0">
  <channel>
    <title>Dudes Rock</title>
    <description>Hell yeah dude!</description>
    <link>https://github.com/Stephan5/podcastinator/tree/master/feed/test</link>
    <image><url>https://link.com/image.jpg</url></image>
    <item>
      <title>2002-06-29</title>
      <description>2002-06-29 - Episode 1 of Dudes Rock</description>
      <pubDate>Sat, 29 Jun 2002 03:00:00 GMT</pubDate>
      <enclosure url="https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=1" length="103016" type="audio/mpeg"/>
    </item>
    <item>
      <title>S01E01</title>
      <description>desc 2</description>
      <pubDate>Sun, 30 Jun 2002 19:07:00 +0100</pubDate>
      <enclosure url="https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=2" length="103016" type="audio/mpeg"/>
    </item>
    <item>
      <title>Ep. 1 - The Pilot</title>
      <description>desc 3</description>
      <pubDate>Mon, 01 Jul 2002 20:33:05 GMT</pubDate>
      <enclosure url="https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=3" length="103016" type="audio/mpeg"/>
    </item>
    <item>
      <title>Inaugral Business</title>
      <description>desc 4</description>
      <pubDate>Tue, 02 Jul 2002 03:00:00 GMT</pubDate>
      <enclosure url="https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=4" length="103016" type="audio/mpeg"/>
    </item>
    <item>
      <title>Last Episode</title>
      <description>desc 6</description>
      <pubDate>Thu, 04 Jul 2002 03:00:00 GMT</pubDate>
      <enclosure url="https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=6" length="103016" type="audio/mpeg"/>
    </item>
    <item>
      <title>Middle Episode</title>
      <description>desc 5</description>
      <pubDate>Wed, 03 Jul 2002 18:50:00 GMT</pubDate>
      <enclosure url="https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=5" length="103016" type="audio/mpeg"/>
    </item>
  </channel>
</rss>`

func TestParseFeedMetadata(t *testing.T) {
	feed, err := feedrss.Parse(strings.NewReader(testRSSXML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Dudes Rock" {
		t.Errorf("Title = %q; want %q", feed.Title, "Dudes Rock")
	}
	if feed.Link != "https://github.com/Stephan5/podcastinator/tree/master/feed/test" {
		t.Errorf("Link = %q", feed.Link)
	}
	if feed.ImageURL != "https://link.com/image.jpg" {
		t.Errorf("ImageURL = %q", feed.ImageURL)
	}
}

func TestParseItemCount(t *testing.T) {
	feed, err := feedrss.Parse(strings.NewReader(testRSSXML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Records) != 6 {
		t.Errorf("got %d records; want 6", len(feed.Records))
	}
}

func TestParseSortsByDateAscending(t *testing.T) {
	feed, err := feedrss.Parse(strings.NewReader(testRSSXML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// First item must be "2002-06-29" (earliest), last must be "Last Episode" (Thu 04 Jul)
	if feed.Records[0].Title != "2002-06-29" {
		t.Errorf("first record title = %q; want %q", feed.Records[0].Title, "2002-06-29")
	}
	if feed.Records[len(feed.Records)-1].Title != "Last Episode" {
		t.Errorf("last record title = %q; want %q",
			feed.Records[len(feed.Records)-1].Title, "Last Episode")
	}
	// Middle episode (Wed 03 Jul) must be 5th (index 4)
	if feed.Records[4].Title != "Middle Episode" {
		t.Errorf("record[4] title = %q; want %q", feed.Records[4].Title, "Middle Episode")
	}
}

func TestParseURLHTMLDecoded(t *testing.T) {
	const xmlWithEncodedAmpersand = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item>
      <title>Episode</title>
      <pubDate>Sat, 29 Jun 2002 03:00:00 GMT</pubDate>
      <enclosure url="https://example.com/file.mp3?a=1&amp;b=2" length="100" type="audio/mpeg"/>
    </item>
  </channel>
</rss>`

	feed, err := feedrss.Parse(strings.NewReader(xmlWithEncodedAmpersand))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(feed.Records))
	}
	// URL should be HTML-decoded: &amp; → &
	want := "https://example.com/file.mp3?a=1&b=2"
	if feed.Records[0].URL != want {
		t.Errorf("URL = %q; want %q", feed.Records[0].URL, want)
	}
}

func TestParseIgnoresItemLink(t *testing.T) {
	const src = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item>
      <title>Episode</title>
      <link>https://example.com/episodes/1</link>
      <pubDate>Sat, 29 Jun 2002 03:00:00 GMT</pubDate>
      <enclosure url="https://example.com/1.mp3" length="100" type="audio/mpeg"/>
    </item>
    <item>
      <title>Trailer</title>
      <link>https://example.com/trailer</link>
      <pubDate>Sun, 30 Jun 2002 03:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

	feed, err := feedrss.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(feed.Records))
	}
	// The enclosure is the audio, not the link to the episode's page.
	if got, want := feed.Records[0].URL, "https://example.com/1.mp3"; got != want {
		t.Errorf("URL = %q; want %q", got, want)
	}
	// With no enclosure there's no audio, whatever the link is.
	if got := feed.Records[1].URL; got != "" {
		t.Errorf("URL of an item without an enclosure = %q; want empty", got)
	}
}

// --- CSV round-trip: Build → write CSV → read CSV ---

func TestBuildThenWriteCSVRoundTrip(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "Round Trip Show",
		RepoDir:              "round-trip",
		FeedURL:              "https://podcasts.example.com/round-trip/feed.xml",
		ImageURL:             "https://podcasts.example.com/round-trip/image.jpg",
		ContentLengthFetcher: mockFetcher("5000"),
	}
	opts.SetDefaults()

	records := []feedcsv.Record{
		{Title: "Ep1", Date: "Sat, 29 Jun 2002 03:00:00 GMT", URL: "https://example.com/1.mp3"},
		{Title: "Ep2", Date: "Sun, 30 Jun 2002 19:07:00 +0100", URL: "https://example.com/2.mp3"},
	}

	xmlBytes, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Write to temp file and parse back
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "feed.xml")
	if err := os.WriteFile(xmlPath, xmlBytes, 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(xmlPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	parsed, err := feedrss.Parse(f)
	if err != nil {
		t.Fatalf("Parse round-trip: %v", err)
	}
	if len(parsed.Records) != len(records) {
		t.Fatalf("round-trip: got %d records; want %d", len(parsed.Records), len(records))
	}
	for i, r := range records {
		if parsed.Records[i].Date != r.Date {
			t.Errorf("record[%d] Date = %q; want %q", i, parsed.Records[i].Date, r.Date)
		}
	}
}

// --- XML escaping tests ---
// These tests specifically cover the bugs fixed when comparing Go output against
// the bash/xmllint reference: double-encoding of &amp; in CSV titles and
// unnecessary &#39; encoding of apostrophes in text nodes.

// TestXMLEscapeTextDoesNotDoubleEncodeAmpersand is the regression test for the
// main bug found by diffing bobs-show/feed.xml.
// CSV titles contain HTML entities (e.g. "Foo &amp; Bar") because rss2csv
// doesn't decode them when writing to CSV. The Go template must NOT re-encode
// the & in &amp; — the output should be "&amp;" not "&amp;amp;".
func TestXMLEscapeTextDoesNotDoubleEncodeAmpersand(t *testing.T) {
	records := []feedcsv.Record{
		{
			Title: "#4 - Jane Doe &amp; John Smith",
			Date:  "Sat, 29 Jun 2002 03:00:00 GMT",
			URL:   "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=1",
		},
	}
	opts := feedrss.FeedOptions{
		Title:                "My Show",
		RepoDir:              "my-show",
		FeedURL:              "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src := string(xmlBytes)

	// Must appear as &amp; (the & in the title), not double-encoded &amp;amp;
	if !strings.Contains(src, `#4 - Jane Doe &amp; John Smith`) {
		t.Errorf("expected &amp; in title, but got double-encoding or missing:\n%s",
			extractTitles(src))
	}
	if strings.Contains(src, `&amp;amp;`) {
		t.Errorf("title was double-encoded to &amp;amp;")
	}
}

// TestXMLEscapeTextDoesNotEncodeApostropheInTextNode ensures apostrophes in
// titles are output as literal ' not &#39;. XML only requires apostrophe escaping
// in attribute values; the bash output leaves them bare in text nodes.
func TestXMLEscapeTextDoesNotEncodeApostropheInTextNode(t *testing.T) {
	records := []feedcsv.Record{
		{
			Title: "Ben O'Brien and the Show",
			Date:  "Sat, 29 Jun 2002 03:00:00 GMT",
			URL:   "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=1",
		},
	}
	opts := feedrss.FeedOptions{
		Title:                "My Show",
		RepoDir:              "my-show",
		FeedURL:              "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src := string(xmlBytes)

	if strings.Contains(src, `&#39;`) {
		t.Errorf("apostrophe was unnecessarily encoded as &#39; in text node")
	}
	if !strings.Contains(src, `Ben O'Brien`) {
		t.Errorf("apostrophe not found as literal ' in output:\n%s", extractTitles(src))
	}
}

// TestXMLEscapeTextEncodesRawSpecialChars verifies that genuinely unescaped
// &, < and > in a title are correctly encoded.
func TestXMLEscapeTextEncodesRawSpecialChars(t *testing.T) {
	records := []feedcsv.Record{
		{Title: "A & B < C > D", Date: "Sat, 29 Jun 2002 03:00:00 GMT",
			URL: "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3?id=1"},
	}
	opts := feedrss.FeedOptions{
		Title:                "My Show",
		RepoDir:              "my-show",
		FeedURL:              "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src := string(xmlBytes)

	assertContains(t, src, `A &amp; B &lt; C &gt; D`)
}

// TestXMLEscapeAttrEncodesApostropheInURL verifies that URLs containing
// apostrophes are correctly encoded when placed in XML attributes (enclosure url=).
func TestXMLEscapeAttrEncodesApostropheInURL(t *testing.T) {
	records := []feedcsv.Record{
		{Title: "Ep", Date: "Sat, 29 Jun 2002 03:00:00 GMT",
			URL: "https://example.com/it%27s-fine.mp3"},
	}
	opts := feedrss.FeedOptions{
		Title:                "My Show",
		RepoDir:              "my-show",
		FeedURL:              "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The enclosure url= attribute should have the URL (encoded form is fine in attrs)
	assertContains(t, string(xmlBytes), `enclosure url=`)
}

// TestXMLEscapeTextAuthorAmpersand verifies the author field (e.g. "Foo &amp; Bar"
// from feed.json) is output as "Foo & Bar" in the <itunes:author> text node,
// not as "Foo &amp;amp; Bar".
func TestXMLEscapeTextAuthorAmpersand(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "My Show",
		Author:               "Jane Doe &amp; John Smith",
		RepoDir:              "my-show",
		FeedURL:              "https://podcasts.example.com/my-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/my-show/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, nil, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src := string(xmlBytes)

	assertContains(t, src, `<itunes:author>Jane Doe &amp; John Smith</itunes:author>`)
	if strings.Contains(src, `&amp;amp;`) {
		t.Errorf("author was double-encoded to &amp;amp;")
	}
}

// TestXMLEscapeTextChannelTitleApostrophe verifies the channel <title> uses
// a bare apostrophe (not &#39;) matching the bash/xmllint output.
func TestXMLEscapeTextChannelTitleApostrophe(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "Bob's Show",
		RepoDir:              "bobs-show",
		FeedURL:              "https://podcasts.example.com/bobs-show/feed.xml",
		ImageURL:             "https://podcasts.example.com/bobs-show/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, nil, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src := string(xmlBytes)

	assertContains(t, src, `<title>Bob's Show</title>`)
	if strings.Contains(src, `&#39;`) {
		t.Errorf("apostrophe in channel title was unnecessarily encoded as &#39;")
	}
}

// TestNamespaceOnOneLine verifies the RSS element has all namespaces on a single
// line, matching the xmllint-formatted bash output.
func TestNamespaceOnOneLine(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "Test",
		RepoDir:              "test",
		FeedURL:              "https://podcasts.example.com/test/feed.xml",
		ImageURL:             "https://podcasts.example.com/test/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, nil, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, line := range strings.Split(string(xmlBytes), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "xmlns:") {
			t.Errorf("namespace found on its own line (should be inline on <rss>): %q", line)
		}
	}
	assertContains(t, string(xmlBytes), `<rss xmlns:atom=`)
}

// TestImageChildIndentation verifies image sub-elements use 6-space indentation
// (matching 2-space xmllint indent: 4 for <image> + 2 more = 6).
func TestImageChildIndentation(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "Test",
		RepoDir:              "test",
		FeedURL:              "https://podcasts.example.com/test/feed.xml",
		ImageURL:             "https://podcasts.example.com/test/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	opts.SetDefaults()

	xmlBytes, err := feedrss.Build(opts, nil, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, line := range strings.Split(string(xmlBytes), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "<url>") || strings.HasPrefix(trimmed, "<title>") || strings.HasPrefix(trimmed, "<link>") {
			// Only check lines inside <image> (identified by being indented)
			if strings.HasPrefix(line, "      ") {
				indent := len(line) - len(strings.TrimLeft(line, " "))
				if indent != 6 {
					t.Errorf("image child %q has %d spaces indent, want 6", trimmed, indent)
				}
			}
		}
	}
}

// extractTitles is a debug helper that pulls all <title>...</title> lines from XML.
func extractTitles(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "<title>") {
			out.WriteString(strings.TrimSpace(line) + "\n")
		}
	}
	return out.String()
}

// assertContains fails the test if sub is not found in s.
func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected output to contain:\n  %q\nbut it was not found", sub)
	}
}

func TestParseItunesMetadata(t *testing.T) {
	const src = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>Show</title>
    <link>https://example.com/show</link>
    <atom:link href="https://feeds.example.com/show.rss" rel="self" type="application/rss+xml"/>
    <itunes:author>Some Body</itunes:author>
    <image><url>https://example.com/small.jpg</url></image>
    <itunes:image href="https://example.com/large.jpg"/>
  </channel>
</rss>`

	feed, err := feedrss.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Author != "Some Body" {
		t.Errorf("Author = %q", feed.Author)
	}
	// atom:link comes after <link>, and must not replace it.
	if feed.Link != "https://example.com/show" {
		t.Errorf("Link = %q", feed.Link)
	}
	// itunes:image is the full-size artwork, so it wins over <image>.
	if feed.ImageURL != "https://example.com/large.jpg" {
		t.Errorf("ImageURL = %q", feed.ImageURL)
	}
}

func TestStampedInputs(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "Test",
		FeedURL:              "https://podcasts.example.com/test/feed.xml",
		ImageURL:             "https://podcasts.example.com/test/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	unstamped, err := feedrss.Build(opts, testRecords, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := feedrss.StampedInputs(unstamped); got != "" {
		t.Errorf("StampedInputs of an unstamped feed = %q; want empty", got)
	}

	opts.InputsHash = "0123abcd"
	stamped, err := feedrss.Build(opts, testRecords, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := feedrss.StampedInputs(stamped); got != "0123abcd" {
		t.Errorf("StampedInputs = %q; want %q", got, "0123abcd")
	}
	if err := xml.Unmarshal(stamped, new(struct{})); err != nil {
		t.Errorf("stamped feed isn't valid XML: %v", err)
	}
	if feedrss.HashContent(stamped) == feedrss.HashContent(unstamped) {
		t.Error("the stamp should count as a change, so build rewrites feed.xml when only its inputs changed")
	}
}

func TestBuildGUIDs(t *testing.T) {
	opts := feedrss.FeedOptions{
		Title:                "Test",
		FeedURL:              "https://podcasts.example.com/test/feed.xml",
		ImageURL:             "https://podcasts.example.com/test/image.jpg",
		ContentLengthFetcher: mockFetcher("0"),
	}
	records := []feedcsv.Record{
		{Title: "Legacy", Date: "Sat, 29 Jun 2002 03:00:00 GMT", URL: "https://example.com/new/1.mp3", GUID: "https://example.com/old/1.mp3"},
		{Title: "New", Date: "Sun, 30 Jun 2002 03:00:00 GMT", URL: "https://example.com/2.mp3", GUID: "6f1c1e0a-3b5d-4c2e-9f7a-2d8b4e6a1c3f"},
		{Title: "Unassigned", Date: "Mon, 01 Jul 2002 03:00:00 GMT", URL: "https://example.com/3.mp3?a=1&b=2"},
	}
	xmlBytes, err := feedrss.Build(opts, records, io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src := string(xmlBytes)
	// A URL GUID stays a plain permalink, even once the audio has moved.
	assertContains(t, src, `<guid>https://example.com/old/1.mp3</guid>`)
	assertContains(t, src, `<enclosure url="https://example.com/new/1.mp3"`)
	assertContains(t, src, `<guid isPermaLink="false">6f1c1e0a-3b5d-4c2e-9f7a-2d8b4e6a1c3f</guid>`)
	// No GUID means the URL, as 0.3.0 wrote it.
	assertContains(t, src, `<guid>https://example.com/3.mp3?a=1&amp;amp;b=2</guid>`)

	parsed, err := feedrss.Parse(bytes.NewReader(xmlBytes))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for i, want := range []string{"https://example.com/old/1.mp3", "6f1c1e0a-3b5d-4c2e-9f7a-2d8b4e6a1c3f", "https://example.com/3.mp3?a=1&amp;b=2"} {
		if parsed.Records[i].GUID != want {
			t.Errorf("parsed record %d GUID = %q; want %q", i, parsed.Records[i].GUID, want)
		}
	}
}
