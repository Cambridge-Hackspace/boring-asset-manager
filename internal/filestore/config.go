package filestore

import (
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// Open returns the store the storage settings name: the directory, or an
// S3 bucket. The settings are checked when the configuration is loaded.
func Open(cfg config.StorageConfig) (Blobs, error) {
	switch cfg.Backend {
	case "", config.StorageLocal:
		return NewLocal(cfg.LocalPath), nil
	case config.StorageS3:
		return NewS3(S3Options{
			Endpoint: cfg.S3.Endpoint, Region: cfg.S3.Region, Bucket: cfg.S3.Bucket, Prefix: cfg.S3.Prefix,
			PathStyle: cfg.S3.PathStyle, AccessKeyID: cfg.S3.AccessKeyID, SecretAccessKey: cfg.S3.SecretAccessKey,
		})
	}
	return nil, fmt.Errorf("filestore: unknown storage backend %q", cfg.Backend)
}
