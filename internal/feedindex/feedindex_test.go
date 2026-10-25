package feedindex_test

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/Stephan5/podcastinator/internal/feedindex"
)

var feeds = []feedindex.Feed{
	{
		Slug:          "example-show",
		Title:         "Example Show",
		Author:        "Tom, Dick & Harry",
		FeedURL:       "https://example.com/example-show/feed.xml",
		ImageURL:      "https://example.com/example-show/image.jpg",
		EpisodeCount:  2,
		LatestEpisode: "1998-06-06T16:52:00Z",
	},
	{
		Slug:         "another-show",
		Title:        "Bill & Ben",
		FeedURL:      "https://example.com/another-show/feed.xml",
		ImageURL:     "https://example.com/another-show/image.jpg",
		EpisodeCount: 1,
	},
}

func TestJSONRoundTrips(t *testing.T) {
	data, err := feedindex.JSON(feeds)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got feedindex.Index
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Feeds) != 2 || got.Feeds[0] != feeds[0] || got.Feeds[1] != feeds[1] {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got.Feeds, feeds)
	}
	if !strings.Contains(string(data), "Tom, Dick & Harry") {
		t.Errorf("expected & left unescaped, got:\n%s", data)
	}
}

func TestJSONEmptyIsArray(t *testing.T) {
	data, err := feedindex.JSON(nil)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(string(data), `"feeds": []`) {
		t.Errorf("expected empty feeds array, got %s", data)
	}
}

func TestOPML(t *testing.T) {
	data, err := feedindex.OPML("Podcasts", feeds)
	if err != nil {
		t.Fatalf("OPML: %v", err)
	}
	src := string(data)
	if !strings.HasPrefix(src, "<?xml") {
		t.Errorf("missing XML header:\n%s", src)
	}

	var doc struct {
		Version string `xml:"version,attr"`
		Title   string `xml:"head>title"`
		Outline []struct {
			Type   string `xml:"type,attr"`
			Text   string `xml:"text,attr"`
			XMLURL string `xml:"xmlUrl,attr"`
		} `xml:"body>outline"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal OPML: %v\n%s", err, src)
	}
	if doc.Version != "2.0" || doc.Title != "Podcasts" {
		t.Errorf("version/title = %q/%q", doc.Version, doc.Title)
	}
	if len(doc.Outline) != 2 {
		t.Fatalf("got %d outlines; want 2", len(doc.Outline))
	}
	if doc.Outline[1].Text != "Bill & Ben" || doc.Outline[1].XMLURL != feeds[1].FeedURL || doc.Outline[1].Type != "rss" {
		t.Errorf("outline = %+v", doc.Outline[1])
	}
}
