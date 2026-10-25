// Package integration contains black-box integration tests for the podcastinator CLI.
// Tests build the binary once in TestMain and then invoke it as a subprocess,
// mirroring what the bash test suite does.
package integration

import (
	"context"
	"fmt"
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

// --- csv2rss integration tests ---

// TestCSV2RSSGoldenSnapshot build RSS from the test CSV and compare its parsed content
// against test/testdata/feed.xml.
// Comparison is semantic (title, date, URL per item) so XML formatting differences
// between the Go template and the legacy xmllint output do not cause false failures.
func TestCSV2RSSGoldenSnapshot(t *testing.T) {
	audio := audioServer(t)
	dir := t.TempDir()
	feedDir := filepath.Join(dir, "feed", "test")
	os.MkdirAll(feedDir, 0755)
	writeHosting(t, filepath.Dir(feedDir))
	csvPath := filepath.Join(feedDir, "test.csv")

	// Write the exact input CSV from the bash test (unit separator delimiter).
	delim := "\x1f"
	lines := []string{
		"title" + delim + "description" + delim + "date" + delim + "url",
		"2002-06-29" + delim + "" + delim + "Sat, 29 Jun 2002 03:00:00 GMT" + delim + audio + "/ep.mp3?id=1",
		"S01E01" + delim + "&lt;p&gt;You're the one that's sad and lonely!&lt;/p&gt;" + delim + "Sun, 30 June 2002 19:07:00 +0100" + delim + audio + "/ep.mp3?id=2",
		"Ep. 1 - The Pilot" + delim + `&lt;a href="https://example.com/shows/ep-1-the-pilot" rel="nofollow noopener" target="_blank"&gt;May 11, 2016&lt;/a&gt;&lt;p&gt;Two friends sit down and start a podcast. This one is different from other podcasts where two people talk about people you don't know. We get real. We take this seriously.&lt;/p&gt;&lt;p&gt;&lt;strong&gt;Tags&lt;/strong&gt;&lt;/p&gt;&lt;ul&gt; &lt;li&gt;&lt;a href="https://example.com/tags/cooking/" rel="nofollow noopener" target="_blank"&gt;cooking&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/gardening/" rel="nofollow noopener" target="_blank"&gt;gardening&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/storytelling/" rel="nofollow noopener" target="_blank"&gt;storytelling&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/comedy/" rel="nofollow noopener" target="_blank"&gt;comedy&lt;/a&gt;&lt;/li&gt; &lt;li&gt;&lt;a href="https://example.com/tags/politics/" rel="nofollow noopener" target="_blank"&gt;politics&lt;/a&gt;&lt;/li&gt;&lt;/ul&gt;` + delim + "Mon, 01 July 2002 20:33:05 GMT" + delim + audio + "/ep.mp3?id=3",
		"Inaugral Business" + delim + "" + delim + "Tue, 02 July 2002 03:00:00 GMT" + delim + audio + "/ep.mp3?id=4",
		"Part 1: The Power of Gentleness" + delim + "&lt;p&gt;Gentle managers, unofficial tractors, cheap lobster and black dogs are covered in the first episode of this football podcast with Bob Mortimer and Andy Dawson.&lt;/p&gt;" + delim + "Wed, 03 July 2002 18:50:00 GMT" + delim + audio + "/ep.mp3?id=5",
		"2006 01 03 - Tuesday" + delim + "" + delim + "Thu, 04 July 2002 03:00:00 GMT" + delim + audio + "/ep.mp3?id=6",
	}
	os.WriteFile(csvPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	out, code := run("csv2rss", csvPath,
		"--title", "Dudes Rock",
		"--description", "Hell yeah dude!",
		"--author", "Real-ass dudes",
		"--image-url", "https://link.com/image.jpg",
	)
	if code != 0 {
		t.Fatalf("csv2rss exited %d:\n%s", code, out)
	}

	actualXML, err := os.ReadFile(filepath.Join(feedDir, "test.xml"))
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

func TestCSV2RSSLastBuildDateFormat(t *testing.T) {
	audio := audioServer(t)
	dir := t.TempDir()
	feedDir := filepath.Join(dir, "feed", "myshow")
	os.MkdirAll(feedDir, 0755)
	writeHosting(t, filepath.Dir(feedDir))
	csvPath := filepath.Join(feedDir, "feed.csv")
	os.WriteFile(csvPath, []byte("title\x1fdescription\x1fdate\x1furl\n"+
		"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep.mp3?id=1\n"),
		0644)

	out, code := run("csv2rss", csvPath, "--title", "My Show")
	if code != 0 {
		t.Fatalf("csv2rss exited %d:\n%s", code, out)
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

func TestCSV2RSSMissingTitleError(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "feed.csv")
	os.WriteFile(csvPath, []byte("title\x1fdescription\x1fdate\x1furl\n"), 0644)

	_, code := run("csv2rss", csvPath)
	if code == 0 {
		t.Error("expected non-zero exit when --title is missing and no feed.json")
	}
}

func TestCSV2RSSLoadsFeedJSON(t *testing.T) {
	audio := audioServer(t)
	dir := t.TempDir()
	feedDir := filepath.Join(dir, "feed", "myshow")
	os.MkdirAll(feedDir, 0755)
	writeHosting(t, filepath.Dir(feedDir))

	os.WriteFile(filepath.Join(feedDir, "feed.json"), []byte(`{
		"title":"JSON Show","description":"From JSON","author":"JSON Author"
	}`), 0644)
	os.WriteFile(filepath.Join(feedDir, "feed.csv"), []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep.mp3?id=1\n"),
		0644)

	out, code := run("csv2rss", filepath.Join(feedDir, "feed.csv"))
	if code != 0 {
		t.Fatalf("csv2rss exited %d:\n%s", code, out)
	}

	xml, _ := os.ReadFile(filepath.Join(feedDir, "feed.xml"))
	src := string(xml)
	if !strings.Contains(src, "<title>JSON Show</title>") {
		t.Errorf("title from feed.json not in output")
	}
	if !strings.Contains(src, "<itunes:author>JSON Author</itunes:author>") {
		t.Errorf("author from feed.json not in output")
	}
}

func TestCSV2RSSRequiresFeedURL(t *testing.T) {
	audio := audioServer(t)
	dir := t.TempDir()
	feedDir := filepath.Join(dir, "feed", "unhosted")
	os.MkdirAll(feedDir, 0755)
	csvPath := filepath.Join(feedDir, "feed.csv")
	os.WriteFile(csvPath, []byte("title\x1fdescription\x1fdate\x1furl\n"+
		"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep.mp3\n"), 0644)

	out, code := run("csv2rss", csvPath, "--title", "Unhosted")
	if code == 0 {
		t.Fatalf("expected csv2rss to fail without hosting.json or feed URLs:\n%s", out)
	}
	if !strings.Contains(out, "no feed URL or image URL") {
		t.Errorf("expected missing feed URL error:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(feedDir, "feed.xml")); err == nil {
		t.Error("feed.xml should not have been written")
	}

	// Flags are enough on their own.
	out, code = run("csv2rss", csvPath, "--title", "Unhosted",
		"--feed-url", "https://podcasts.example.com/unhosted/feed.xml",
		"--image-url", "https://podcasts.example.com/unhosted/image.jpg")
	if code != 0 {
		t.Fatalf("csv2rss with --feed-url and --image-url exited %d:\n%s", code, out)
	}
}

func TestCSV2RSSFailsWhenURLCannotBeResolved(t *testing.T) {
	dir := t.TempDir()
	feedDir := filepath.Join(dir, "feed", "broken")
	os.MkdirAll(feedDir, 0755)
	writeHosting(t, filepath.Dir(feedDir))
	csvPath := filepath.Join(feedDir, "feed.csv")
	os.WriteFile(csvPath, []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1fhttp://127.0.0.1:1/ep1.mp3\n"),
		0644)

	out, code := run("csv2rss", csvPath, "--title", "Broken Show")
	if code == 0 {
		t.Fatalf("expected csv2rss to fail when URL cannot be resolved")
	}
	if !strings.Contains(out, "resolve URL") {
		t.Fatalf("expected resolve URL error in output, got:\n%s", out)
	}
}

// --- rss2csv integration tests ---

// copyTestFeed copies testdata/feed.xml to dst, creating its directory.
func copyTestFeed(t *testing.T, dst string) {
	t.Helper()
	data, err := os.ReadFile("testdata/feed.xml")
	if err != nil {
		t.Fatalf("read testdata/feed.xml: %v", err)
	}
	os.MkdirAll(filepath.Dir(dst), 0755)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// assertFeedCSV checks that path is a CSV converted from testdata/feed.xml.
func assertFeedCSV(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected CSV at %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines[0] != "title\x1fdescription\x1fdate\x1furl" {
		t.Errorf("unexpected header %q", lines[0])
	}
	if len(lines) != 7 {
		t.Errorf("got %d lines; want header + 6 episodes:\n%s", len(lines), data)
	}
	if !strings.Contains(string(data), "1: 2002-06-29\x1f") {
		t.Errorf("first episode missing from CSV:\n%s", data)
	}
}

// TestRSS2CSVInputInFeedDir is a regression test for rss2csv writing to
// feed/feed/<slug>/ when the input was already at feed/<slug>/feed.xml.
func TestRSS2CSVInputInFeedDir(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "feed", "my-show", "feed.xml")
	copyTestFeed(t, input)

	// No --repo-dir: it defaults to the input's directory name.
	out, code := runInDir(root, "rss2csv", "feed/my-show/feed.xml")
	if code != 0 {
		t.Fatalf("rss2csv exited %d:\n%s", code, out)
	}

	assertFeedCSV(t, filepath.Join(root, "feed", "my-show", "feed.csv"))
	if _, err := os.Stat(filepath.Join(root, "feed", "feed")); err == nil {
		t.Error("feed/feed/ should not have been created")
	}
}

// TestRSS2CSVInputOutsideFeedDir is a regression test for rss2csv writing to
// <parent of input dir>/feed/<slug>/<input name>.csv instead of feed/<slug>/feed.csv.
func TestRSS2CSVInputOutsideFeedDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "feed"), 0755)
	copyTestFeed(t, filepath.Join(root, "downloads", "their.xml"))

	out, code := runInDir(root, "rss2csv", "downloads/their.xml", "--repo-dir", "other")
	if code != 0 {
		t.Fatalf("rss2csv exited %d:\n%s", code, out)
	}

	feedDir := filepath.Join(root, "feed", "other")
	assertFeedCSV(t, filepath.Join(feedDir, "feed.csv"))
	for _, name := range []string{"their.csv", "their.xml", "feed.xml"} {
		if _, err := os.Stat(filepath.Join(feedDir, name)); err == nil {
			t.Errorf("feed/other/%s should not have been created", name)
		}
	}
}

func TestRSS2CSVFeedDirFlag(t *testing.T) {
	feedRoot := t.TempDir()
	input := filepath.Join(t.TempDir(), "their.xml")
	copyTestFeed(t, input)

	out, code := run("rss2csv", input, "--repo-dir", "other", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("rss2csv exited %d:\n%s", code, out)
	}
	assertFeedCSV(t, filepath.Join(feedRoot, "other", "feed.csv"))
}

func TestRSS2CSVRequiresRepoDirOutsideFeedDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "feed"), 0755)
	copyTestFeed(t, filepath.Join(root, "downloads", "their.xml"))

	out, code := runInDir(root, "rss2csv", "downloads/their.xml")
	if code == 0 {
		t.Fatalf("expected rss2csv to fail without --repo-dir:\n%s", out)
	}
	if !strings.Contains(out, "--repo-dir is required") {
		t.Errorf("expected missing --repo-dir error:\n%s", out)
	}
}

