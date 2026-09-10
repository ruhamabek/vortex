package minio

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
)

type MinIOStorage struct{
	client *minio.Client
	bucket string
}

func NewMinIOStorage(client *minio.Client, bucket string) *MinIOStorage {
	return &MinIOStorage{
		client: client,
		bucket: bucket,
	}
}

func (s *MinIOStorage) PutStorage(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	opts := minio.PutObjectOptions{
			ContentType: contentType,
		}

	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, opts)
	if err != nil {
		return fmt.Errorf("failed to upload object to minio: %w", err)
	}
	
	return nil
}

func (s *MinIOStorage) GetObject(ctx context.Context, key string)(io.ReadCloser, error){
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from minio: %v", err)
	}

	_,err = obj.Stat()
	if err != nil {
		obj.Close()
		return nil, fmt.Errorf("object not found or unreadble: %v", err)
	}

	return obj, nil
}

func (s *MinIOStorage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration)(string, error){
	reqParams := make(url.Values)
	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, reqParams)

	if err != nil {
		return "", fmt.Errorf("failed to generate presigned url: %v", err)
	}

	return presignedURL.String(), nil
}

func (s *MinIOStorage) DeleteObject(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object from minio: %v", err)
	}

	return nil
}