// Package integration contains black-box integration tests for the podcastinator CLI.
// Tests build the binary once in TestMain and then invoke it as a subprocess,
// mirroring what the bash test suite does.
package integration

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// binary is the path to the compiled podcastinator binary, set in TestMain.
var binary string

func TestMain(m *testing.M) {
	// Build the binary into a temp dir so we always test the current code.
	tmp, err := os.MkdirTemp("", "podcastinator-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binary = filepath.Join(tmp, "podcastinator")
	out, err := exec.Command("go", "build", "-o", binary, "../cmd/podcastinator").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// run executes the binary with the given arguments and returns stdout+stderr combined.
func run(args ...string) (string, int) {
	cmd := exec.Command(binary, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = 1
		}
	}
	return string(out), code
}

// runInDir runs the binary from a specific working directory.
func runInDir(dir string, args ...string) (string, int) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = 1
		}
	}
	return string(out), code
}

// goldenAudioURL is the episode URL recorded in testdata/feed.xml.
const goldenAudioURL = "https://freetestdata.com/wp-content/uploads/2021/09/Free_Test_Data_100KB_MP3.mp3"

// audioServer serves testdata/test.mp3 at every path, so feed generation can
// fetch each episode's Content-Length without going to the internet.
func audioServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/test.mp3")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// writeHosting writes the hosting.json every feed directory needs for its
// feed and image URLs.
func writeHosting(t *testing.T, feedRoot string) {
	t.Helper()
	os.WriteFile(filepath.Join(feedRoot, "hosting.json"),
		[]byte(`{"bucket":"my-bucket","region":"eu-west-2","public_url":"https://podcasts.example.com"}`), 0644)
}

// writeFeed writes feedRoot/slug/feed.json and feed.csv, whose rows are
// joined with the unit separator.
func writeFeed(t *testing.T, feedRoot, slug, feedJSON string, rows ...string) string {
	t.Helper()
	dir := filepath.Join(feedRoot, slug)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "feed.json"), []byte(feedJSON), 0644)
	csv := "title\x1fdescription\x1fdate\x1furl\n"
	for _, row := range rows {
		csv += row + "\n"
	}
	os.WriteFile(filepath.Join(dir, "feed.csv"), []byte(csv), 0644)
	return dir
}

// episode is a feed.csv row with no description.
func episode(title, date, url string) string {
	return title + "\x1f\x1f" + date + "\x1f" + url
}

// --- build integration tests ---

// TestBuildGoldenSnapshot builds RSS from the test CSV and compares its parsed
// content against test/testdata/feed.xml.
// Comparison is semantic (title, date, URL per item) so XML formatting differences
// between the Go template and the legacy xmllint output do not cause false failures.
func TestBuildGoldenSnapshot(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)

	// The exact input CSV from the bash test (unit separator delimiter).
	delim := "\x1f"
	feedDir := writeFeed(t, feedRoot, "test", `{
		"title": "Dudes Rock",
		"description": "Hell yeah dude!",
		"author": "Real-ass dudes",
		"image": "https://link.com/image.jpg"
	}`,
		"2002-06-29"+delim+""+delim+"Sat, 29 Jun 2002 03:00:00 GMT"+delim+audio+"/ep.mp3?id=1",
		"S01E01"+delim+"&lt;p&gt;You're the one that's sad and lonely!&lt;/p&gt;"+delim+"Sun, 30 June 2002 19:07:00 +0100"+delim+audio+"/ep.mp3?id=2",
		"Ep. 1 - The Pilot"+delim+`&lt;a href="https://example.com/shows/ep-1-the-pilot" rel="nofollow noopener" target="_blank"&gt;May 11, 2016&lt;/a&gt;&lt;p&gt;Two friends sit down and start a podcast. This one is different from other podcasts where two people talk about people you don't know. We get real. We take this seriously.&lt;/p&gt;&lt;p&gt;&lt;strong&gt;Tags&lt;/strong&gt;&lt;/p&gt;&lt;ul&gt; &lt;li&gt;&lt;a href="https://example.com/tags/cooking/" rel="nofollow noopener" target="_blank"&gt;cooking&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/gardening/" rel="nofollow noopener" target="_blank"&gt;gardening&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/storytelling/" rel="nofollow noopener" target="_blank"&gt;storytelling&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/comedy/" rel="nofollow noopener" target="_blank"&gt;comedy&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/politics/" rel="nofollow noopener" target="_blank"&gt;politics&lt;/a&gt;&lt;/li&gt;&lt;/ul&gt;`+delim+"Mon, 01 July 2002 20:33:05 GMT"+delim+audio+"/ep.mp3?id=3",
		"Inaugral Business"+delim+""+delim+"Tue, 02 July 2002 03:00:00 GMT"+delim+audio+"/ep.mp3?id=4",
		"Part 1: The Power of Gentleness"+delim+"&lt;p&gt;Gentle managers, unofficial tractors, cheap lobster and black dogs are covered in the first episode of this football podcast with Bob Mortimer and Andy Dawson.&lt;/p&gt;"+delim+"Wed, 03 July 2002 18:50:00 GMT"+delim+audio+"/ep.mp3?id=5",
		"2006 01 03 - Tuesday"+delim+""+delim+"Thu, 04 July 2002 03:00:00 GMT"+delim+audio+"/ep.mp3?id=6",
	)

	out, code := run("build", "test", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}

	actualXML, err := os.ReadFile(filepath.Join(feedDir, "feed.xml"))
	if err != nil {
		t.Fatalf("output XML not found: %v", err)
	}
	goldenXML, err := os.ReadFile("testdata/feed.xml")
	if err != nil {
		t.Fatalf("golden file not found: %v", err)
	}

	goldenXML = []byte(strings.ReplaceAll(string(goldenXML), goldenAudioURL, audio+"/ep.mp3"))

	actual := parseXMLItems(t, actualXML)
	golden := parseXMLItems(t, goldenXML)

	// Channel-level checks.
	if actual.title != golden.title {
		t.Errorf("channel title: got %q; want %q", actual.title, golden.title)
	}
	if !strings.Contains(string(actualXML), "<itunes:author>Real-ass dudes</itunes:author>") {
		t.Errorf("author from feed.json not in output")
	}

	// Structural check: lastBuildDate and pubDate must be present and non-empty.
	if actual.lastBuildDate == "" {
		t.Error("lastBuildDate missing from output")
	}

	// Item-level checks.
	if len(actual.items) != len(golden.items) {
		t.Fatalf("item count: got %d; want %d", len(actual.items), len(golden.items))
	}
	for i := range golden.items {
		g, a := golden.items[i], actual.items[i]
		if a.title != g.title {
			t.Errorf("item[%d] title: got %q; want %q", i, a.title, g.title)
		}
		if a.pubDate != g.pubDate {
			t.Errorf("item[%d] pubDate: got %q; want %q", i, a.pubDate, g.pubDate)
		}
		if a.enclosureURL != g.enclosureURL {
			t.Errorf("item[%d] enclosureURL: got %q; want %q", i, a.enclosureURL, g.enclosureURL)
		}
		if a.description != g.description {
			t.Errorf("item[%d] description: got %q; want %q", i, a.description, g.description)
		}
	}
}