// --- pubdate integration tests ---

func TestPubdateMirroringBashTest(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "test.csv")

	os.WriteFile(csvPath, []byte(
		"title;description;date;url\n"+
			"Episode 1;Desc 1;Jun 1, 2023;http://example.com/1\n"+
			"Episode 2;Desc 2;Jul 2, 2023;http://example.com/2\n"+
			"Episode 3;Desc 3;Jul. 3, 2023;http://example.com/3\n",
	), 0644)

	out, code := run("pubdate", csvPath, "--input-format", "%b %d, %Y", "--delimiter", ";")
	if code != 0 {
		t.Fatalf("pubdate exited %d:\n%s", code, out)
	}

	data, _ := os.ReadFile(csvPath)
	content := string(data)
	if !strings.Contains(content, "Thu, 01 Jun 2023 03:00:00 GMT") {
		t.Errorf("missing Thu, 01 Jun 2023 in:\n%s", content)
	}
	if !strings.Contains(content, "Sun, 02 Jul 2023 03:00:00 GMT") {
		t.Errorf("missing Sun, 02 Jul 2023 in:\n%s", content)
	}
	if !strings.Contains(content, "Mon, 03 Jul 2023 03:00:00 GMT") {
		t.Errorf("missing Mon, 03 Jul 2023 in:\n%s", content)
	}

	// .old backup must exist
	if _, err := os.Stat(csvPath + ".old"); err != nil {
		t.Error("expected .old backup file")
	}
}

