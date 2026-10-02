package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type R2Config struct {
	AccessKeyID string
	SecretKey   string
	Bucket      string
	Endpoint    string
}

type s3Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type presigner interface {
	PresignGetObject(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type R2Store struct {
	client    s3Client
	presigner presigner
	bucket    string
}

func NewR2(ctx context.Context, cfg R2Config) (*R2Store, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("R2 storage credentials and bucket are required")
	}
	if err := validateEndpoint(cfg.Endpoint); err != nil {
		return nil, err
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("configure R2 storage: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = true
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		options.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return &R2Store{client: client, presigner: s3.NewPresignClient(client), bucket: cfg.Bucket}, nil
}

func NewWithClients(bucket string, client s3Client, signer presigner) (*R2Store, error) {
	if bucket == "" || client == nil || signer == nil {
		return nil, fmt.Errorf("storage test clients and bucket are required")
	}
	return &R2Store{client: client, presigner: signer, bucket: bucket}, nil
}

func (s *R2Store) Put(ctx context.Context, key string, body io.Reader, contentType string, size int64) (ObjectInfo, error) {
	if body == nil || key == "" || size < 0 || contentType == "" {
		return ObjectInfo{}, fmt.Errorf("storage put requires key, body, content type, and size")
	}
	output, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("store object: %w", err)
	}
	info := ObjectInfo{Key: key, ContentType: contentType, Size: size}
	if output.ETag != nil {
		info.ETag = aws.ToString(output.ETag)
	}
	return info, nil
}

func (s *R2Store) Get(ctx context.Context, key string) (Object, error) {
	if key == "" {
		return Object{}, fmt.Errorf("storage get requires a key")
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return Object{}, fmt.Errorf("get object: %w", err)
	}
	return Object{Info: ObjectInfo{Key: key, ContentType: aws.ToString(output.ContentType), Size: aws.ToInt64(output.ContentLength), ETag: aws.ToString(output.ETag)}, Body: output.Body}, nil
}

func (s *R2Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("storage delete requires a key")
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (s *R2Store) PresignGet(ctx context.Context, key string, ttlSeconds int64) (string, error) {
	if key == "" {
		return "", fmt.Errorf("presign requires a key")
	}
	if err := validatePresignTTL(ttlSeconds); err != nil {
		return "", err
	}
	result, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(options *s3.PresignOptions) {
		options.Expires = time.Duration(ttlSeconds) * time.Second
	})
	if err != nil {
		return "", fmt.Errorf("presign object: %w", err)
	}
	return result.URL, nil
}

func validateEndpoint(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("R2 endpoint must be a valid https URL")
	}
	return nil
}
