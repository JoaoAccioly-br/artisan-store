package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3ObjectAPI interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// S3Config agrupa os parâmetros para conectar a buckets compatíveis com S3 (AWS S3, Cloudflare R2, MinIO).
type S3Config struct {
	Endpoint      string
	Region        string
	Bucket        string
	AccessKey     string
	SecretKey     string
	PublicBaseURL string
	UsePathStyle  bool
}

// S3Storage implementa Storage enviando objetos para provedores compatíveis com Amazon S3.
type S3Storage struct {
	client        s3ObjectAPI
	bucket        string
	region        string
	endpoint      string
	publicBaseURL string
	usePathStyle  bool
}

// NewS3Storage cria um cliente S3Storage configurado via SDK v2 da AWS.
func NewS3Storage(ctx context.Context, cfg S3Config) (*S3Storage, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	region := strings.TrimSpace(cfg.Region)
	if bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if region == "" {
		return nil, errors.New("s3 region is required")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, errors.New("s3 credentials are required")
	}

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid s3 endpoint %q", endpoint)
		}
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws s3 config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
		// Evita incompatibilidades de aws-chunked/CRC32 em provedores compatíveis como MinIO e Cloudflare R2.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return NewS3StorageWithClient(client, cfg), nil
}

// NewS3StorageWithClient constrói S3Storage injetando um cliente S3 específico (útil para testes unitários).
func NewS3StorageWithClient(client s3ObjectAPI, cfg S3Config) *S3Storage {
	return &S3Storage{
		client:        client,
		bucket:        strings.TrimSpace(cfg.Bucket),
		region:        strings.TrimSpace(cfg.Region),
		endpoint:      strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		usePathStyle:  cfg.UsePathStyle,
	}
}

// Save envia o arquivo para o bucket S3 e retorna sua URL pública.
func (s *S3Storage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	if s.client == nil {
		return "", errors.New("s3 client not initialized")
	}

	cleanKey, err := SanitizeKey(key)
	if err != nil {
		return "", err
	}

	cacheControl := "public, max-age=31536000, immutable"
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(cleanKey),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String(cacheControl),
	})
	if err != nil {
		return "", fmt.Errorf("put s3 object: %w", err)
	}

	return s.objectURL(cleanKey), nil
}

// Delete remove o objeto do bucket S3.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if s.client == nil {
		return errors.New("s3 client not initialized")
	}

	cleanKey, err := SanitizeKey(key)
	if err != nil {
		return err
	}

	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(cleanKey),
	})
	if err != nil {
		return fmt.Errorf("delete s3 object: %w", err)
	}
	return nil
}

func (s *S3Storage) objectURL(cleanKey string) string {
	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/" + cleanKey
	}
	if s.endpoint != "" {
		if s.usePathStyle {
			return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, cleanKey)
		}
		if parsed, err := url.Parse(s.endpoint); err == nil {
			return fmt.Sprintf("%s://%s.%s/%s", parsed.Scheme, s.bucket, parsed.Host, cleanKey)
		}
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, cleanKey)
}
