// Package feedrss handles building and parsing podcast RSS XML feeds.
package feedrss

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/Stephan5/podcastinator/internal/dateutil"
	"github.com/Stephan5/podcastinator/internal/feedcsv"
	"github.com/Stephan5/podcastinator/internal/urlutil"
)

const githubRepo = "Stephan5/podcastinator"

// DefaultConcurrency is how many Content-Length lookups Build makes at once
// when FeedOptions.Concurrency isn't set.
const DefaultConcurrency = 16

// httpClient makes the HEAD requests. The timeout stops one unresponsive host
// from hanging a build.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// FeedOptions contains podcast-level metadata for building an RSS feed.
type FeedOptions struct {
	Title       string
	Description string
	Author      string
	WebsiteURL  string
	ImageURL    string
	FeedURL     string
	RepoDir     string // subdirectory name under feed/

	// Type is the show's itunes:type, "episodic" or "serial"; empty leaves
	// it out, which apps treat as episodic.
	Type string

	// InputsHash, if set, is stamped into the feed (see StampInputs) so
	// publish can tell whether feed.xml is up to date with its inputs.
	InputsHash string

	// ContentLengthFetcher is called to retrieve the Content-Length for each episode
	// URL. If nil, an HTTP HEAD request is used. Override in tests.
	// It's called concurrently, so it must be safe for that.
	ContentLengthFetcher func(url string) (string, error)

	// Concurrency is how many Content-Length lookups run at once. Zero or
	// less means DefaultConcurrency.
	Concurrency int
}

// SetDefaults fills in the website link, which defaults to this tool's GitHub
// repo. FeedURL and ImageURL have no default: they're where the feed is hosted,
// which only the caller knows.
func (o *FeedOptions) SetDefaults() {
	if o.WebsiteURL == "" {
		o.WebsiteURL = "https://github.com/" + githubRepo
	}
}

// xmlEscapeText escapes only the characters required in XML text nodes: &, <, >.
// It intentionally does NOT escape ' or " (only needed in attribute values),
// matching the bash/xmllint output. It also HTML-decodes the input first so that
// values already stored as HTML entities in the CSV (e.g. "&amp;" in a title)
// are not double-encoded into "&amp;amp;".
func xmlEscapeText(s string) string {
	s = html.UnescapeString(s)
	var buf strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// xmlEscapeAttr escapes characters required in XML attribute values using the
// standard library (escapes &, <, >, ", '). Used for href/url attributes.
func xmlEscapeAttr(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s)) //nolint:errcheck
	return buf.String()
}