func TestBuildLastBuildDateFormat(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	feedDir := writeFeed(t, feedRoot, "myshow", `{"title":"My Show"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep.mp3?id=1"))

	out, code := run("build", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}

	xml, _ := os.ReadFile(filepath.Join(feedDir, "feed.xml"))
	re := regexp.MustCompile(`<lastBuildDate>([^<]+)</lastBuildDate>`)
	m := re.FindSubmatch(xml)
	if m == nil {
		t.Fatal("lastBuildDate not found in output")
	}
	// Must match RFC 2822 pattern
	dateStr := strings.TrimSpace(string(m[1]))
	if !regexp.MustCompile(`^[A-Z][a-z]{2}, \d{2} [A-Z][a-z]{2} \d{4} \d{2}:\d{2}:\d{2} GMT$`).MatchString(dateStr) {
		t.Errorf("lastBuildDate %q is not valid RFC 2822", dateStr)
	}
}

func TestBuildMissingTitleError(t *testing.T) {
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	writeFeed(t, feedRoot, "untitled", `{"author":"Someone"}`)

	out, code := run("build", "untitled", "--feed-dir", feedRoot)
	if code == 0 {
		t.Fatalf("expected build to fail when feed.json has no title:\n%s", out)
	}
	if !strings.Contains(out, `"title" is required`) {
		t.Errorf("expected missing title error:\n%s", out)
	}
}

func TestBuildRequiresFeedURL(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	feedDir := writeFeed(t, feedRoot, "unhosted", `{"title":"Unhosted"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep.mp3"))

	out, code := run("build", "unhosted", "--feed-dir", feedRoot)
	if code == 0 {
		t.Fatalf("expected build to fail without hosting.json or feed URLs:\n%s", out)
	}
	if !strings.Contains(out, "no feed URL or image URL") {
		t.Errorf("expected missing feed URL error:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(feedDir, "feed.xml")); err == nil {
		t.Error("feed.xml should not have been written")
	}

	// feed.json's URLs are enough on their own.
	os.WriteFile(filepath.Join(feedDir, "feed.json"), []byte(`{
		"title": "Unhosted",
		"feed": "https://podcasts.example.com/unhosted/feed.xml",
		"image": "https://podcasts.example.com/unhosted/image.jpg"
	}`), 0644)
	out, code = run("build", "unhosted", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build with feed and image in feed.json exited %d:\n%s", code, out)
	}
}

func TestBuildFailsWhenURLCannotBeResolved(t *testing.T) {
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	writeFeed(t, feedRoot, "broken", `{"title":"Broken Show"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", "http://127.0.0.1:1/ep1.mp3"))

	out, code := run("build", "broken", "--feed-dir", feedRoot)
	if code == 0 {
		t.Fatalf("expected build to fail when URL cannot be resolved")
	}
	if !strings.Contains(out, "resolve URL") {
		t.Fatalf("expected resolve URL error in output, got:\n%s", out)
	}
}

func TestBuildUsesHostingURLs(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	feedDir := writeFeed(t, feedRoot, "hosted-show", `{"title":"Hosted Show"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep1.mp3"))

	out, code := run("build", "hosted-show", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}
	xml, _ := os.ReadFile(filepath.Join(feedDir, "feed.xml"))
	src := string(xml)
	base := "https://podcasts.example.com/hosted-show/"
	for _, want := range []string{
		`<atom:link href="` + base + `feed.xml"`,
		`<url>` + base + `image.jpg</url>`,
		`<!-- podcastinator-inputs: `,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("expected %q in feed.xml:\n%s", want, src)
		}
	}
}

// TestBuildAllFeeds verifies that build with no feeds named builds every
// feed directory with a feed.json, and ignores directories without one.
func TestBuildAllFeeds(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)

	goodDir := writeFeed(t, feedRoot, "good-show", `{"title":"Good Show"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep.mp3?id=1"))

	noJSONDir := filepath.Join(feedRoot, "no-json")
	os.MkdirAll(noJSONDir, 0755)
	os.WriteFile(filepath.Join(noJSONDir, "feed.csv"), []byte("title\x1fdescription\x1fdate\x1furl\n"), 0644)

	out, code := run("build", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}

	if _, err := os.Stat(filepath.Join(goodDir, "feed.xml")); err != nil {
		t.Errorf("good-show/feed.xml not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(noJSONDir, "feed.xml")); err == nil {
		t.Error("no-json/feed.xml should not have been created")
	}
}

func TestBuildExclude(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	for _, name := range []string{"show-a", "show-b"} {
		writeFeed(t, feedRoot, name, `{"title":"`+name+`"}`,
			episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep.mp3?id=1"))
	}

	out, code := run("build", "--feed-dir", feedRoot, "--exclude", "show-b")
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}

	if _, err := os.Stat(filepath.Join(feedRoot, "show-a", "feed.xml")); err != nil {
		t.Error("show-a/feed.xml should have been created")
	}
	if _, err := os.Stat(filepath.Join(feedRoot, "show-b", "feed.xml")); err == nil {
		t.Error("show-b/feed.xml should not have been created (excluded)")
	}
}

func TestBuildNamedFeedsAndExcludeMutuallyExclusive(t *testing.T) {
	feedRoot := t.TempDir()
	writeFeed(t, feedRoot, "something", `{"title":"Something"}`)
	out, code := run("build", "something", "--exclude", "other", "--feed-dir", feedRoot)
	if code == 0 {
		t.Errorf("expected non-zero exit when feeds are named with --exclude:\n%s", out)
	}
}

func TestBuildUnknownFeed(t *testing.T) {
	feedRoot := t.TempDir()
	out, code := run("build", "nope", "--feed-dir", feedRoot)
	if code == 0 || !strings.Contains(out, `no feed "nope"`) {
		t.Errorf("expected unknown feed error, exit %d:\n%s", code, out)
	}
}

func TestDefaultFeedDir(t *testing.T) {
	root := t.TempDir()
	out, code := runInDir(root, "build")
	if code == 0 || !strings.Contains(out, "podcastinator init") {
		t.Errorf("expected missing ./feed to suggest init, exit %d:\n%s", code, out)
	}

	out, code = runInDir(root, "init")
	if code != 0 {
		t.Fatalf("init exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "feed")); err != nil {
		t.Fatalf("init didn't create ./feed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "feed", "hosting.json")); err == nil {
		t.Error("init without --bucket shouldn't write hosting.json")
	}
	if out, code := runInDir(root, "build"); code != 0 {
		t.Errorf("build in an empty ./feed exited %d:\n%s", code, out)
	}
}

func TestInitWritesHosting(t *testing.T) {
	feedRoot := filepath.Join(t.TempDir(), "feeds")
	out, code := run("init", "--feed-dir", feedRoot, "--bucket", "my-bucket", "--public-url", "https://podcasts.example.com/")
	if code != 0 {
		t.Fatalf("init exited %d:\n%s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(feedRoot, "hosting.json"))
	if err != nil {
		t.Fatalf("hosting.json not written: %v", err)
	}
	for _, want := range []string{`"bucket": "my-bucket"`, `"region": "eu-west-2"`, `"public_url": "https://podcasts.example.com"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("expected %s in hosting.json:\n%s", want, data)
		}
	}

	out, code = run("init", "--feed-dir", feedRoot, "--bucket", "other-bucket")
	if code == 0 || !strings.Contains(out, "already exists") {
		t.Errorf("expected init to refuse to overwrite hosting.json, exit %d:\n%s", code, out)
	}
}

// --- import and update integration tests ---

func TestImportFromFile(t *testing.T) {
	feedRoot := t.TempDir()

	out, code := run("import", "testdata/feed.xml", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("import exited %d:\n%s", code, out)
	}

	// Named after the title, "Dudes Rock".
	dir := filepath.Join(feedRoot, "dudes-rock")
	data, err := os.ReadFile(filepath.Join(dir, "feed.csv"))
	if err != nil {
		t.Fatalf("expected feed.csv: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines[0] != "title\x1fdescription\x1fdate\x1furl\x1fguid" {
		t.Errorf("unexpected header %q", lines[0])
	}
	if len(lines) != 7 {
		t.Errorf("got %d lines; want header + 6 episodes:\n%s", len(lines), data)
	}
	// Every episode gets its own new UUID, not the source's GUID.
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\x1f")
		guid := fields[len(fields)-1]
		if !uuidPattern.MatchString(guid) || seen[guid] {
			t.Errorf("expected a new, distinct UUID, got %q in %q", guid, line)
		}
		seen[guid] = true
	}
	if !strings.Contains(string(data), "1: 2002-06-29\x1f") {
		t.Errorf("first episode missing from CSV:\n%s", data)
	}
	// Descriptions are stored escaped, as build writes them out verbatim.
	if !strings.Contains(string(data), "\x1f&lt;p&gt;You're the one that's sad and lonely!&lt;/p&gt;") {
		t.Errorf("expected an HTML-escaped description:\n%s", data)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, "feed.json"))
	if err != nil {
		t.Fatalf("expected feed.json: %v", err)
	}
	abs, _ := filepath.Abs("testdata/feed.xml")
	for _, want := range []string{`"title": "Dudes Rock"`, `"author": "Real-ass dudes"`, `"source": "` + abs + `"`} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("expected %s in feed.json:\n%s", want, cfg)
		}
	}

	// It won't overwrite the feed without --force.
	out, code = run("import", "testdata/feed.xml", "--feed-dir", feedRoot)
	if code == 0 || !strings.Contains(out, "already exists") {
		t.Errorf("expected a second import to refuse, exit %d:\n%s", code, out)
	}
	out, code = run("import", "testdata/feed.xml", "--slug", "other", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("import --slug exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(feedRoot, "other", "feed.csv")); err != nil {
		t.Errorf("import --slug didn't write other/feed.csv: %v", err)
	}
}