// --- archive integration tests ---

func TestArchiveMirroringBashTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		switch r.URL.Path {
		case "/Free_Test_Data_100KB_MP3.mp3":
			w.Write(make([]byte, 1024))
		case "/Free_Test_Data_100KB_OGG.ogg":
			w.Write(make([]byte, 1024))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	inputDir := t.TempDir()
	outputDir := t.TempDir()

	os.WriteFile(filepath.Join(inputDir, "feed.csv"), []byte(fmt.Sprintf(
		"title;description;date;url\n"+
			"Episode 1;Desc 1;Jun 1, 2023;%s/Free_Test_Data_100KB_MP3.mp3\n"+
			"Episode 2;Desc 2;Jul 2, 2023;%s/Free_Test_Data_100KB_OGG.ogg\n",
		srv.URL, srv.URL,
	)), 0644)
	os.WriteFile(filepath.Join(inputDir, "foo.txt"), []byte("foo"), 0644)

	out, code := run("archive", inputDir, outputDir, "--delimiter", ";")
	if code != 0 {
		t.Fatalf("archive exited %d:\n%s", code, out)
	}

	feedName := filepath.Base(inputDir)
	feedDir := filepath.Join(outputDir, feedName)

	// Check MP3 downloaded
	if _, err := os.Stat(filepath.Join(feedDir, "items", "Free_Test_Data_100KB_MP3.mp3")); err != nil {
		t.Error("MP3 not downloaded")
	}
	// Check OGG downloaded
	if _, err := os.Stat(filepath.Join(feedDir, "items", "Free_Test_Data_100KB_OGG.ogg")); err != nil {
		t.Error("OGG not downloaded")
	}
	// Check foo.txt copied
	if data, err := os.ReadFile(filepath.Join(feedDir, "foo.txt")); err != nil || string(data) != "foo" {
		t.Error("foo.txt not copied correctly")
	}
	// Check local.csv
	localCSV, _ := os.ReadFile(filepath.Join(feedDir, "local.csv"))
	if !strings.Contains(string(localCSV), "file://items/Free_Test_Data_100KB_MP3.mp3") {
		t.Errorf("local.csv missing mp3 URL:\n%s", string(localCSV))
	}
	if !strings.Contains(string(localCSV), "file://items/Free_Test_Data_100KB_OGG.ogg") {
		t.Errorf("local.csv missing ogg URL:\n%s", string(localCSV))
	}
}

