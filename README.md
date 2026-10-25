# podcastinator
![](https://github.com/Stephan5/podcastinator/actions/workflows/main.yml/badge.svg)

A CLI for building podcast RSS feeds from CSVs and hosting them yourself in S3. Each feed is a directory holding a CSV of episodes and a little metadata. `podcastinator` can import that from an existing podcast's feed, builds a `feed.xml` from it, can copy the episode audio into your bucket, and publishes the feeds there for any podcast app to subscribe to.

It's handy for archiving shows that are disappearing, stitching together episodes from different sources into one feed, or turning a pile of audio files into something your podcast app understands.

## Install

```shell
go install github.com/Stephan5/podcastinator/cmd/podcastinator@v0.7.0
```

Or download a binary for Linux or macOS from the [latest release](https://github.com/Stephan5/podcastinator/releases/latest), or build from a clone, which writes `build/podcastinator`:

```shell
make build
```

macOS blocks binaries downloaded from the internet that aren't signed. Clear the flag before the first run: `xattr -d com.apple.quarantine podcastinator`.

## Quick start

```shell
podcastinator init --bucket my-podcast-bucket --public-url https://podcasts.example.com
podcastinator import https://example.com/podcast.rss   # creates feed/<show>/
podcastinator audio upload <show>                       # optional: host the audio too
podcastinator build                                     # builds feed/<show>/feed.xml
podcastinator publish                                   # subscribe at <public_url>/<show>/feed.xml
```

`podcastinator status` shows where each feed has got to. Commands look for `./feed` in the current directory; `--feed-dir` points them elsewhere. Commands that take `[feed...]` act on every feed when none are named.

To make a feed by hand rather than importing one, [`example/`](example) is a complete feed directory to copy:

```
feed/
  hosting.json        ← where feeds are hosted (shared by every feed)
  example-show/
    feed.json         ← title, description, author, website
    feed.csv          ← one row per episode
    image.jpg         ← cover art (Apple wants 1400–3000px square)
```

```shell
cp -r example/feed ./feed
# edit feed/hosting.json to point at your bucket
podcastinator build example-show
podcastinator publish
```

### The CSV

Columns are `title`, `description`, `date`, `url`, `guid`, separated by the ASCII unit separator (`\x1F`) by default so titles and descriptions can contain commas and quotes without escaping. There's no quoting at all. Descriptions are stored HTML-escaped (`&lt;p&gt;`), as they go into the feed verbatim. Dates must be RFC 2822 (`Mon, 06 Jan 2025 09:00:00 GMT`); `dates` converts other formats. An empty description becomes `<title> - Episode <n> of <show>`. Every description ends with a "Generated using Stephan5/podcastinator" link.

Episodes are numbered in CSV order. The `guid` is what podcast apps identify an episode by, so it must never change. Leave it empty on a new row and `build` fills it in with a UUID; after that, the episode's URL can change (say, when `audio upload` moves it into your bucket) without apps seeing it as new. CSVs without the column still work, and `build` adds it. Episodes already published before the column existed keep their URL as their GUID, taken from the existing `feed.xml`, so subscribers don't see them again.

### `feed.json`

```json
{
  "title": "Example Show",
  "description": "A two-episode feed showing the layout podcastinator expects.",
  "author": "Example Author",
  "website": "https://example.com/example-show"
}
```

| Key           | Required | What it is                                                                                     |
|---------------|----------|------------------------------------------------------------------------------------------------|
| `title`       | Yes      | The show's name.                                                                               |
| `description` | No       | The show's description.                                                                        |
| `author`      | No       | Who makes it.                                                                                  |
| `website`     | No       | The show's own site, the feed's `<link>`. Defaults to this repo.                               |
| `image`       | No       | The cover art's URL. Defaults to `<public_url>/<slug>/image.jpg`, from `hosting.json`.         |
| `feed`        | No       | The feed's own URL, its self link. Defaults to `<public_url>/<slug>/feed.xml`, from `hosting.json`. |
| `delimiter`   | No       | The CSV's field separator. Defaults to the unit separator, `"\u001f"`.                           |
| `source`      | No       | The RSS feed the episodes came from, which `update` fetches new ones from. Set by `import`.    |
| `type`        | No       | `"serial"` for a show meant to be heard in order, which apps that honour `itunes:type` (Apple Podcasts does) list oldest first. Defaults to episodic, newest first. |

## Hosting

Hosting is configured once, in `feed/hosting.json`:

```json
{
  "bucket": "my-podcast-bucket",
  "region": "eu-west-2",
  "public_url": "https://podcasts.example.com"
}
```

`public_url` is the address that serves the bucket root, and defaults to `https://s3.<region>.amazonaws.com/<bucket>`. It applies to feeds, cover art and the index files. Episode audio uploaded by `audio upload` is served from its S3 URL. The optional `prefix` puts everything under an S3 key prefix (default none). `podcastinator init --bucket <bucket>` writes the file for you.

The bucket ends up laid out as:

```
<prefix>/
  index.json               ← every feed's title, URLs and episode count
  podcasts.opml            ← every feed, to import into a podcast app in one go
  <feed>/
    feed.xml
    image.jpg
    001-episode-title.mp3  ← uploaded by audio upload
```

Objects need to be publicly readable, which is simplest with a bucket policy granting `s3:GetObject` on `arn:aws:s3:::<bucket>/*`.

### Custom domain (CloudFront)

To serve feeds from your own domain, map it straight onto the bucket root so `https://<domain>/<feed>/feed.xml` is the object `<feed>/feed.xml`:

1. Request an ACM certificate for the domain in **us-east-1**, the only region CloudFront takes certificates from ([AWS docs](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/cnames-and-https-requirements.html)), and validate it via DNS.
2. Create a CloudFront distribution:
   - Origin: the bucket, `<bucket>.s3.<region>.amazonaws.com`.
   - Alternate domain name: your domain, with the certificate from step 1.
   - Viewer protocol policy: redirect HTTP to HTTPS.
   - Cache policy `CachingOptimized`. It honours each object's `Cache-Control`, which `publish` sets to 5 minutes for feeds and the index and a day for cover art, so no invalidations are needed.
   - No default root object and no custom error responses, so a missing feed is a 404 rather than an HTML page podcast apps choke on.
3. Point a DNS record for the domain at the distribution.

Then set `public_url` to `https://<domain>`. `publish` checks that `<public_url>/index.json` is reachable after uploading, so a broken domain shows up there rather than in podcast apps.

### Moving a feed

If a feed's subscribers are on an old URL, change `hosting.json` (or `feed` in `feed.json`), `build` and `publish`, then have the old URL return a 301 redirect to the new one for a few weeks so podcast apps move over ([Apple: change the RSS feed URL](https://podcasters.apple.com/support/837-change-the-rss-feed-url)). Generated feeds don't carry `<itunes:new-feed-url>`.

### Publishing from CI

Keep your `feed/` directory in a repo of its own, commit the built `feed.xml` files, and publish on every push. `publish` refuses a feed whose `feed.csv`, `feed.json` or `hosting.json` changed since its `feed.xml` was built, so a push that forgets to rebuild fails rather than publishing a stale feed. With GitHub Actions and an IAM role assumed through GitHub's OIDC provider, no AWS secrets are stored anywhere:

```yaml
jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.PUBLISH_ROLE_ARN }}
          aws-region: eu-west-2
      - run: go install github.com/Stephan5/podcastinator/cmd/podcastinator@v0.6.0
      - run: podcastinator publish
```

Pin a version rather than `@latest`, so a change here can't change your feeds without you noticing.

The role's trust policy should only accept your repo's default branch:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::<account-id>:oidc-provider/token.actions.githubusercontent.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:<owner>/<repo>:ref:refs/heads/<branch>"
        }
      }
    }
  ]
}
```

`StringEquals` is exact and case-sensitive. If the job fails with `Not authorized to perform sts:AssumeRoleWithWebIdentity`, compare the `sub` with the failed attempts in CloudTrail:

```sh
aws cloudtrail lookup-events --region <region> \
  --lookup-attributes AttributeKey=EventName,AttributeValue=AssumeRoleWithWebIdentity \
  --max-results 5 --query 'Events[].CloudTrailEvent' --output text \
  | jq '{time: .eventTime, error: .errorCode, sub: .userIdentity.userName}'