// TestImportFixture imports testdata/import/source.xml, a feed in the shape
// third-party hosts produce, and compares the feed directory it writes with
// testdata/import/want: feed.csv (UUIDs masked) and feed.json byte for byte,
// and image.jpg converted from art.png. {{base}} in the fixtures stands for
// the test server's URL.
func TestImportFixture(t *testing.T) {
	const fixtures = "testdata/import"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/source.xml":
			data, _ := os.ReadFile(filepath.Join(fixtures, "source.xml"))
			w.Write(bytes.ReplaceAll(data, []byte("{{base}}"), []byte(srv.URL)))
		case "/art.png":
			http.ServeFile(w, r, filepath.Join(fixtures, "art.png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	feedRoot := t.TempDir()
	out, code := run("import", srv.URL+"/source.xml", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("import exited %d:\n%s", code, out)
	}
	for _, title := range []string{"A note about our schedule", "Trailer"} {
		if !strings.Contains(out, fmt.Sprintf("Warning: skipping %q, which has no audio URL", title)) {
			t.Errorf("expected %q, which has no enclosure, to be reported as skipped:\n%s", title, out)
		}
	}

	// The slug comes from the title.
	dir := filepath.Join(feedRoot, "bill-ben-s-garden")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("import didn't create the feed directory: %v\n%s", err, out)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, " "); got != "feed.csv feed.json image.jpg" {
		t.Errorf("feed directory holds %q; want feed.csv, feed.json and image.jpg", got)
	}

	for _, name := range []string{"feed.csv", "feed.json"} {
		want, err := os.ReadFile(filepath.Join(fixtures, "want", name))
		if err != nil {
			t.Fatal(err)
		}
		want = bytes.ReplaceAll(want, []byte("{{base}}"), []byte(srv.URL))
		got, _ := os.ReadFile(filepath.Join(dir, name))
		got = uuidPattern.ReplaceAll(got, []byte("UUID"))
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from %s/want/%s.\ngot:\n%q\nwant:\n%q", name, fixtures, name, got, want)
		}
	}

	// Each episode has its own UUID, not the source's GUID.
	csv, _ := os.ReadFile(filepath.Join(dir, "feed.csv"))
	if ids := uuidPattern.FindAll(csv, -1); len(ids) != 3 || bytes.Equal(ids[0], ids[1]) || bytes.Equal(ids[1], ids[2]) {
		t.Errorf("expected 3 distinct UUIDs:\n%s", csv)
	}

	// image.jpg is itunes:image's PNG (not <image>'s small.jpg), converted to JPEG.
	art, err := os.Open(filepath.Join(dir, "image.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer art.Close()
	cfg, format, err := image.DecodeConfig(art)
	if err != nil || format != "jpeg" || cfg.Width != 30 || cfg.Height != 20 {
		t.Errorf("image.jpg: format %q, %dx%d, err %v; want a 30x20 JPEG", format, cfg.Width, cfg.Height, err)
	}
}

// sourceServer serves a source feed, rendered from the given items on every
// request, along with episode audio.
func sourceServer(t *testing.T, items *[]string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.rss":
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>Bill &amp; Ben</title>
    <link>https://example.com/bill-and-ben</link>
    <atom:link href="%s/feed.rss" rel="self" type="application/rss+xml"/>
    <description>Flowerpot men.</description>
    %s
  </channel>
</rss>`, srv.URL, strings.Join(*items, "\n"))
		default:
			http.ServeFile(w, r, "testdata/test.mp3")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sourceItem(srvURL, title, date, description string) string {
	return fmt.Sprintf(`<item><title>%s</title><pubDate>%s</pubDate><description><![CDATA[%s]]></description>`+
		`<enclosure url="%s/%s.mp3" length="1" type="audio/mpeg"/></item>`,
		title, date, description, srvURL, urlSafe(title))
}

func urlSafe(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}

// TestImportBuildUpdate imports a feed from a URL, builds it, and picks up a
// new episode with update. TestImportFixture checks what import writes.
func TestImportBuildUpdate(t *testing.T) {
	var items []string
	srv := sourceServer(t, &items)
	items = []string{
		sourceItem(srv.URL, "Episode Two", "Tue, 02 Jan 2024 09:00:00 GMT", "<p>Weeds\nand pots</p>"),
		sourceItem(srv.URL, "Episode One", "Mon, 01 Jan 2024 09:00:00 GMT", ""),
	}

	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)

	out, code := run("import", srv.URL+"/feed.rss", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("import exited %d:\n%s", code, out)
	}
	dir := filepath.Join(feedRoot, "bill-ben")

	out, code = run("build", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}
	assertWellFormed(t, filepath.Join(dir, "feed.xml"))

	out, code = run("update", "--feed-dir", feedRoot)
	if code != 0 || !strings.Contains(out, "no new episodes") {
		t.Fatalf("update with nothing new exited %d:\n%s", code, out)
	}

	items = append([]string{
		sourceItem(srv.URL, "Episode Three", "Wed, 03 Jan 2024 09:00:00 GMT", "New & improved"),
	}, items...)
	out, code = run("update", "bill-ben", "--feed-dir", feedRoot)
	if code != 0 || !strings.Contains(out, "1 new episode(s)") {
		t.Fatalf("update exited %d:\n%s", code, out)
	}
	csv, _ := os.ReadFile(filepath.Join(dir, "feed.csv"))
	if got := uuidPattern.ReplaceAllString(string(csv), "UUID"); !strings.HasSuffix(got,
		"Episode Three\x1fNew &amp; improved\x1fWed, 03 Jan 2024 09:00:00 GMT\x1f"+srv.URL+"/episode-three.mp3\x1fUUID\n") {
		t.Errorf("expected Episode Three appended:\n%s", csv)
	}

	out, _ = run("status", "--feed-dir", feedRoot)
	if !regexp.MustCompile(`bill-ben\s+3\s+2024-01-03T09:00:00Z\s+stale\s+0/3 hosted\s+yes`).MatchString(out) {
		t.Errorf("expected status to show the feed as stale after update:\n%s", out)
	}
	if out, code := run("build", "--feed-dir", feedRoot); code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}
	assertWellFormed(t, filepath.Join(dir, "feed.xml"))
	out, _ = run("status", "--feed-dir", feedRoot)
	if !regexp.MustCompile(`bill-ben\s+3\s+\S+\s+ok\s`).MatchString(out) {
		t.Errorf("expected status ok after build:\n%s", out)
	}
}

func TestUpdateRequiresSource(t *testing.T) {
	feedRoot := t.TempDir()
	writeFeed(t, feedRoot, "handmade", `{"title":"Handmade"}`)

	out, code := run("update", "--feed-dir", feedRoot)
	if code != 0 {
		t.Errorf("update of every feed should skip feeds without a source, exit %d:\n%s", code, out)
	}
	out, code = run("update", "handmade", "--feed-dir", feedRoot)
	if code == 0 || !strings.Contains(out, `no "source"`) {
		t.Errorf("expected update of a named feed without a source to fail, exit %d:\n%s", code, out)
	}
}

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

// guids returns the <guid> elements in a feed, as written.
func guids(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return regexp.MustCompile(`<guid[^>]*>[^<]*</guid>`).FindAllString(string(data), -1)
}

// TestBuildKeepsPublishedGUIDs checks a feed from before the guid column keeps
// the GUIDs it was published with (its episode URLs), written exactly as
// before, while new episodes get UUIDs.
func TestBuildKeepsPublishedGUIDs(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	dir := writeFeed(t, feedRoot, "old-show", `{"title":"Old Show"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep1.mp3"),
		episode("Ep2", "Sat, 06 Jul 2002 03:00:00 GMT", audio+"/ep2.mp3?a=1&b=2"),
		episode("Ep3", "Sat, 13 Jul 2002 03:00:00 GMT", audio+"/ep3.mp3"))

	// As 0.3.0 published Ep1 and Ep2, with their URLs as GUIDs (and & escaped twice).
	published := []string{
		`<guid>` + audio + `/ep1.mp3</guid>`,
		`<guid>` + audio + `/ep2.mp3?a=1&amp;amp;b=2</guid>`,
	}
	os.WriteFile(filepath.Join(dir, "feed.xml"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Old Show</title>
<item><title>1: Ep1</title><pubDate>Sat, 29 Jun 2002 03:00:00 GMT</pubDate>`+published[0]+
		`<enclosure url="`+audio+`/ep1.mp3" length="1" type="audio/mpeg"/></item>
<item><title>2: Ep2</title><pubDate>Sat, 06 Jul 2002 03:00:00 GMT</pubDate>`+published[1]+
		`<enclosure url="`+audio+`/ep2.mp3?a=1&amp;amp;b=2" length="1" type="audio/mpeg"/></item>
</channel></rss>
`), 0644)

	out, code := run("build", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Gave 3 episode(s) a GUID") {
		t.Errorf("expected build to report the GUIDs it assigned:\n%s", out)
	}

	got := guids(t, filepath.Join(dir, "feed.xml"))
	if len(got) != 3 || got[0] != published[0] || got[1] != published[1] {
		t.Fatalf("published GUIDs changed:\n%s\nwant to start:\n%s", strings.Join(got, "\n"), strings.Join(published, "\n"))
	}
	if !regexp.MustCompile(`^<guid isPermaLink="false">` + uuidPattern.String() + `</guid>$`).MatchString(got[2]) {
		t.Errorf("new episode's GUID = %s; want a UUID that isn't a permalink", got[2])
	}

	// The GUIDs are now in the CSV, so moving an episode doesn't change its GUID.
	csvPath := filepath.Join(dir, "feed.csv")
	csv, _ := os.ReadFile(csvPath)
	os.WriteFile(csvPath, []byte(strings.ReplaceAll(string(csv), "/ep1.mp3\x1f", "/moved/ep1.mp3\x1f")), 0644)
	if out, code := run("build", "--feed-dir", feedRoot); code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}
	moved := guids(t, filepath.Join(dir, "feed.xml"))
	if strings.Join(moved, "\n") != strings.Join(got, "\n") {
		t.Errorf("GUIDs changed when an episode moved:\n%s\nwant:\n%s", strings.Join(moved, "\n"), strings.Join(got, "\n"))
	}
	xml, _ := os.ReadFile(filepath.Join(dir, "feed.xml"))
	if !strings.Contains(string(xml), `<enclosure url="`+audio+`/moved/ep1.mp3"`) {
		t.Errorf("expected the moved episode's new URL in its enclosure:\n%s", xml)
	}
}

// assertWellFormed fails unless path parses as XML.
func assertWellFormed(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		if _, err := dec.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("%s isn't well-formed XML: %v", path, err)
		}
	}
}

// --- dates integration tests ---

func TestDates(t *testing.T) {
	feedRoot := t.TempDir()
	dir := filepath.Join(feedRoot, "show")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "feed.json"), []byte(`{"title":"Show","delimiter":";"}`), 0644)
	csvPath := filepath.Join(dir, "feed.csv")
	os.WriteFile(csvPath, []byte(
		"title;description;date;url\n"+
			"Episode 1;Desc 1;Jun 1, 2023;http://example.com/1\n"+
			"Episode 2;Desc 2;Jul 2, 2023;http://example.com/2\n"+
			"Episode 3;Desc 3;Jul. 3, 2023;http://example.com/3\n",
	), 0644)

	out, code := run("dates", "show", "--from", "%b %d, %Y", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("dates exited %d:\n%s", code, out)
	}

	data, _ := os.ReadFile(csvPath)
	content := string(data)
	for _, want := range []string{"Thu, 01 Jun 2023 03:00:00 GMT", "Sun, 02 Jul 2023 03:00:00 GMT", "Mon, 03 Jul 2023 03:00:00 GMT"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %s in:\n%s", want, content)
		}
	}
	if _, err := os.Stat(csvPath + ".old"); err != nil {
		t.Error("expected .old backup file")
	}

	if _, code := run("dates", "show", "--feed-dir", feedRoot); code == 0 {
		t.Error("expected dates without --from to fail")
	}
}

// --- audio integration tests ---

func TestAudioDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		switch r.URL.Path {
		case "/Free_Test_Data_100KB_MP3.mp3", "/Free_Test_Data_100KB_OGG.ogg":
			w.Write(make([]byte, 1024))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	feedRoot := t.TempDir()
	outputDir := t.TempDir()
	dir := filepath.Join(feedRoot, "show")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "feed.json"), []byte(`{"title":"Show","delimiter":";"}`), 0644)
	os.WriteFile(filepath.Join(dir, "feed.csv"), []byte(fmt.Sprintf(
		"title;description;date;url\n"+
			"Episode 1;Desc 1;Jun 1, 2023;%s/Free_Test_Data_100KB_MP3.mp3\n"+
			"Episode 2;Desc 2;Jul 2, 2023;%s/Free_Test_Data_100KB_OGG.ogg\n",
		srv.URL, srv.URL,
	)), 0644)
	os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("foo"), 0644)

	out, code := run("audio", "download", "show", outputDir, "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("audio download exited %d:\n%s", code, out)
	}

	archived := filepath.Join(outputDir, "show")
	for _, name := range []string{"Free_Test_Data_100KB_MP3.mp3", "Free_Test_Data_100KB_OGG.ogg"} {
		if _, err := os.Stat(filepath.Join(archived, "items", name)); err != nil {
			t.Errorf("%s not downloaded", name)
		}
	}
	if data, err := os.ReadFile(filepath.Join(archived, "foo.txt")); err != nil || string(data) != "foo" {
		t.Error("foo.txt not copied correctly")
	}
	localCSV, _ := os.ReadFile(filepath.Join(archived, "local.csv"))
	for _, want := range []string{"file://items/Free_Test_Data_100KB_MP3.mp3", "file://items/Free_Test_Data_100KB_OGG.ogg"} {
		if !strings.Contains(string(localCSV), want) {
			t.Errorf("local.csv missing %s:\n%s", want, localCSV)
		}
	}
}

func TestAudioUploadTestResourceExists(t *testing.T) {
	info, err := os.Stat("testdata/test.mp3")
	if err != nil {
		t.Fatalf("testdata/test.mp3 not found: %v", err)
	}
	const want = 103016
	if info.Size() != want {
		t.Errorf("test.mp3 size = %d; want %d", info.Size(), want)
	}
}

// TestAudioUploadDstURLHasSlashBetweenBucketAndKey is a regression test for the bug
// where the destination HTTPS URL was constructed as:
//
//	https://s3.REGION.amazonaws.com/BUCKETrepo-dir/file.mp3   ← missing /
//
// instead of:
//
//	https://s3.REGION.amazonaws.com/BUCKET/repo-dir/file.mp3  ← correct
//
// This caused S3 moves to 404 immediately because the URL pointed at a
// non-existent host rather than the correct bucket path.
func TestAudioUploadDstURLHasSlashBetweenBucketAndKey(t *testing.T) {
	feedRoot := t.TempDir()
	bucket := "my-bucket"
	region := "eu-west-2"
	os.WriteFile(filepath.Join(feedRoot, "hosting.json"), []byte(fmt.Sprintf(
		`{"bucket":%q,"region":%q}`, bucket, region)), 0644)

	// The URL is already at the correct destination, so upload should detect
	// "already at destination, skipping" without any S3 calls.
	writeFeed(t, feedRoot, "my-show", `{"title":"My Show"}`,
		episode("Episode 1", "Sat, 29 Jun 2002 03:00:00 GMT",
			fmt.Sprintf("https://s3.%s.amazonaws.com/%s/my-show/01-episode-1.mp3", region, bucket)))

	out, _ := run("audio", "upload", "my-show", "--feed-dir", feedRoot)

	wantURLFragment := fmt.Sprintf("amazonaws.com/%s/my-show/", bucket)
	if !strings.Contains(out, wantURLFragment) {
		t.Errorf("Dst URL missing slash between bucket and key.\nWant fragment: %q\nGot output:\n%s",
			wantURLFragment, out)
	}
	badURLFragment := fmt.Sprintf("amazonaws.com/%smy-show/", bucket)
	if strings.Contains(out, badURLFragment) {
		t.Errorf("Dst URL has missing slash (regression): %q found in output", badURLFragment)
	}
}

func TestAudioUploadWithoutHostingFails(t *testing.T) {
	feedRoot := t.TempDir()
	writeFeed(t, feedRoot, "my-show", `{"title":"My Show"}`)
	out, code := run("audio", "upload", "my-show", "--feed-dir", feedRoot)
	if code == 0 || !strings.Contains(out, "no bucket configured") {
		t.Errorf("expected missing bucket error, exit %d:\n%s", code, out)
	}
}

// --- XML parsing helpers ---

type parsedFeed struct {
	title         string
	lastBuildDate string
	items         []parsedItem
}

type parsedItem struct {
	title        string
	pubDate      string
	enclosureURL string
	description  string
}

// parseXMLItems extracts channel title, lastBuildDate, and per-item fields from
// raw RSS XML bytes using simple regex extraction. This is intentionally
// format-agnostic so it works equally well on xmllint-formatted output and
// our Go template output.
func parseXMLItems(t *testing.T, data []byte) parsedFeed {
	t.Helper()
	src := string(data)

	extract := func(tag, content string) string {
		re := regexp.MustCompile(`(?s)<` + tag + `[^>]*>([^<]*)<\/` + tag + `>`)
		m := re.FindStringSubmatch(content)
		if m != nil {
			return strings.TrimSpace(m[1])
		}
		return ""
	}
	extractAttr := func(attr, content string) string {
		re := regexp.MustCompile(attr + `="([^"]*)"`)
		m := re.FindStringSubmatch(content)
		if m != nil {
			return m[1]
		}
		return ""
	}
	extractFull := func(tag, content string) string {
		re := regexp.MustCompile(`(?s)<` + tag + `[^>]*>(.*?)<\/` + tag + `>`)
		m := re.FindStringSubmatch(content)
		if m != nil {
			return strings.TrimSpace(m[1])
		}
		return ""
	}

	feed := parsedFeed{}

	// Channel title: first <title> before any <item>
	firstItem := strings.Index(src, "<item>")
	channelSection := src
	if firstItem > 0 {
		channelSection = src[:firstItem]
	}
	feed.title = extract("title", channelSection)
	feed.lastBuildDate = extract("lastBuildDate", channelSection)

	// Items
	itemRe := regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	for _, m := range itemRe.FindAllStringSubmatch(src, -1) {
		body := m[1]
		feed.items = append(feed.items, parsedItem{
			title:        extract("title", body),
			pubDate:      extract("pubDate", body),
			enclosureURL: extractAttr("url", body),
			description:  extractFull("description", body),
		})
	}
	return feed
}

// --- publish integration tests ---

// s3TestBucket returns the bucket to publish to, skipping unless a local S3
// (SeaweedFS in CI) is configured via AWS_ENDPOINT_URL.
func s3TestBucket(t *testing.T) string {
	t.Helper()
	if os.Getenv("AWS_ENDPOINT_URL") == "" {
		t.Skip("AWS_ENDPOINT_URL not set; skipping S3 integration test")
	}
	if b := os.Getenv("TEST_BUCKET"); b != "" {
		return b
	}
	return "test-bucket"
}

// writeHostedFeed writes a built feed (feed.json, feed.csv, feed.xml,
// image.jpg) under feedRoot/slug and a hosting.json pointing at bucket/prefix,
// served from publicURL.
func writeHostedFeed(t *testing.T, feedRoot, slug, bucket, prefix, publicURL string) {
	t.Helper()
	audio := audioServer(t)
	os.WriteFile(filepath.Join(feedRoot, "hosting.json"), []byte(fmt.Sprintf(
		`{"bucket":%q,"region":"eu-west-2","prefix":%q,"public_url":%q}`,
		bucket, prefix, publicURL)), 0644)
	dir := writeFeed(t, feedRoot, slug, `{"title":"Hosted Show","author":"Someone & Co"}`,
		episode("Ep1", "Sat, 29 Jun 2002 03:00:00 GMT", audio+"/ep1.mp3"),
		episode("Ep2", "Sat, 06 Jul 2002 03:00:00 GMT", audio+"/ep2.mp3"))
	os.WriteFile(filepath.Join(dir, "image.jpg"), []byte("not really a jpeg"), 0644)
	if out, code := run("build", slug, "--feed-dir", feedRoot); code != 0 {
		t.Fatalf("build exited %d:\n%s", code, out)
	}
}

func TestPublishUploadsFeedsAndSkipsUnchanged(t *testing.T) {
	bucket := s3TestBucket(t)
	feedRoot := t.TempDir()
	prefix := fmt.Sprintf("it-%d", os.Getpid())
	cdn := fakeCDN(t, bucket)
	writeHostedFeed(t, feedRoot, "hosted-show", bucket, prefix, cdn.URL)

	out, code := run("publish", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("publish exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Published.") {
		t.Errorf("expected publish to confirm the public URL serves the index:\n%s", out)
	}
	for _, key := range []string{"hosted-show/feed.xml", "hosted-show/image.jpg", "index.json", "podcasts.opml"} {
		want := fmt.Sprintf("uploaded → s3://%s/%s/%s", bucket, prefix, key)
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}

	out, code = run("publish", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("second publish exited %d:\n%s", code, out)
	}
	if strings.Contains(out, "uploaded") || strings.Count(out, "unchanged") != 4 {
		t.Errorf("second publish should skip all 4 unchanged files:\n%s", out)
	}

	// The published feed files must not count as orphaned episodes.
	out, code = run("audio", "prune", "hosted-show", "--feed-dir", feedRoot, "--fail-on-orphans")
	if code != 0 {
		t.Fatalf("audio prune exited %d (feed files reported as orphans?):\n%s", code, out)
	}
	if !strings.Contains(out, "No orphaned self-hosted files found.") {
		t.Errorf("expected no orphans:\n%s", out)
	}
}

// fakeCDN serves the bucket root over plain HTTP, as CloudFront does for the
// real bucket. (The local S3 doesn't serve anonymous reads.)
func fakeCDN(t *testing.T, bucket string) *httptest.Server {
	t.Helper()
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("eu-west-2"))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := client.GetObject(r.Context(), &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(strings.TrimPrefix(r.URL.Path, "/")),
		})
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer out.Body.Close()
		w.Header().Set("Content-Type", aws.ToString(out.ContentType))
		if r.Method != http.MethodHead {
			io.Copy(w, out.Body) //nolint:errcheck
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPublishFailsWhenPublicURLUnreachable(t *testing.T) {
	bucket := s3TestBucket(t)
	feedRoot := t.TempDir()
	writeHostedFeed(t, feedRoot, "hosted-show", bucket, fmt.Sprintf("it-dead-%d", os.Getpid()), "http://127.0.0.1:1")

	out, code := run("publish", "--feed-dir", feedRoot)
	if code == 0 {
		t.Fatalf("expected publish to fail when the public URL is unreachable:\n%s", out)
	}
	if !strings.Contains(out, "isn't reachable") {
		t.Errorf("expected unreachable public URL error:\n%s", out)
	}
}

// TestPublishRefusesStaleFeed checks publish refuses, before touching the
// bucket, a feed whose inputs changed since it was built.
func TestPublishRefusesStaleFeed(t *testing.T) {
	feedRoot := t.TempDir()
	writeHostedFeed(t, feedRoot, "hosted-show", "my-bucket", "", "http://127.0.0.1:1")
	writeFeed(t, feedRoot, "other-show", `{"title":"Other Show"}`)

	csvPath := filepath.Join(feedRoot, "hosted-show", "feed.csv")
	csv, _ := os.ReadFile(csvPath)
	os.WriteFile(csvPath, append(csv, []byte(episode("Ep3", "Sat, 13 Jul 2002 03:00:00 GMT", "https://example.com/ep3.mp3")+"\n")...), 0644)

	for _, args := range [][]string{{"publish"}, {"publish", "hosted-show"}, {"publish", "--dry-run"}} {
		out, code := run(append(args, "--feed-dir", feedRoot)...)
		if code == 0 {
			t.Fatalf("expected %v to refuse a stale feed:\n%s", args, out)
		}
		if !strings.Contains(out, "feed.xml is out of date for hosted-show; run podcastinator build hosted-show first") {
			t.Errorf("expected %v to name the stale feed:\n%s", args, out)
		}
	}

	// Changing hosting.json makes the feed stale too, as its URLs come from it.
	if out, code := run("build", "--feed-dir", feedRoot); code == 0 {
		t.Fatalf("build should fail on Ep3's unreachable URL:\n%s", out)
	}
	os.WriteFile(csvPath, csv, 0644)
	os.WriteFile(filepath.Join(feedRoot, "hosting.json"), []byte(`{"bucket":"my-bucket","public_url":"https://elsewhere.example.com"}`), 0644)
	out, code := run("publish", "--feed-dir", feedRoot)
	if code == 0 || !strings.Contains(out, "out of date for hosted-show") {
		t.Errorf("expected a hosting.json change to make the feed stale, exit %d:\n%s", code, out)
	}
}

func TestPublishWithoutBucketFails(t *testing.T) {
	feedRoot := t.TempDir()
	out, code := run("publish", "--feed-dir", feedRoot)
	if code == 0 {
		t.Fatalf("expected publish to fail without a bucket:\n%s", out)
	}
	if !strings.Contains(out, "no bucket configured") {
		t.Errorf("expected missing bucket error:\n%s", out)
	}
}

func TestVersion(t *testing.T) {
	out, code := run("--version")
	if code != 0 {
		t.Fatalf("--version exited %d: %s", code, out)
	}
	if !strings.HasPrefix(out, "podcastinator version ") {
		t.Errorf("--version printed %q", out)
	}
}