// --- regenerate integration tests ---

// TestRegenerateNoDelimiterInFeedJSON is a regression test for the bug where
// regenerate passed rune(0) as the delimiter to csv2rss, causing "invalid field
// or comment delimiter" when feed.json has no "delimiter" field (the common case).
func TestRegenerateNoDelimiterInFeedJSON(t *testing.T) {
	audio := audioServer(t)
	// Build a minimal feed directory with feed.json (no delimiter) and feed.csv
	// using the default unit-separator delimiter.
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)
	feedDir := filepath.Join(feedRoot, "my-show")
	os.MkdirAll(feedDir, 0755)

	os.WriteFile(filepath.Join(feedDir, "feed.json"), []byte(`{
		"title": "My Show",
		"description": "A test show",
		"author": "Test Author"
	}`), 0644)

	// CSV with unit-separator delimiter (default) and a single episode.
	os.WriteFile(filepath.Join(feedDir, "feed.csv"), []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Episode 1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep.mp3?id=1\n",
	), 0644)

	out, code := run("regenerate", "--feed-dir", feedRoot, "--feed", "my-show")
	if code != 0 {
		t.Fatalf("regenerate exited %d:\n%s", code, out)
	}

	// feed.xml must have been created
	if _, err := os.Stat(filepath.Join(feedDir, "feed.xml")); err != nil {
		t.Errorf("feed.xml not created: %v", err)
	}
}

