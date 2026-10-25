# Release notes

Newest first. A release's description on GitHub is its section here.

## 0.11.1

### Removed

* **Episode durations**, added in 0.11.0. Most podcast MP3s have no header giving their length, so `durations` could only estimate it from the bitrate, which is a few percent out for variable-bitrate files, for a length apps only show before an episode is downloaded. The `durations` command, the `duration` column and `<itunes:duration>` are gone, and `import` and `update` no longer copy durations from the source. Feeds don't need rebuilding.

## 0.11.0

### Added

* **Episode durations.** `feed.csv` has a new `duration` column, in seconds, which `build` writes out as `<itunes:duration>`, so apps can show how long an episode is before downloading it. The new `durations` command fills it in for episodes without one by running `ffprobe` (part of ffmpeg) on their audio, which reads only as much of each file as it needs. `import` and `update` copy durations from the source feed where it has them. CSVs without the column still work, and anything that rewrites the CSV adds it. Feeds count as stale after upgrading, so rebuild them.

### Changed

* **Feeds no longer add text of their own to descriptions.** An episode without a description used to get `<title> - Episode <n> of <show>`, which only repeated what apps show already; now it has none. Every episode's description also ended with a "Generated using Stephan5/podcastinator" link, and the show's description was wrapped in HTML with a line of the same; both are gone, leaving the credit to `<generator>`, which listeners don't see.
* The show's description is also written as `<itunes:summary>`, which some apps read instead of `<description>`. A show without one gets its title, as RSS requires a description.
* The cover art is also written as `<itunes:image>`, which Apple requires and most apps look for before RSS's own `<image>`.

## 0.10.0

### Changed

* **A feed built by a podcastinator that builds feeds differently counts as out of date**, so `status` shows it as `stale` and `publish` refuses it until it's rebuilt. Before, only changes to `feed.csv`, `feed.json` or `hosting.json` did, so upgrading to a release like 0.9.0 left feeds built the old way looking up to date. Releases that don't change `build`'s output don't make feeds stale. This release counts as a change, so rebuild every feed once after upgrading.

## 0.9.0

### Changed

* Episode titles are no longer prefixed with their number (`12: Title`). Apps that show `<itunes:episode>`, which every episode has carried since 0.7.0, number them already, so the prefix doubled up. GUIDs don't change, so subscribers see the new titles on the same episodes rather than new ones.

### Removed

* `publish` no longer writes `index.json` or `podcasts.opml`; nothing used them. Its check that the public URL works now fetches a feed it uploaded instead of `index.json`. Copies already in the bucket are left there, so delete them by hand.

## 0.8.0

### Changed

* **Episode audio is served from `public_url`**, like feeds and cover art, rather than its S3 URL, so a CloudFront distribution in front of the bucket serves it too. `audio upload` gives new episodes their public URL, and points episodes already in the bucket by their S3 URL at their public one without moving them. Since GUIDs are stored, changing an episode's URL doesn't make it new to subscribers. Without `public_url`, nothing changes.

### Fixed

* `audio prune`, `audio upload` and `status` recognise audio in the bucket by its public URL as well as its S3 URL. Before, an episode whose CSV row had its public URL counted as not hosted, `audio upload` downloaded and re-uploaded it, and **`audio prune` reported it as an orphan, so `--delete` would have deleted it**.

## 0.7.0

### Added

* `"type": "serial"` in `feed.json` marks a show as meant to be heard in order, as `<itunes:type>serial</itunes:type>`, which Apple Podcasts and other apps that honour it list oldest first. Leave it out, or set `"episodic"`, for newest first as before.
* Every episode carries an `<itunes:episode>` number, its position in the CSV, which Apple requires for serial shows.

## 0.6.0

### Removed

* Generated feeds no longer carry `<itunes:new-feed-url>`. It pointed at the feed's own URL so subscribers on an old URL would follow it over; with that move done, it's dropped. To move a feed, redirect the old URL instead.

## 0.5.0

### Changed

* `build` looks up episode lengths 16 at a time instead of one by one, which makes it about ten times faster on a large feed. `--concurrency` changes how many; lower it if a host rate-limits you. Each lookup now times out after 30 seconds, where an unresponsive host used to hang the build.

### Fixed

* `import` and `update` skip items without an `<enclosure>`, such as text posts and trailers. They used to take the item's `<link>` as its audio, which is usually the episode's web page, so `build` published an enclosure pointing at HTML.

## 0.4.0

