package main

import (
	"bytes"
	"context"
	"path"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"k8s.io/klog"
)

// NewMinIO creates a MinIO instance.
func NewMinIO(minioInstances []string, vault VaultConfigurer) MinIO {
	return &MinIOStruct{
		MinioInstances:  minioInstances,
		VaultConfigurer: vault,
	}
}

// MinIO is the interface for interacting with a MinIO instance.
type MinIO interface {
	CreateBucketsForProfile(profileName string) error
}

// MinIOStruct is a MinIO implementation.
type MinIOStruct struct {
	VaultConfigurer VaultConfigurer
	MinioInstances  []string
}

// CreateBucketsForProfile creates the profile's buckets in the MinIO instances.
func (m *MinIOStruct) CreateBucketsForProfile(profileName string) error {
	for _, instance := range m.MinioInstances {
		conf, err := m.VaultConfigurer.GetMinIOConfiguration(instance)
		if err != nil {
			return err
		}

		client, err := minio.New(conf.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(conf.AccessKeyID, conf.SecretAccessKey, ""),
			Secure: conf.UseSSL,
		})
		if err != nil {
			return err
		}

		exists, err := client.BucketExists(context.Background(), profileName)
		if err != nil {
			return err
		}

		if !exists {
			klog.Infof("making bucket %s in instance %s", profileName, instance)
			err = client.MakeBucket(context.Background(), profileName, minio.MakeBucketOptions{})
			if err != nil {
				return err
			}
		}

		// Make shared folder
		_, err = client.PutObject(context.Background(), "shared", path.Join(profileName, ".hold"), bytes.NewReader([]byte{}), 0, minio.PutObjectOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}
