package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Config struct {
	Endpoint          string
	Region            string
	Bucket            string
	AccessKey         string
	SecretKey         string
	UseRole           bool
	BatchSize         int
	FlushIntervalSecs int
}

type S3Writer struct {
	cfg     S3Config
	client  *s3.Client
	entries []Entry
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
}

func NewS3Writer(cfg S3Config) (*S3Writer, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushIntervalSecs <= 0 {
		cfg.FlushIntervalSecs = 60
	}

	var opts []func(*awsconfig.LoadOptions) error
	if cfg.UseRole || (cfg.AccessKey == "" && cfg.SecretKey == "") {
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
		return nil, fmt.Errorf("audit s3: load config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}
	clientOpts = append(clientOpts, func(o *s3.Options) {
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})

	client := s3.NewFromConfig(awsCfg, clientOpts...)
	return &S3Writer{
		cfg:     cfg,
		client:  client,
		entries: make([]Entry, 0, cfg.BatchSize),
		done:    make(chan struct{}),
	}, nil
}

func (w *S3Writer) Start() {
	w.wg.Add(1)
	go w.flushLoop()
	slog.Info("audit s3 writer started", "bucket", w.cfg.Bucket, "batch_size", w.cfg.BatchSize, "interval_secs", w.cfg.FlushIntervalSecs)
}

func (w *S3Writer) Stop() {
	close(w.done)
	w.wg.Wait()
	w.flush()
	slog.Info("audit s3 writer stopped")
}

func (w *S3Writer) Write(entry Entry) {
	w.mu.Lock()
	w.entries = append(w.entries, entry)
	shouldFlush := len(w.entries) >= w.cfg.BatchSize
	w.mu.Unlock()
	if shouldFlush {
		go w.flush()
	}
}

func (w *S3Writer) flushLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(time.Duration(w.cfg.FlushIntervalSecs) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.flush()
		}
	}
}

func (w *S3Writer) flush() {
	w.mu.Lock()
	if len(w.entries) == 0 {
		w.mu.Unlock()
		return
	}
	batch := w.entries
	w.entries = make([]Entry, 0, w.cfg.BatchSize)
	w.mu.Unlock()

	type s3Entry struct {
		OrgID              string         `json:"org_id"`
		UserID             string         `json:"user_id,omitempty"`
		UserEmail          string         `json:"user_email,omitempty"`
		Action             string         `json:"action"`
		ResourceType       string         `json:"resource_type"`
		ResourceID         string         `json:"resource_id,omitempty"`
		ResourceName       string         `json:"resource_name,omitempty"`
		ResourceParentName string         `json:"resource_parent_name,omitempty"`
		Metadata           map[string]any `json:"metadata,omitempty"`
		CreatedAt          time.Time      `json:"created_at"`
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, entry := range batch {
		if err := enc.Encode(s3Entry{
			OrgID:              entry.OrgID,
			UserID:             entry.UserID,
			UserEmail:          entry.UserEmail,
			Action:             entry.Action,
			ResourceType:       entry.ResourceType,
			ResourceID:         entry.ResourceID,
			ResourceName:       entry.ResourceName,
			ResourceParentName: entry.ResourceParentName,
			Metadata:           entry.Metadata,
			CreatedAt:          entry.CreatedAt,
		}); err != nil {
			slog.Error("audit s3: encode entry", "error", err)
			continue
		}
	}

	now := time.Now().UTC()
	orgID := ""
	if len(batch) > 0 {
		orgID = batch[0].OrgID
	}
	key := fmt.Sprintf("audit/org=%s/date=%s/%s-%d.ndjson",
		orgID,
		now.Format("2006-01-02"),
		now.Format("150405"),
		now.UnixNano(),
	)

	body := bytes.NewReader(buf.Bytes())
	_, err := w.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(w.cfg.Bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		slog.Error("audit s3: put object", "key", key, "error", err)
		return
	}
	slog.Debug("audit s3: flushed", "org", orgID, "key", key, "entries", len(batch))
}

func (w *S3Writer) TestConnection(ctx context.Context) error {
	_, err := w.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("audit s3: connection test failed: %w", err)
	}
	return nil
}