The commands are reorganised around the three jobs: importing a feed, building it and hosting it. Every command that acts on feeds names them by slug (`build example-show`, not a path to its CSV), and every one that can act on all of them does when none are named.

### Changed

* **Renamed commands.** `regenerate` and `csv2rss` are now `build`, `rss2csv` is `import`, `selfhost` is `audio upload`, `selfhost-orphans` is `audio prune`, `archive` is `audio download` and `pubdate` is `dates`. The old names are gone.
* **Feeds are arguments, not `--feed`.** `build` and `publish` take any number of feed names: `publish example-show`, not `publish --feed example-show`. `--exclude` takes a comma-separated list as before.
* **Config lives only in `feed.json` and `hosting.json`.** The `--title`, `--description`, `--author`, `--website`, `--image-url`, `--feed-url`, `--delimiter`, `--bucket`, `--region` and `--prefix` flags are gone; set the matching keys instead. `--feed-dir` now works on every command.
* **Episode GUIDs are stored in a new `guid` column in `feed.csv`, and new episodes get UUIDs.** A GUID used to be the episode's URL, so moving the audio made podcast apps treat it as a new episode, which is why `selfhost` had to run before anyone subscribed. Now `build` gives each episode a GUID once and it never changes, so `audio upload` is safe at any time. Episodes already published keep their URL as their GUID, taken from the existing `feed.xml` and written exactly as before, so subscribers don't see them again. CSVs without the column still read.
* **`publish` refuses stale feeds.** `build` stamps each `feed.xml` with a hash of its `feed.csv`, `feed.json` and `hosting.json`, and `publish` fails if any feed it would upload doesn't match, where it used to upload whatever was on disk. **Feeds built by 0.3.0 have no stamp, so run `podcastinator build` and commit the result, `feed.csv`'s new `guid` column included, before publishing with 0.4.0. Run it with each feed's existing `feed.xml` in place, as that's where the published GUIDs come from.**
* `audio prune` deletes with `--delete`, which was `--delete-orphans`.
* `dates` takes the input format as `--from`, which was `--input-format`, and it's now required.
* `audio download` takes a feed name rather than a directory, and no longer skips a `cmd.sh`.

### Added

* `import` fetches a URL as well as a local file, and writes `feed.json` and `image.jpg` as well as `feed.csv`, rather than printing what to put in them. It prefers `itunes:image` to `<image>`, converts PNG cover art to JPEG, and won't overwrite an existing feed without `--force`.
* `update` appends episodes newer than a feed's latest from its `source`, a new `feed.json` key that `import` sets.
* `status` shows each feed's episode count, latest episode, whether it's built and up to date, how much of its audio is self-hosted, and with `--remote` whether the bucket has its current `feed.xml` and `image.jpg`.
* `init` creates the feed directory and, with `--bucket`, `hosting.json`.

### Fixed

* Imported descriptions are HTML-escaped, and titles and descriptions have their whitespace, newlines included, collapsed to single spaces. `rss2csv` wrote them raw, so a description with HTML built into invalid XML, and one with a newline broke the CSV.
* Importing no longer takes a feed's `<atom:link>` for its website, which happened when it came after `<link>`.
* `feed.json` is written with `&` rather than `\u0026`.

## 0.3.0

### Changed

* **`feed.json`'s `image_url` and `feed_url` are now `image` and `feed`**, to match `website`. The old keys are ignored, so rename them in your own `feed.json` files. The `--image-url` and `--feed-url` flags, and the keys in the published `index.json`, are unchanged.

### Added

* `podcastinator --version`.
* Each release has binaries for Linux (amd64) and macOS (Apple Silicon and Intel) attached, with their SHA-256 checksums, for installing without Go.

## 0.2.0

### Changed

* **`feed.json`'s `website_url` is now `website`.** The old key is ignored, so a feed that still uses it gets the default website link, this repo, until it's renamed.

## 0.1.0

The first release, split out of `Stephan5/podcasts`, which held both this tool and my own feeds.

* Builds podcast RSS feeds from CSVs (`csv2rss`, `regenerate`), and CSVs from existing feeds (`rss2csv`).
* Copies episode audio into an S3 bucket (`selfhost`), finds audio nothing points at (`selfhost-orphans`), and publishes feeds, cover art, `index.json` and `podcasts.opml` to the bucket (`publish`).
* Downloads a feed's episodes (`archive`) and reformats dates (`pubdate`).
* Installs with `go install github.com/Stephan5/podcastinator/cmd/podcastinator@latest`. The binary is `podcastinator`.
* A feed's website link defaults to this repo, rather than its directory in `Stephan5/podcasts`.
