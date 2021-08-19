package main

import (
	"bytes"
	"context"
	"path"
	"fmt"

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
			return fmt.Errorf("failed reading MinIO configuration for %q: %v", instance, err)
		}

		client, err := minio.New(conf.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(conf.AccessKeyID, conf.SecretAccessKey, ""),
			Secure: conf.UseSSL,
		})
		if err != nil {
			return fmt.Errorf("failed configuring MinIO client for %q: %v", instance, err)
		}

		for _, bucket := range []string{profileName, "shared"} {
			exists, err := client.BucketExists(context.Background(), bucket)
			if err != nil {
				return fmt.Errorf("failed checking if bucket %q exists in %q: %v", bucket, instance, err)
			}

			if !exists {
				klog.Infof("making bucket %q in instance %q", bucket, instance)
				err = client.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{})
				if err != nil {
					return fmt.Errorf("failed making bucket %q in instance %q: %v", bucket, instance, err)
				}
			} else {
				klog.Infof("bucket %q in instance %q already exists", bucket, instance)
			}
		}

		// Make shared folder
		_, err = client.PutObject(context.Background(), "shared", path.Join(profileName, ".hold"), bytes.NewReader([]byte{}), 0, minio.PutObjectOptions{})
		if err != nil {
			return fmt.Errorf("failed writing .hold for %q in %q: %v", profileName, instance, err)
		}
	}

	return nil
}
