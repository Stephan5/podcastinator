# podcastinator
![](https://github.com/Stephan5/podcastinator/actions/workflows/main.yml/badge.svg)

A CLI for building podcast RSS feeds from CSVs and hosting them yourself in S3. Each feed is a directory holding a CSV of episodes and a little metadata. `podcastinator` turns that into a `feed.xml`, can copy the episode audio into your bucket, and publishes the feeds there for any podcast app to subscribe to.

It's handy for archiving shows that are disappearing, stitching together episodes from different sources into one feed, or turning a pile of audio files into something your podcast app understands.

## Install

```shell
go install github.com/Stephan5/podcastinator/cmd/podcastinator@latest
```

Or build from a clone, which writes `build/podcastinator`:

```shell
make build
```

## Quick start

[`example/`](example) is a complete feed directory to copy:

```
feed/
  hosting.json        ← where feeds are hosted (shared by every feed)
  example-show/
    feed.json         ← title, description, author
    feed.csv          ← one row per episode
    image.jpg         ← cover art (Apple wants 1400–3000px square)
```

```shell
cp -r example/feed ./feed
# edit feed/hosting.json to point at your bucket
podcastinator regenerate          # builds feed/example-show/feed.xml
podcastinator publish             # uploads it, then subscribe at <public_url>/example-show/feed.xml
```

Commands look for `./feed` in the current directory; `--feed-dir` points them elsewhere.

### The CSV

Columns are `title`, `description`, `date`, `url`, separated by the ASCII unit separator (`\x1F`) by default so titles and descriptions can contain commas and quotes without escaping. There's no quoting at all. Dates must be RFC 2822 (`Mon, 06 Jan 2025 09:00:00 GMT`); `pubdate` converts other formats. An empty description becomes `<title> - Episode <n> of <show>`.

Episodes are numbered in CSV order, and each episode's URL is its GUID, so changing an episode's URL makes podcast apps treat it as a new episode.

### `feed.json`

```json
{
  "title": "Example Show",
  "description": "A two-episode feed showing the layout podcastinator expects.",
  "author": "Example Author"
}
```

Optional fields: `delimiter` (default `\u001f`), `website` (defaults to this repo), `image` and `feed`, the cover art and feed URLs (default to their location in the bucket, from `hosting.json`).

## Hosting

Hosting is configured once, in `feed/hosting.json`:

```json
{
  "bucket": "my-podcast-bucket",
  "region": "eu-west-2",
  "public_url": "https://podcasts.example.com"
}
```

`public_url` is the address that serves the bucket root, and defaults to `https://s3.<region>.amazonaws.com/<bucket>`. It applies to feeds, cover art and the index files. Episode audio uploaded by `selfhost` keeps its S3 URL, because that URL is the episode's GUID. The optional `prefix` puts everything under an S3 key prefix (default none). The `--bucket`, `--region` and `--prefix` flags override the file.

The bucket ends up laid out as:

```
<prefix>/
  index.json               ← every feed's title, URLs and episode count
  podcasts.opml            ← every feed, to import into a podcast app in one go
  <feed>/
    feed.xml
    image.jpg
    001-episode-title.mp3  ← uploaded by selfhost
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

If a feed's subscribers are on an old URL, run `publish` first so the feed exists at its new home, then `regenerate`. Every generated feed carries `<itunes:new-feed-url>` pointing at its own feed URL, so if the old URL keeps serving the regenerated feed for a few weeks, apps that honour the tag move over on their own ([Apple: change the RSS feed URL](https://podcasters.apple.com/support/837-change-the-rss-feed-url)).

### Publishing from CI

Keep your `feed/` directory in a repo of its own and publish on every push. With GitHub Actions and an IAM role assumed through GitHub's OIDC provider, no AWS secrets are stored anywhere:

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
      - run: go install github.com/Stephan5/podcastinator/cmd/podcastinator@v0.3.0
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

### `regenerate` — rebuild feeds

```shell
podcastinator regenerate                                  # every feed
podcastinator regenerate --feed example-show              # one feed
podcastinator regenerate --exclude "some-feed,another"    # all but these
podcastinator regenerate --feed-dir ./somewhere/feed
```

Runs `csv2rss` on every `feed/<slug>/feed.csv` that has a `feed.json` alongside it.

### `csv2rss` — CSV → RSS

```shell
podcastinator csv2rss ./feed/example-show/feed.csv
podcastinator csv2rss ./feed/example-show/feed.csv --title "Override Title"
```

Writes `feed.xml` alongside the CSV, using `feed.json` from the same directory; flags override it. The feed and image URLs come from `--feed-url`/`--image-url`, then `feed.json`, then their location in the bucket from `hosting.json`, and it fails if none of those supplies them. Sends a `HEAD` request for each episode to fill in its length. Skips writing if the feed content hasn't changed.

### `rss2csv` — RSS → CSV

```shell
podcastinator rss2csv ./feed/example-show/feed.xml            # input already in feed/<slug>/
podcastinator rss2csv ~/Downloads/show.xml --repo-dir my-show # input anywhere else
```

Parses an existing feed and writes `feed/<slug>/feed.csv`, sorted by publication date, overwriting any CSV already there. It prints the source feed's title, description, link and image URL to help you write `feed.json`. Override the feed directory with `--feed-dir`.

### `selfhost` — copy episodes into the bucket

```shell
podcastinator selfhost example-show
podcastinator selfhost example-show --bucket another-bucket --region us-east-1 --prefix rss
```

Downloads each episode that isn't already in the bucket, uploads it to `<prefix>/<slug>/` with a numbered, slugified name (`001-episode-title.mp3`) and an audio `Content-Type`, and rewrites the CSV with the new URLs. Since URLs are GUIDs, do this before anyone subscribes, or they'll see every episode again.

### `selfhost-orphans` — find unreferenced audio

```shell
podcastinator selfhost-orphans example-show --fail-on-orphans --delete-orphans
```

Compares the objects under `<prefix>/<slug>/` with the URLs in the CSV and reports, and optionally deletes, files nothing points at. `feed.xml` and `image.jpg` are never orphans. Exits `2` when orphans are found and `--fail-on-orphans` is set.

### `publish` — upload feeds

```shell
podcastinator publish                     # every feed
podcastinator publish --feed example-show # one feed
podcastinator publish --dry-run           # report what would change
```

Uploads each feed's `feed.xml` and `image.jpg` to `<prefix>/<slug>/`, then writes `index.json` and `podcasts.opml` to the bucket root, always covering every feed. Files that already match the bucket by ETag are skipped. Uploads `feed.xml` as it is on disk, so run `regenerate` first after changing a CSV.

### `archive` — download a feed's episodes

```shell
podcastinator archive ./feed/example-show ./archive
```

Downloads all episode audio to `archive/example-show/items/` and writes a `local.csv` with `file://items/<filename>` URLs.

### `pubdate` — reformat dates

```shell
podcastinator pubdate ./feed/example-show/feed.csv --input-format "%b %d, %Y"
```

Parses the date column with a strftime format and rewrites it as RFC 2822. Backs up the original to `.old`.

## Development

```shell
make test               # unit tests (internal/... cmd/podcastinator/...)
make test-integration   # build the binary and run the black-box tests in test/
make test-all           # both
```

The `publish` integration tests need an S3 endpoint (`AWS_ENDPOINT_URL`) and skip without one. CI runs them against [SeaweedFS](https://github.com/seaweedfs/seaweedfs); see `.github/workflows/main.yml` to do the same locally.

## License

[MIT](LICENSE)
