package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	client *s3.Client
	bucket string
}

type S3Config struct {
	Endpoint  string // leave empty for AWS; set for Garage/self-hosted
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

func NewS3Storage(cfg S3Config) (Storage, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.AccessKey == "" && cfg.SecretKey == "" {
		// Use default credential chain — picks up IRSA, env vars, EC2 metadata, etc.
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	} else {
		opts = append(opts,
			awsconfig.WithRegion(cfg.Region),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfg.AccessKey, cfg.SecretKey, "",
			)),
		)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}
	// Disable trailing checksums for plain HTTP (dev with Garage)
	clientOpts = append(clientOpts, func(o *s3.Options) {
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})

	client := s3.NewFromConfig(awsCfg, clientOpts...)
	return &S3Storage{client: client, bucket: cfg.Bucket}, nil
}

func (s *S3Storage) Put(id string, r io.Reader, size int64, mimeType string) error {
	// Buffer entire content (S3 SDK requires a seekable stream without TLS)
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r); err != nil {
		return fmt.Errorf("s3: buffer %s: %w", id, err)
	}
	body := bytes.NewReader(buf.Bytes())
	contentLength := int64(buf.Len())

	_, err := s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(id),
		Body:          body,
		ContentLength: aws.Int64(contentLength),
		ContentType:   aws.String(mimeType),
	})
	if err != nil {
		return fmt.Errorf("s3: put %s: %w", id, err)
	}
	return nil
}

func (s *S3Storage) Get(id string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(id),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: get %s: %w", id, err)
	}
	return out.Body, nil
}

func (s *S3Storage) Delete(id string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(id),
	})
	if err != nil {
		return fmt.Errorf("s3: delete %s: %w", id, err)
	}
	return nil
}
