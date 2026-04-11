package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
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
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	IsDir        bool       `json:"isDir"`
	Size         int64      `json:"size"`
	LastModified *time.Time `json:"lastModified,omitempty"`
	ContentType  string     `json:"contentType,omitempty"`
}

type Client struct {
	s3         *s3.Client
	presigner  *s3.PresignClient
	bucket     string
	rootPrefix string
}

func NewR2Client(ctx context.Context, cfg config.Config) (*Client, error) {
	if cfg.R2Endpoint == "" || cfg.R2AccessKey == "" || cfg.R2SecretKey == "" || cfg.R2Bucket == "" {
		return nil, errors.New("R2 endpoint, access key, secret key and bucket are required")
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
	})
	return &Client{
		s3:         client,
		presigner:  s3.NewPresignClient(client),
		bucket:     cfg.R2Bucket,
		rootPrefix: cleanPrefix(cfg.R2RootPrefix),
	}, nil
}

func (c *Client) List(ctx context.Context, dir string) ([]FileEntry, error) {
	prefix := c.keyForDir(dir)
	out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(c.bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, err
	}

	entries := make([]FileEntry, 0, len(out.CommonPrefixes)+len(out.Contents))
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
	return entries, nil
}

func (c *Client) Stat(ctx context.Context, filePath string) (FileEntry, error) {
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

func (c *Client) ReadText(ctx context.Context, filePath string) (string, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.keyForFile(filePath)),
	})
	if err != nil {
		return "", err
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Client) PresignDownload(ctx context.Context, filePath string, ttl time.Duration) (string, error) {
	return c.presignGetObject(ctx, filePath, ttl, fmt.Sprintf("attachment; filename=%q", path.Base(filePath)))
}

func (c *Client) PresignPreview(ctx context.Context, filePath string, ttl time.Duration) (string, error) {
	return c.presignGetObject(ctx, filePath, ttl, "inline")
}

func (c *Client) presignGetObject(ctx context.Context, filePath string, ttl time.Duration, disposition string) (string, error) {
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