// TestRegenerateAllFeeds verifies that regenerate (no --feed flag) processes
// every feed directory that has both feed.json and feed.csv, and skips those
// that are missing either.
func TestRegenerateAllFeeds(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)

	// Feed with both files — should be regenerated.
	goodDir := filepath.Join(feedRoot, "good-show")
	os.MkdirAll(goodDir, 0755)
	os.WriteFile(filepath.Join(goodDir, "feed.json"), []byte(`{"title":"Good Show"}`), 0644)
	os.WriteFile(filepath.Join(goodDir, "feed.csv"), []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep.mp3?id=1\n",
	), 0644)

	// Feed with no feed.json — should be skipped silently.
	noJSONDir := filepath.Join(feedRoot, "no-json")
	os.MkdirAll(noJSONDir, 0755)
	os.WriteFile(filepath.Join(noJSONDir, "feed.csv"), []byte("title\x1fdescription\x1fdate\x1furl\n"), 0644)

	out, code := run("regenerate", "--feed-dir", feedRoot)
	if code != 0 {
		t.Fatalf("regenerate exited %d:\n%s", code, out)
	}

	if _, err := os.Stat(filepath.Join(goodDir, "feed.xml")); err != nil {
		t.Errorf("good-show/feed.xml not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(noJSONDir, "feed.xml")); err == nil {
		t.Error("no-json/feed.xml should not have been created")
	}
}

// TestRegenerateExclude verifies --exclude skips named feeds.
func TestRegenerateExclude(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	writeHosting(t, feedRoot)

	for _, name := range []string{"show-a", "show-b"} {
		dir := filepath.Join(feedRoot, name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "feed.json"), []byte(`{"title":"`+name+`"}`), 0644)
		os.WriteFile(filepath.Join(dir, "feed.csv"), []byte(
			"title\x1fdescription\x1fdate\x1furl\n"+
				"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep.mp3?id=1\n",
		), 0644)
	}

	out, code := run("regenerate", "--feed-dir", feedRoot, "--exclude", "show-b")
	if code != 0 {
		t.Fatalf("regenerate exited %d:\n%s", code, out)
	}

	if _, err := os.Stat(filepath.Join(feedRoot, "show-a", "feed.xml")); err != nil {
		t.Error("show-a/feed.xml should have been created")
	}
	if _, err := os.Stat(filepath.Join(feedRoot, "show-b", "feed.xml")); err == nil {
		t.Error("show-b/feed.xml should not have been created (excluded)")
	}
}

// TestRegenerateSingleFeedAndExcludeMutuallyExclusive verifies the CLI rejects
// the combination of --feed and --exclude.
func TestRegenerateSingleFeedAndExcludeMutuallyExclusive(t *testing.T) {
	_, code := run("regenerate", "--feed", "something", "--exclude", "other")
	if code == 0 {
		t.Error("expected non-zero exit when both --feed and --exclude are set")
	}
}

func TestSelfhostTestResourceExists(t *testing.T) {
	info, err := os.Stat("testdata/test.mp3")
	if err != nil {
		t.Fatalf("testdata/test.mp3 not found: %v", err)
	}
	const want = 103016
	if info.Size() != want {
		t.Errorf("test.mp3 size = %d; want %d", info.Size(), want)
	}
}

