# Release notes

Newest first. A release's description on GitHub is its section here.

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

* Imported descriptions are HTML-escaped and flattened onto one line. `rss2csv` wrote them raw, so a description with HTML built into invalid XML, and one with a newline broke the CSV.
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
