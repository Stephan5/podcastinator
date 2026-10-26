// Package s3util wraps the S3 calls podcastinator makes.
package s3util

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type Client struct {
	svc *s3.Client
	tm  *transfermanager.Client
}

// New returns a client for region, with credentials from the environment.
func New(ctx context.Context, region string) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	// A custom endpoint (AWS_ENDPOINT_URL, e.g. SeaweedFS in CI) needs path-style addressing.
	usePathStyle := os.Getenv("AWS_ENDPOINT_URL") != ""
	svc := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = usePathStyle })
	return &Client{svc: svc, tm: transfermanager.New(svc)}, nil
}

// UploadReader uploads from r to s3://bucket/key.
func (c *Client) UploadReader(ctx context.Context, bucket, key, contentType string, r io.Reader) error {
	_, err := c.tm.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: optional(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

// downloadAttempts is how many times DownloadAndUpload tries before giving up.
// A dropped connection an hour into a long upload shouldn't end it.
const downloadAttempts = 3

// DownloadAndUpload streams srcURL to s3://bucket/key, starting again from
// the top if the download or upload fails, unless srcURL answers with a 4xx.
func (c *Client) DownloadAndUpload(ctx context.Context, srcURL, bucket, key, contentType string) error {
	var (
		err   error
		retry bool
	)
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		if attempt > 1 {
			wait := time.Duration(attempt-1) * 5 * time.Second
			fmt.Printf("  Warning: %v. Retrying in %s.\n", err, wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		if retry, err = c.downloadAndUpload(ctx, srcURL, bucket, key, contentType); err == nil || !retry {
			return err
		}
	}
	return err
}

// downloadAndUpload reports whether a failure is worth retrying.
func (c *Client) downloadAndUpload(ctx context.Context, srcURL, bucket, key, contentType string) (bool, error) {
	resp, err := http.Get(srcURL) //nolint:noctx
	if err != nil {
		return true, fmt.Errorf("download %s: %w", srcURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resp.StatusCode >= 500, fmt.Errorf("download %s: HTTP %d", srcURL, resp.StatusCode)
	}
	return true, c.UploadReader(ctx, bucket, key, contentType, resp.Body)
}

// PutBytes uploads a small object in a single PutObject, so its ETag is the
// MD5 of body (with SSE-S3 or no encryption).
func (c *Client) PutBytes(ctx context.Context, bucket, key string, body []byte, contentType, cacheControl string) error {
	_, err := c.svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(body),
		ContentType:  optional(contentType),
		CacheControl: optional(cacheControl),
	})
	if err != nil {
		return fmt.Errorf("put s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

// ETag returns the object's ETag without quotes, or "" if it does not exist.
// Without s3:ListBucket, S3 answers 403 rather than 404 for a missing key, so
// both are treated as missing; a real permissions problem surfaces on upload.
func (c *Client) ETag(ctx context.Context, bucket, key string) (string, error) {
	out, err := c.head(ctx, bucket, key)
	if out == nil {
		return "", err
	}
	return strings.Trim(aws.ToString(out.ETag), `"`), nil
}

// Size returns the object's size in bytes, or -1 if it does not exist,
// treating 403 as missing as ETag does.
func (c *Client) Size(ctx context.Context, bucket, key string) (int64, error) {
	out, err := c.head(ctx, bucket, key)
	if out == nil {
		return -1, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

// head returns nil, with no error, for a missing object.
func (c *Client) head(ctx context.Context, bucket, key string) (*s3.HeadObjectOutput, error) {
	out, err := c.svc.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if respErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok {
			if code := respErr.HTTPStatusCode(); code == http.StatusNotFound || code == http.StatusForbidden {
				return nil, nil
			}
		}
		if apiErr, ok := errors.AsType[smithy.APIError](err); ok && apiErr.ErrorCode() == "NotFound" {
			return nil, nil
		}
		return nil, fmt.Errorf("head s3://%s/%s: %w", bucket, key, err)
	}
	return out, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return aws.String(s)
}

// Move copies an object to dstBucket/dstKey, then deletes the original.
func (c *Client) Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	copySource := fmt.Sprintf("%s/%s", srcBucket, srcKey)
	_, err := c.svc.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(copySource),
	})
	if err != nil {
		return fmt.Errorf("copy s3://%s/%s → s3://%s/%s: %w", srcBucket, srcKey, dstBucket, dstKey, err)
	}
	if err := c.Delete(ctx, srcBucket, srcKey); err != nil {
		return fmt.Errorf("delete original s3://%s/%s after copy: %w", srcBucket, srcKey, err)
	}
	return nil
}

// Delete deletes s3://bucket/key.
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.svc.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

// List returns every key in bucket under prefix.
func (c *Client) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(c.svc, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	return keys, nil
}

// ValidateURL fails unless a HEAD request for rawURL succeeds.
func ValidateURL(rawURL string) error {
	_, err := HeadSize(rawURL)
	return err
}

// HeadSize fails unless a HEAD request for rawURL succeeds, and returns the
// Content-Length it reports, or -1 if it reports none.
func HeadSize(rawURL string) (int64, error) {
	resp, err := http.Head(rawURL) //nolint:noctx
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return -1, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return resp.ContentLength, nil
}