```

Give the role an inline policy that can only touch the feed files and the index, so a bad push can't touch the audio:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "s3:ListBucket",
      "Resource": "arn:aws:s3:::<bucket>"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject"],
      "Resource": [
        "arn:aws:s3:::<bucket>/*/feed.xml",
        "arn:aws:s3:::<bucket>/*/image.jpg",
        "arn:aws:s3:::<bucket>/index.json",
        "arn:aws:s3:::<bucket>/podcasts.opml"
      ]
    }
  ]
}
```

`s3:ListBucket` lets `publish` tell a missing file (404) from a forbidden one; `s3:GetObject` covers the `HeadObject` it uses to skip unchanged files.

## Commands

| Job       | Command                                      | Does                                                                  |
|-----------|----------------------------------------------|-----------------------------------------------------------------------|
| Set up    | `init [--bucket b --public-url u]`           | Creates `feed/` and `hosting.json`                                    |
| Import    | `import <url\|file> [--slug s] [--force]`    | Creates a feed from an existing podcast's RSS feed                    |
|           | `update [feed...]`                           | Adds new episodes from each feed's `source`                           |
| Generate  | `build [feed...] [--exclude a,b] [--concurrency n]` | Builds `feed.xml` from `feed.csv` and `feed.json`                     |
|           | `dates <feed> --from <format>`               | Rewrites a hand-written CSV's dates as RFC 2822                       |
| Self-host | `audio upload <feed>`                        | Copies a feed's episodes into the bucket and points its CSV at them   |
|           | `audio prune <feed> [--delete]`              | Finds audio in the bucket the CSV no longer points at                 |
|           | `audio download <feed> <dir>`                | Downloads a feed's episodes for safekeeping                           |
|           | `publish [feed...] [--dry-run]`              | Uploads feeds, cover art and the index to the bucket                  |
|           | `status [feed...] [--remote]`                | Shows where each feed is between import and publish                   |

