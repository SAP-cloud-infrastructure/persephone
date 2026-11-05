// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/gophercloud/gophercloud/v2"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// ObjectLockCfg configures S3 Object Lock (WORM) behaviour.
type ObjectLockCfg struct {
	RetentionDays int32
	// Mode is "COMPLIANCE" or "GOVERNANCE".
	Mode string
}

// S3Writer writes batches of audit events as gzip-compressed NDJSON objects
// to a Ceph RadosGW S3-compatible bucket.
type S3Writer struct {
	Logger     logr.Logger
	Client     *s3.Client
	Bucket     string
	Prefix     string
	ObjectLock *ObjectLockCfg
}

// NewS3Writer authenticates with OpenStack, fetches EC2 credentials, and
// returns a ready-to-use S3Writer. s3Endpoint overrides endpoint discovery
// when non-empty (use "https://rgw.st1.<region>.cloud.sap"). If objectLock
// is non-nil, Object Lock will be enabled on the bucket.
func NewS3Writer(ctx context.Context, logger logr.Logger, authOpts gophercloud.AuthOptions, bucket, prefix, s3Endpoint string, objectLock *ObjectLockCfg) (*S3Writer, error) {
	accessKey, secretKey, endpoint, err := FetchEC2Credentials(ctx, authOpts, s3Endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch ec2 credentials: %w", err)
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("default"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	if err := ensureBucket(ctx, client, bucket, objectLock); err != nil {
		return nil, fmt.Errorf("ensure bucket %q: %w", bucket, err)
	}

	return &S3Writer{
		Logger:     logger,
		Client:     client,
		Bucket:     bucket,
		Prefix:     prefix,
		ObjectLock: objectLock,
	}, nil
}

// Write serialises events as gzip NDJSON and puts the object into S3.
// Object key: <prefix><YYYY-MM-DD>/<timestamp>-<uuid>.ndjson.gz
func (w *S3Writer) Write(ctx context.Context, events []auditv1.Event) error {
	if len(events) == 0 {
		return nil
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	now := time.Now().UTC()
	key := fmt.Sprintf("%s%s/%s-%s.ndjson.gz",
		w.Prefix,
		now.Format("2006-01-02"),
		now.Format("20060102T150405Z"),
		uuid.New().String(),
	)

	input := &s3.PutObjectInput{
		Bucket:      aws.String(w.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/gzip"),
	}

	if w.ObjectLock != nil {
		retainUntil := now.AddDate(0, 0, int(w.ObjectLock.RetentionDays))
		input.ObjectLockRetainUntilDate = aws.Time(retainUntil)
		switch w.ObjectLock.Mode {
		case "COMPLIANCE":
			input.ObjectLockMode = s3types.ObjectLockModeCompliance
		case "GOVERNANCE":
			input.ObjectLockMode = s3types.ObjectLockModeGovernance
		default:
			return fmt.Errorf("unknown object lock mode %q", w.ObjectLock.Mode)
		}
	}

	if _, err := w.Client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", w.Bucket, key, err)
	}

	w.Logger.Info("flushed audit events to s3", "bucket", w.Bucket, "key", key, "count", len(events))
	return nil
}

func ensureBucket(ctx context.Context, client *s3.Client, bucket string, objectLock *ObjectLockCfg) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	}
	if objectLock != nil {
		input.ObjectLockEnabledForBucket = aws.Bool(true)
	}

	_, err := client.CreateBucket(ctx, input)
	if err == nil {
		return nil
	}

	// Bucket already exists — that's fine.
	var alreadyOwned *s3types.BucketAlreadyOwnedByYou
	var alreadyExists *s3types.BucketAlreadyExists
	if errors.As(err, &alreadyOwned) || errors.As(err, &alreadyExists) {
		return nil
	}
	return err
}
