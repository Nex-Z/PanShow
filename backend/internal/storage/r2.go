package storage

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"panshow/backend/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type FileEntry struct {
	Name                string     `json:"name"`
	Path                string     `json:"path"`
	IsDir               bool       `json:"isDir"`
	Size                int64      `json:"size"`
	LastModified        *time.Time `json:"lastModified,omitempty"`
	ContentType         string     `json:"contentType,omitempty"`
	MetadataUnavailable bool       `json:"metadataUnavailable,omitempty"`
}

type Client struct {
	s3             *s3.Client
	presigner      *s3.PresignClient
	bucket         string
	rootPrefix     string
	publicBaseURL  *url.URL
	requestTimeout time.Duration
}

func NewR2Client(ctx context.Context, cfg config.Config) (*Client, error) {
	if cfg.R2Endpoint == "" || cfg.R2AccessKey == "" || cfg.R2SecretKey == "" || cfg.R2Bucket == "" {
		return nil, errors.New("R2 endpoint, access key, secret key and bucket are required")
	}
	publicBaseURL, err := parsePublicBaseURL(cfg.R2PublicBaseURL)
	if err != nil {
		return nil, err
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.R2Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.R2AccessKey, cfg.R2SecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.R2Endpoint)
		options.UsePathStyle = true
		if cfg.R2MaxAttempts > 0 {
			options.RetryMaxAttempts = cfg.R2MaxAttempts
		}
	})
	return &Client{
		s3:             client,
		presigner:      s3.NewPresignClient(client),
		bucket:         cfg.R2Bucket,
		rootPrefix:     cleanPrefix(cfg.R2RootPrefix),
		publicBaseURL:  publicBaseURL,
		requestTimeout: cfg.R2RequestTimeout,
	}, nil
}

func (c *Client) List(ctx context.Context, dir string) ([]FileEntry, error) {
	ctx, cancel := c.withRequestTimeout(ctx)
	defer cancel()

	prefix := c.keyForDir(dir)
	entries := make([]FileEntry, 0)
	var continuationToken *string
	for {
		out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		for _, commonPrefix := range out.CommonPrefixes {
			key := aws.ToString(commonPrefix.Prefix)
			sitePath := c.sitePathFromKey(strings.TrimSuffix(key, "/"))
			entries = append(entries, FileEntry{
				Name:  path.Base(sitePath),
				Path:  sitePath,
				IsDir: true,
			})
		}
		for _, object := range out.Contents {
			key := aws.ToString(object.Key)
			if key == prefix {
				continue
			}
			sitePath := c.sitePathFromKey(key)
			entries = append(entries, FileEntry{
				Name:         path.Base(sitePath),
				Path:         sitePath,
				IsDir:        false,
				Size:         aws.ToInt64(object.Size),
				LastModified: object.LastModified,
				ContentType:  mime.TypeByExtension(path.Ext(sitePath)),
			})
		}
		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil {
			break
		}
		continuationToken = out.NextContinuationToken
	}
	return entries, nil
}

func (c *Client) Stat(ctx context.Context, filePath string) (FileEntry, error) {
	ctx, cancel := c.withRequestTimeout(ctx)
	defer cancel()

	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.keyForFile(filePath)),
	})
	if err != nil {
		return FileEntry{}, err
	}
	return FileEntry{
		Name:         path.Base(filePath),
		Path:         filePath,
		IsDir:        false,
		Size:         aws.ToInt64(out.ContentLength),
		LastModified: out.LastModified,
		ContentType:  aws.ToString(out.ContentType),
	}, nil
}

func (c *Client) PresignDownload(ctx context.Context, filePath string, ttl time.Duration) (string, error) {
	if publicURL, ok := c.publicObjectURL(filePath); ok {
		return publicURL, nil
	}
	return c.presignGetObject(ctx, filePath, ttl, downloadContentDisposition(filePath))
}

func (c *Client) PresignPreview(ctx context.Context, filePath string, ttl time.Duration) (string, error) {
	if publicURL, ok := c.publicObjectURL(filePath); ok {
		return publicURL, nil
	}
	return c.presignGetObject(ctx, filePath, ttl, "inline")
}

func (c *Client) HasPublicBaseURL() bool {
	return c.publicBaseURL != nil
}

func FallbackFileEntry(filePath string) FileEntry {
	return FileEntry{
		Name:                path.Base(filePath),
		Path:                filePath,
		IsDir:               false,
		ContentType:         mime.TypeByExtension(path.Ext(filePath)),
		MetadataUnavailable: true,
	}
}

func downloadContentDisposition(filePath string) string {
	filename := path.Base(filePath)
	if filename == "" || filename == "." || filename == "/" {
		filename = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallbackDownloadFilename(filename), url.PathEscape(filename))
}

func fallbackDownloadFilename(filename string) string {
	var builder strings.Builder
	for _, r := range filename {
		switch {
		case r == '"' || r == '\\' || r == '/' || r == '\r' || r == '\n':
			builder.WriteByte('_')
		case r >= 0x20 && r <= 0x7e:
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	fallback := strings.TrimSpace(builder.String())
	if fallback == "" || fallback == "." || fallback == ".." {
		return "download"
	}
	return fallback
}

func (c *Client) presignGetObject(ctx context.Context, filePath string, ttl time.Duration, disposition string) (string, error) {
	ctx, cancel := c.withRequestTimeout(ctx)
	defer cancel()

	out, err := c.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(c.bucket),
		Key:                        aws.String(c.keyForFile(filePath)),
		ResponseContentDisposition: aws.String(disposition),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *Client) keyForDir(dir string) string {
	if dir == "/" {
		if c.rootPrefix == "" {
			return ""
		}
		return c.rootPrefix + "/"
	}
	return c.keyForFile(dir) + "/"
}

func (c *Client) keyForFile(filePath string) string {
	trimmed := strings.TrimPrefix(filePath, "/")
	if c.rootPrefix == "" {
		return trimmed
	}
	if trimmed == "" {
		return c.rootPrefix
	}
	return c.rootPrefix + "/" + trimmed
}

func (c *Client) sitePathFromKey(key string) string {
	site := strings.TrimPrefix(key, c.rootPrefix)
	site = strings.TrimPrefix(site, "/")
	if site == "" {
		return "/"
	}
	return "/" + site
}

func cleanPrefix(prefix string) string {
	return strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/")
}

func parsePublicBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse R2 public base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("R2 public base URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("R2 public base URL must include a host")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func (c *Client) publicObjectURL(filePath string) (string, bool) {
	if c.publicBaseURL == nil {
		return "", false
	}
	publicURL := *c.publicBaseURL
	key := c.keyForFile(filePath)
	if key != "" {
		publicURL.Path = strings.TrimRight(publicURL.Path, "/") + "/" + key
		publicURL.RawPath = ""
	}
	return publicURL.String(), true
}

func (c *Client) withRequestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.requestTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}
