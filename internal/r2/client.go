package r2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	appconfig "github.com/backlink-orchestrator/internal/config"
)

type Client struct {
	s3Client      *s3.Client
	presignClient *s3.PresignClient
	bucket        string
}

func NewClient(ctx context.Context, cfg *appconfig.Config) (*Client, error) {
	if cfg.R2Endpoint == "" || cfg.R2AccessKeyID == "" || cfg.R2SecretAccess == "" || cfg.R2Bucket == "" {
		return nil, fmt.Errorf("R2 configuration is incomplete")
	}

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               cfg.R2Endpoint,
			HostnameImmutable: true, // Cloudflare R2 requires this
			SigningRegion:     "auto",
		}, nil
	})

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.R2AccessKeyID, cfg.R2SecretAccess, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(awsCfg)
	presignClient := s3.NewPresignClient(s3Client)

	return &Client{
		s3Client:      s3Client,
		presignClient: presignClient,
		bucket:        cfg.R2Bucket,
	}, nil
}

func (c *Client) GeneratePresignedPutURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	req, err := c.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiration
	})
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

type ObjectMetadata struct {
	ContentLength int64
}

func (c *Client) VerifyObjectExists(ctx context.Context, key string) (*ObjectMetadata, error) {
	resp, err := c.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	var length int64
	if resp.ContentLength != nil {
		length = *resp.ContentLength
	}

	return &ObjectMetadata{
		ContentLength: length,
	}, nil
}