// TestSelfhostDstURLHasSlashBetweenBucketAndKey is a regression test for the bug
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
func TestSelfhostDstURLHasSlashBetweenBucketAndKey(t *testing.T) {
	feedRoot := t.TempDir()
	feedDir := filepath.Join(feedRoot, "my-show")
	os.MkdirAll(feedDir, 0755)

	bucket := "my-bucket"
	region := "eu-west-2"

	// Write a CSV whose URL is already at the correct destination — selfhost
	// should detect "already at destination, skipping" without any S3 calls.
	csvContent := fmt.Sprintf(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Episode 1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1fhttps://s3.%s.amazonaws.com/%s/my-show/01-episode-1.mp3\n",
		region, bucket,
	)
	os.WriteFile(filepath.Join(feedDir, "feed.csv"), []byte(csvContent), 0644)

	out, _ := run("selfhost", "my-show", "--feed-dir", feedRoot, "--bucket", bucket, "--region", region)

	// Verify the Dst URL shown in the output contains a / between bucket and key.
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

// writeHostedFeed writes a generated feed (feed.json, feed.csv, feed.xml,
// image.jpg) under feedRoot/slug and a hosting.json pointing at bucket/prefix,
// served from publicURL.
func writeHostedFeed(t *testing.T, feedRoot, slug, bucket, prefix, publicURL string) {
	t.Helper()
	dir := filepath.Join(feedRoot, slug)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(feedRoot, "hosting.json"), []byte(fmt.Sprintf(
		`{"bucket":%q,"region":"eu-west-2","prefix":%q,"public_url":%q}`,
		bucket, prefix, publicURL)), 0644)
	os.WriteFile(filepath.Join(dir, "feed.json"), []byte(`{"title":"Hosted Show","author":"Someone & Co"}`), 0644)
	os.WriteFile(filepath.Join(dir, "feed.csv"), []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1fhttps://example.com/ep1.mp3\n"+
			"Ep2\x1f\x1fSat, 06 Jul 2002 03:00:00 GMT\x1fhttps://example.com/ep2.mp3\n"), 0644)
	os.WriteFile(filepath.Join(dir, "feed.xml"), []byte("<rss/>\n"), 0644)
	os.WriteFile(filepath.Join(dir, "image.jpg"), []byte("not really a jpeg"), 0644)
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
	out, code = run("selfhost-orphans", "hosted-show", "--feed-dir", feedRoot, "--fail-on-orphans")
	if code != 0 {
		t.Fatalf("selfhost-orphans exited %d (feed files reported as orphans?):\n%s", code, out)
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

func TestCSV2RSSUsesHostingURLs(t *testing.T) {
	audio := audioServer(t)
	feedRoot := t.TempDir()
	feedDir := filepath.Join(feedRoot, "hosted-show")
	os.MkdirAll(feedDir, 0755)
	os.WriteFile(filepath.Join(feedRoot, "hosting.json"), []byte(
		`{"bucket":"my-bucket","region":"eu-west-2","public_url":"https://podcasts.example.com"}`), 0644)
	os.WriteFile(filepath.Join(feedDir, "feed.json"), []byte(`{"title":"Hosted Show"}`), 0644)
	os.WriteFile(filepath.Join(feedDir, "feed.csv"), []byte(
		"title\x1fdescription\x1fdate\x1furl\n"+
			"Ep1\x1f\x1fSat, 29 Jun 2002 03:00:00 GMT\x1f"+audio+"/ep1.mp3\n"), 0644)

	out, code := run("csv2rss", filepath.Join(feedDir, "feed.csv"))
	if code != 0 {
		t.Fatalf("csv2rss exited %d:\n%s", code, out)
	}
	xml, _ := os.ReadFile(filepath.Join(feedDir, "feed.xml"))
	src := string(xml)
	base := "https://podcasts.example.com/hosted-show/"
	for _, want := range []string{
		`<atom:link href="` + base + `feed.xml"`,
		`<itunes:new-feed-url>` + base + `feed.xml</itunes:new-feed-url>`,
		`<url>` + base + `image.jpg</url>`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("expected %q in feed.xml:\n%s", want, src)
		}
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
