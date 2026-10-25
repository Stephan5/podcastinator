# Release notes

Newest first. A release's description on GitHub is its section here.

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