`podcastinator <command> --help` has the details.

### `import` — start a feed from an existing podcast

```shell
podcastinator import https://example.com/podcast.rss
podcastinator import ~/Downloads/show.xml --slug my-show
```

Fetches the feed and writes `feed/<slug>/feed.csv` (oldest episode first), `feed.json` (title, description, author, website and `source`) and `image.jpg`, preferring `itunes:image` and converting PNGs to JPEG. Every episode gets a new UUID as its GUID. The slug comes from the show's title unless `--slug` says otherwise. It won't overwrite an existing feed without `--force`, which gives every episode a new GUID, so subscribers would see them all again.

### `update` — pick up new episodes

```shell
podcastinator update               # every feed with a source
podcastinator update example-show
```

Re-fetches each feed's `source` and appends the episodes dated after the newest one in its CSV. It matches on date rather than URL, because self-hosted episodes no longer have their source URLs. Run `audio upload` and `build` afterwards.

### `build` — CSV → RSS

```shell
podcastinator build                               # every feed
podcastinator build example-show another-show     # just these
podcastinator build --exclude some-feed,another   # all but these
```

Writes `feed/<slug>/feed.xml` from its `feed.csv` and `feed.json`. The feed and image URLs come from `feed` and `image` in `feed.json`, then their location in the bucket from `hosting.json`, and it fails if neither supplies them. Gives any episode without a GUID one, writing it to the CSV. Numbers each episode with `itunes:episode`, in CSV order, matching the `N:` in its title. Sends a `HEAD` request for each episode to fill in its length, 16 at a time by default; `--concurrency` changes that. Skips writing if the feed hasn't changed. Each `feed.xml` ends with a comment holding a hash of its inputs, which `publish` and `status` check.

