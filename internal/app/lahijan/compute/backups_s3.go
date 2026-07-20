// Package compute: backups_s3.go is the S3-compatible BackupTarget driver.
// Works against any S3 API endpoint that supports SigV4 (AWS S3, MinIO,
// SeaweedFS S3, etc.). Uses the AWS SDK for Go v2 service/s3 client that
// is already in the dep tree (brought in by WS-13 SeaweedFS).
package compute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Driver implements BackupTarget against an S3-compatible endpoint.
// The driver is safe for concurrent use: the underlying *awss3.Client is
// goroutine-safe and the SDK's internal connection pool handles
// parallel requests.
type S3Driver struct {
	client *awss3.Client
	bucket string
	prefix string
}

// NewS3Driver builds an S3Driver from the per-kind config + decrypted
// secret. The driver has not been pinged; the caller SHOULD call Ping
// before the first upload.
func NewS3Driver(cfg BackupTargetConfig, secret BackupTargetSecret) *S3Driver {
	region := cfg.S3Region
	if region == "" {
		region = "us-east-1"
	}
	client := awss3.New(awss3.Options{
		Region:       region,
		Credentials:  awscredentials.NewStaticCredentialsProvider(secret.S3AccessKeyID, secret.S3SecretKey, ""),
		BaseEndpoint: aws.String(cfg.S3Endpoint),
		UsePathStyle: cfg.S3ForcePathStyle,
	})
	prefix := strings.Trim(cfg.S3Prefix, "/")
	return &S3Driver{client: client, bucket: cfg.S3Bucket, prefix: prefix}
}

// Kind returns the driver's kind slug.
func (d *S3Driver) Kind() string { return BackupTargetKindS3 }

// Ping verifies the bucket exists + the credentials can read it.
func (d *S3Driver) Ping(ctx context.Context) error {
	if d.bucket == "" {
		return errors.New("compute: s3 backup target: bucket is required")
	}
	if _, err := d.client.HeadBucket(ctx, &awss3.HeadBucketInput{
		Bucket: aws.String(d.bucket),
	}); err != nil {
		return fmt.Errorf("%w: s3 head bucket: %w", ErrBackupTargetUnreachable, err)
	}
	return nil
}

// Upload streams body to S3 under the joined prefix+key. The SHA-256 is
// computed in-memory from the bytes read so the worker can record it on
// the backup row; for very large backups this is a future optimisation
// candidate (multipart upload with hash-on-the-fly).
func (d *S3Driver) Upload(ctx context.Context, key string, body io.Reader) (int64, string, error) {
	fullKey := d.fullKey(key)
	// Tee the body through a hasher so we get both size + checksum in one
	// pass. The SDK's PutObject takes an io.Reader; we satisfy it with
	// the tee'd reader.
	h := sha256.New()
	n := int64(0)
	tee := teeCount{r: body, h: h, n: &n}
	_, err := d.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(fullKey),
		Body:   &tee,
	})
	if err != nil {
		return 0, "", fmt.Errorf("compute: s3 upload: %w", err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// Delete removes the object at key. Idempotent: a missing key returns nil
// so retry-on-failure does not double-error.
func (d *S3Driver) Delete(ctx context.Context, key string) error {
	_, err := d.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.fullKey(key)),
	})
	if err == nil {
		return nil
	}
	// S3 already returns "NoSuchKey" for missing objects on DELETE in most
	// cases; treat any 404-class error as success.
	if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "404") {
		return nil
	}
	return fmt.Errorf("compute: s3 delete: %w", err)
}

// Download fetches the object at key. The caller owns the returned reader
// and MUST Close it.
func (d *S3Driver) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := d.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.fullKey(key)),
	})
	if err != nil {
		return nil, fmt.Errorf("compute: s3 download: %w", err)
	}
	return out.Body, nil
}

// fullKey joins the configured prefix (if any) with the per-object key.
// Stored keys always use "/" as separator so they round-trip across
// operating systems.
func (d *S3Driver) fullKey(key string) string {
	if d.prefix == "" {
		return strings.TrimLeft(key, "/")
	}
	return d.prefix + "/" + strings.TrimLeft(key, "/")
}

// teeCount is a minimal io.Reader that counts bytes read + feeds them to
// a hasher. We do not use io.TeeReader because we also need the byte
// count without an extra wrapper; combining the two avoids one
// allocation per upload.
type teeCount struct {
	r io.Reader
	h interface{ Write([]byte) (int, error) }
	n *int64
}

// Read implements io.Reader.
func (t *teeCount) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		*t.n += int64(n)
		// Hash errors are unreachable for sha256; swallow to satisfy the
		// io.Reader contract.
		_, _ = t.h.Write(p[:n])
	}
	return n, err
}
