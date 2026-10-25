// Package feedindex builds the machine-readable listings of all hosted feeds:
// index.json (for the website) and podcasts.opml (for importing every feed
// into a podcast app at once).
package feedindex

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"time"
)

const (
	JSONFilename = "index.json"
	OPMLFilename = "podcasts.opml"
)

// Feed is one podcast's entry in the index.
type Feed struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Author       string `json:"author,omitempty"`
	FeedURL      string `json:"feed_url"`
	ImageURL     string `json:"image_url"`
	EpisodeCount int    `json:"episode_count"`
	// LatestEpisode is the newest episode's publication date in RFC 3339, if any.
	LatestEpisode string `json:"latest_episode,omitempty"`
}

// Index is the document written to index.json.
type Index struct {
	Feeds []Feed `json:"feeds"`
}

// FormatDate formats an episode date for LatestEpisode.
func FormatDate(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// JSON renders the index as indented JSON. The output contains nothing that
// changes between runs, so an unchanged index is byte-for-byte identical.
func JSON(feeds []Feed) ([]byte, error) {
	if feeds == nil {
		feeds = []Feed{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Index{Feeds: feeds}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type opmlDoc struct {
	XMLName xml.Name      `xml:"opml"`
	Version string        `xml:"version,attr"`
	Title   string        `xml:"head>title"`
	Feeds   []opmlOutline `xml:"body>outline"`
}

type opmlOutline struct {
	Type    string `xml:"type,attr"`
	Text    string `xml:"text,attr"`
	Title   string `xml:"title,attr"`
	XMLURL  string `xml:"xmlUrl,attr"`
	HTMLURL string `xml:"htmlUrl,attr,omitempty"`
}

// OPML renders the feeds as an OPML 2.0 subscription list. Like JSON, it has
// no timestamps, so it only changes when the feeds do.
func OPML(title string, feeds []Feed) ([]byte, error) {
	doc := opmlDoc{Version: "2.0", Title: title}
	for _, f := range feeds {
		doc.Feeds = append(doc.Feeds, opmlOutline{Type: "rss", Text: f.Title, Title: f.Title, XMLURL: f.FeedURL})
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