### `dates` — reformat dates

```shell
podcastinator dates example-show --from "%b %d, %Y"
```

Parses the CSV's date column with a strftime format and rewrites it as RFC 2822. Backs up the original to `feed.csv.old`.

### `audio upload` — copy episodes into the bucket

```shell
podcastinator audio upload example-show
```

Downloads each episode that isn't already in the bucket, uploads it to `<prefix>/<slug>/` with a numbered, slugified name (`001-episode-title.mp3`) and an audio `Content-Type`, and rewrites the CSV with the new URLs. Local `file://` URLs, relative to the feed directory, are uploaded directly. Episodes keep their GUIDs, so subscribers don't see moved episodes as new.

### `audio prune` — find unreferenced audio

```shell
podcastinator audio prune example-show                     # report
podcastinator audio prune example-show --delete            # and delete
podcastinator audio prune example-show --fail-on-orphans   # exit 2 if any
```

Compares the objects under `<prefix>/<slug>/` with the URLs in the CSV and reports, and with `--delete` deletes, files nothing points at. `feed.xml` and `image.jpg` are never orphans.

### `audio download` — keep a local copy

```shell
podcastinator audio download example-show ./archive
```

Downloads all episode audio to `archive/example-show/items/`, copies the feed's files alongside, and writes a `local.csv` with `file://items/<filename>` URLs.

### `publish` — upload feeds

```shell
podcastinator publish                  # every feed
podcastinator publish example-show     # just this one
podcastinator publish --dry-run        # report what would change
```

Uploads each feed's `feed.xml` and `image.jpg` to `<prefix>/<slug>/`, then writes `index.json` and `podcasts.opml` to the bucket root, always covering every feed. Files that already match the bucket by ETag are skipped. Refuses to upload anything if a feed's `feed.xml` is out of date with its `feed.csv`, `feed.json` or `hosting.json`. Never touches episode audio.

### `status` — what state is everything in

```
$ podcastinator status
FEED           EPISODES   LATEST                 BUILT   AUDIO            SOURCE
another-show   214        2024-11-02T06:00:00Z   stale   214/214 hosted   yes
example-show   2          2025-01-13T09:00:00Z   ok      0/2 hosted       -
```

`BUILT` is `ok`, `stale` (rebuild it) or `missing`. `--remote` adds a `PUBLISHED` column comparing the local `feed.xml` and `image.jpg` with the bucket's, which needs AWS credentials.

## Development

```shell
make test               # unit tests (internal/... cmd/podcastinator/...)
make test-integration   # build the binary and run the black-box tests in test/
make test-all           # both
```

`TestImportFixture` imports [`test/testdata/import/source.xml`](test/testdata/import/source.xml), a feed shaped like a third-party host's, and compares the result with the files in `want/`; add a case by editing the source and the expected files together.

The `publish` and `audio prune` integration tests need an S3 endpoint (`AWS_ENDPOINT_URL`) and skip without one. CI runs them against [SeaweedFS](https://github.com/seaweedfs/seaweedfs); see `.github/workflows/main.yml` to do the same locally.

### Releasing

Set `version` in [`cmd/podcastinator/version.go`](cmd/podcastinator/version.go) and add a section at the top of [`RELEASE_NOTES.md`](RELEASE_NOTES.md), headed `## <version>`, and commit them. Older versions' sections stay below as the history. Once that's on `master`, tag it and push the tag:

```shell
make release   # git tag v<version> && git push origin v<version>
```

It refuses unless you're on a clean `master` that matches `origin/master`, the version isn't already tagged, and the release notes have its section, so a bad tag never reaches GitHub. Pushing the tag runs the tests, builds binaries for Linux and macOS, and attaches them to a GitHub release with that version's section as its description (see [`release.yml`](.github/workflows/release.yml)). The workflow refuses to release if the tag doesn't match the version, or the release notes have no section for it. Push the tag rather than making the release on GitHub, which would leave the workflow nothing to create.

## License

[MIT](LICENSE)