var rssTemplate = template.Must(template.New("rss").Funcs(template.FuncMap{
	"xmlEscape":     xmlEscapeText,
	"xmlEscapeAttr": xmlEscapeAttr,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:wfw="http://wellformedweb.org/CommentAPI/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:googleplay="http://www.google.com/schemas/play-podcasts/1.0" xmlns:spotify="http://www.spotify.com/ns/rss" xmlns:podcast="https://podcastindex.org/namespace/1.0" xmlns:media="http://search.yahoo.com/mrss/" version="2.0">
  <channel>
    <atom:link href="{{xmlEscapeAttr .FeedURL}}" rel="self" type="application/rss+xml"/>
    <title>{{xmlEscape .Title}}</title>
    <description>&lt;p&gt;{{xmlEscape .Description}} &lt;/p&gt;&lt;br/&gt;&lt;p&gt;Generated using {{.Repo}}.&lt;/p&gt;</description>
    {{- if .Author}}
    <itunes:author>{{xmlEscape .Author}}</itunes:author>
    {{- end}}
    {{- if .Type}}
    <itunes:type>{{.Type}}</itunes:type>
    {{- end}}
    <language>en-gb</language>
    <copyright>none</copyright>
    <link>{{xmlEscape .WebsiteURL}}</link>
    <image>
      <url>{{xmlEscape .ImageURL}}</url>
      <title>{{xmlEscape .Title}}</title>
      <link>{{xmlEscape .WebsiteURL}}</link>
    </image>
    <generator>{{.Repo}}</generator>
    <lastBuildDate>{{.BuildDate}}</lastBuildDate>
    <pubDate>{{.BuildDate}}</pubDate>
    {{- range .Items}}
    <item>
      <link>{{xmlEscapeAttr .Link}}</link>
      <guid{{if not .PermaLink}} isPermaLink="false"{{end}}>{{xmlEscapeAttr .GUID}}</guid>
      <title>{{xmlEscape .Title}}</title>
      <itunes:episode>{{.Number}}</itunes:episode>
      <description>{{.Description}}</description>
      <pubDate>{{.PubDate}}</pubDate>
      <enclosure url="{{xmlEscapeAttr .Link}}" length="{{.Length}}" type="{{.MimeType}}"/>
    </item>
    {{- end}}
  </channel>
</rss>
{{- if .InputsHash}}
<!-- podcastinator-inputs: {{.InputsHash}} -->
{{- end}}
`))

type rssTemplateData struct {
	Title       string
	Description string
	Author      string
	WebsiteURL  string
	ImageURL    string
	FeedURL     string
	Repo        string
	BuildDate   string
	Type        string
	InputsHash  string
	Items       []rssItem
}

type rssItem struct {
	Number      int
	Title       string
	Description string
	PubDate     string
	Link        string
	GUID        string
	PermaLink   bool
	Length      string
	MimeType    string
}

// Build generates RSS XML bytes from the given options and CSV records.
// It fetches Content-Length headers for each episode URL.
func Build(opts FeedOptions, records []feedcsv.Record, logger io.Writer) ([]byte, error) {
	if logger == nil {
		logger = io.Discard
	}
	if opts.FeedURL == "" || opts.ImageURL == "" {
		return nil, fmt.Errorf("feed URL and image URL are required")
	}

	repoLink := fmt.Sprintf("https://github.com/%s", githubRepo)
	buildDate := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")

	// Use injected fetcher or fall back to real HTTP HEAD.
	fetcher := opts.ContentLengthFetcher
	if fetcher == nil {
		fetcher = fetchContentLength
	}

	// Check every record before making any requests, so bad data fails fast.
	encodedURLs := make([]string, len(records))
	for i, rec := range records {
		num := i + 1
		if err := dateutil.ValidateRFC2822(rec.Date); err != nil {
			return nil, fmt.Errorf("item %d (%q): %w", num, rec.Title, err)
		}
		encodedURL, err := urlutil.Encode(rec.URL)
		if err != nil {
			return nil, fmt.Errorf("item %d: encode URL: %w", num, err)
		}
		encodedURLs[i] = encodedURL
	}

	if len(records) > 0 {
		fmt.Fprintf(logger, "Fetching Content-Length for %d episodes...\n\n", len(records))
	}
	lengths, err := fetchContentLengths(records, encodedURLs, fetcher, opts.Concurrency)
	if err != nil {
		return nil, err
	}

	items := make([]rssItem, 0, len(records))
	for i, rec := range records {
		num := i + 1
		fmt.Fprintf(logger, "Title: %q\nDate: %q\nLink: %q\n", rec.Title, rec.Date, rec.URL)

		encodedURL := encodedURLs[i]
		if encodedURL != rec.URL {
			fmt.Fprintf(logger, "URL encoded URL: %s\n", encodedURL)
		}

		length := lengths[i]
		if strings.TrimSpace(length) == "" {
			return nil, fmt.Errorf("item %d (%q): resolve URL %q for content-length: empty Content-Length", num, rec.Title, encodedURL)
		}
		fmt.Fprintf(logger, "Content-Length fetched: %q\n", length)

		// HTML encode URL for use in XML
		htmlURL := urlutil.HTMLEscape(encodedURL)
		fmt.Fprintf(logger, "HTML encoded URL: %s\n", htmlURL)

		// Build description — apply xmlEscapeText to both titles so any
		// raw & (e.g. from "Bill & Ben") becomes &amp; in the XML.
		desc := rec.Description
		if desc == "" {
			desc = fmt.Sprintf("%s - Episode %d of %s",
				xmlEscapeText(rec.Title), num, xmlEscapeText(opts.Title))
		}
		// Append "Generated using" link using literal HTML (not Go %q quoting)
		desc = fmt.Sprintf(`%s &lt;br/&gt;&lt;br/&gt;&lt;a href="%s" rel="nofollow noopener" target="_blank"&gt;Generated using %s&lt;/a&gt;`,
			desc, repoLink, githubRepo)

		// Detect MIME type
		cleanURL := urlutil.StripQuery(encodedURL)
		ext := ""
		if dotIdx := strings.LastIndex(cleanURL, "."); dotIdx != -1 {
			ext = cleanURL[dotIdx:]
		}
		mimeType := urlutil.MimeTypeForExtension(ext)

		// Episodes published before GUIDs were stored used their URL, and
		// keep it, written exactly as it was. Those are still URLs, so they
		// stay permalinks; anything else (a UUID) isn't one.
		guid := rec.GUID
		if guid == "" {
			guid = htmlURL
		}
		permaLink := strings.HasPrefix(guid, "http://") || strings.HasPrefix(guid, "https://")

		items = append(items, rssItem{
			Number:      num,
			Title:       fmt.Sprintf("%d: %s", num, rec.Title),
			Description: desc,
			PubDate:     rec.Date,
			Link:        htmlURL,
			GUID:        guid,
			PermaLink:   permaLink,
			Length:      length,
			MimeType:    mimeType,
		})
		fmt.Fprintln(logger)
	}

	data := rssTemplateData{
		Title:       opts.Title,
		Description: opts.Description,
		Author:      opts.Author,
		WebsiteURL:  opts.WebsiteURL,
		ImageURL:    opts.ImageURL,
		FeedURL:     opts.FeedURL,
		Repo:        githubRepo,
		BuildDate:   buildDate,
		Type:        opts.Type,
		InputsHash:  opts.InputsHash,
		Items:       items,
	}

	var buf bytes.Buffer
	if err := rssTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render RSS template: %w", err)
	}

	return buf.Bytes(), nil
}

// fetchContentLengths looks up every episode's Content-Length, up to
// concurrency at a time, and returns them in record order. Once a lookup fails
// no new ones start, and the error returned is the first record's that failed.
func fetchContentLengths(records []feedcsv.Record, urls []string, fetcher func(string) (string, error), concurrency int) ([]string, error) {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	lengths := make([]string, len(urls))
	errs := make([]error, len(urls))
	var failed atomic.Bool
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i, u := range urls {
		sem <- struct{}{}
		if failed.Load() {
			<-sem
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			lengths[i], errs[i] = fetcher(u)
			if errs[i] != nil {
				failed.Store(true)
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("item %d (%q): resolve URL %q for content-length: %w", i+1, records[i].Title, urls[i], err)
		}
	}
	return lengths, nil
}

// fetchContentLength performs a HEAD request and returns the Content-Length value.
func fetchContentLength(rawURL string) (string, error) {
	resp, err := httpClient.Head(rawURL) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	cl := resp.Header.Get("Content-Length")
	return strings.TrimSpace(cl), nil
}

// HashContent computes SHA-256 of the RSS XML bytes, excluding lastBuildDate and pubDate lines.
func HashContent(xmlBytes []byte) string {
	h := sha256.New()
	for _, line := range strings.Split(string(xmlBytes), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<lastBuildDate>") || strings.HasPrefix(trimmed, "<pubDate>") {
			continue
		}
		h.Write([]byte(line))
		h.Write([]byte("\n"))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

var inputsStamp = regexp.MustCompile(`<!-- podcastinator-inputs: ([0-9a-f]+) -->`)

// StampedInputs returns the inputs hash that Build stamped into a feed, or ""
// if it has none (a feed built before stamping, or by something else).
func StampedInputs(xmlBytes []byte) string {
	m := inputsStamp.FindSubmatch(xmlBytes)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// --- RSS Parsing (for import) ---

const itunesNS = "http://www.itunes.com/dtds/podcast-1.0.dtd"

type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string       `xml:"title"`
	Description string       `xml:"description"`
	Author      string       `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd author"`
	Links       []rssLink    `xml:"link"`
	Images      []rssImage   `xml:"image"`
	Items       []rssDocItem `xml:"item"`
}

// rssLink matches <link> in any namespace, so atom:link (whose text is
// empty) can be told apart from the channel's own <link>.
type rssLink struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

// rssImage matches both <image><url>…</url></image> and <itunes:image href="…"/>.
type rssImage struct {
	XMLName xml.Name
	URL     string `xml:"url"`
	Href    string `xml:"href,attr"`
}

type rssDocItem struct {
	Title       string       `xml:"title"`
	Description string       `xml:"description"`
	PubDate     string       `xml:"pubDate"`
	GUID        string       `xml:"guid"`
	Enclosure   rssEnclosure `xml:"enclosure"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

// ParsedFeed contains the parsed RSS feed data.
type ParsedFeed struct {
	Title       string
	Description string
	Author      string
	Link        string
	ImageURL    string
	Records     []feedcsv.Record
}

// Parse parses an RSS XML feed and returns records sorted by pubDate ascending.
func Parse(r io.Reader) (*ParsedFeed, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read RSS: %w", err)
	}

	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse RSS XML: %w", err)
	}

	ch := doc.Channel
	feed := &ParsedFeed{
		Title:       strings.TrimSpace(ch.Title),
		Description: strings.TrimSpace(ch.Description),
		Author:      strings.TrimSpace(ch.Author),
	}
	for _, l := range ch.Links {
		if l.XMLName.Space == "" && strings.TrimSpace(l.Value) != "" {
			feed.Link = strings.TrimSpace(l.Value)
			break
		}
	}
	// Prefer itunes:image, which Apple requires to be the full-size artwork.
	for _, img := range ch.Images {
		if img.XMLName.Space == itunesNS && img.Href != "" {
			feed.ImageURL = strings.TrimSpace(img.Href)
		}
	}
	for _, img := range ch.Images {
		if feed.ImageURL == "" && img.URL != "" {
			feed.ImageURL = strings.TrimSpace(img.URL)
		}
	}

	type itemWithTime struct {
		rec feedcsv.Record
		t   time.Time
	}
	var items []itemWithTime

	for _, item := range ch.Items {
		t, err := dateutil.ParseRFC2822(item.PubDate)
		if err != nil {
			return nil, fmt.Errorf("item %q: %w", item.Title, err)
		}

		// Only the enclosure is audio. An item's <link> is usually the
		// episode's web page, so an item without an enclosure gets no URL,
		// and import skips it.
		decodedURL := urlutil.HTMLUnescape(item.Enclosure.URL)

		items = append(items, itemWithTime{
			rec: feedcsv.Record{
				Title:       item.Title,
				Description: item.Description,
				Date:        item.PubDate,
				URL:         decodedURL,
				GUID:        strings.TrimSpace(item.GUID),
			},
			t: t,
		})
	}

	// Sort by date ascending
	sort.Slice(items, func(i, j int) bool {
		return items[i].t.Before(items[j].t)
	})

	for _, it := range items {
		feed.Records = append(feed.Records, it.rec)
	}

	return feed, nil
}
