// Package s3util provides S3 operations for podcast self-hosting.
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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Client wraps S3 operations.
type Client struct {
	svc    *s3.Client
	region string
}

// New creates a new S3 client for the given region.
func New(ctx context.Context, region string) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	// A custom endpoint (AWS_ENDPOINT_URL, e.g. SeaweedFS in CI) needs path-style addressing.
	usePathStyle := os.Getenv("AWS_ENDPOINT_URL") != ""
	return &Client{
		svc:    s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = usePathStyle }),
		region: region,
	}, nil
}

// UploadReader uploads from r to s3://bucket/key.
func (c *Client) UploadReader(ctx context.Context, bucket, key, contentType string, r io.Reader) error {
	uploader := manager.NewUploader(c.svc)
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
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

// DownloadAndUpload downloads srcURL and uploads to s3://bucket/key.
func (c *Client) DownloadAndUpload(ctx context.Context, srcURL, bucket, key, contentType string) error {
	resp, err := http.Get(srcURL) //nolint:noctx
	if err != nil {
		return fmt.Errorf("download %s: %w", srcURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download %s: HTTP %d", srcURL, resp.StatusCode)
	}

	uploader := manager.NewUploader(c.svc)
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        resp.Body,
		ContentType: optional(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload s3://%s/%s: %w", bucket, key, err)
	}
	return nil
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
	out, err := c.svc.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var respErr *smithyhttp.ResponseError
		if errors.As(err, &respErr) {
			if code := respErr.HTTPStatusCode(); code == http.StatusNotFound || code == http.StatusForbidden {
				return "", nil
			}
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return "", nil
		}
		return "", fmt.Errorf("head s3://%s/%s: %w", bucket, key, err)
	}
	return strings.Trim(aws.ToString(out.ETag), `"`), nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return aws.String(s)
}

// Move copies srcKey to dstKey within the same bucket (or across buckets) then deletes srcKey.
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

// Delete removes an object from S3.
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

// List returns all object keys under the given prefix in the bucket.
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

// ValidateURL checks that a URL returns a non-error HTTP status.
func ValidateURL(rawURL string) error {
	resp, err := http.Head(rawURL) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return nil
}

// HTTPToS3URL converts an HTTPS S3 URL to an s3:// URI.
func HTTPToS3URL(httpURL, bucket string) (resultBucket, key string, err error) {
	// https://s3.REGION.amazonaws.com/BUCKET/KEY
	const awsHost = "amazonaws.com/"
	idx := strings.Index(httpURL, awsHost)
	if idx == -1 {
		return "", "", fmt.Errorf("not an S3 URL: %s", httpURL)
	}
	rest := httpURL[idx+len(awsHost):]
	slashIdx := strings.IndexByte(rest, '/')
	if slashIdx == -1 {
		return rest, "", nil
	}
	return rest[:slashIdx], rest[slashIdx+1:], nil
}
