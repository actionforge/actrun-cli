package nodes

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

//go:embed storage-provider-s3@v1.yml
var storageProviderS3Definition string

type StorageProviderS3 struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StorageProviderS3) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	bucket, err := core.InputValueById[string](c, n, ni.Core_storage_provider_s3_v1_Input_bucket)
	if err != nil {
		return nil, err
	}

	region, err := core.InputValueById[string](c, n, ni.Core_storage_provider_s3_v1_Input_region)
	if err != nil {
		return nil, err
	}

	credentials, err := core.InputValueById[core.Credentials](c, n, ni.Core_storage_provider_s3_v1_Input_credentials)
	if err != nil {
		return nil, err
	}

	accessKey, accessSecret, err := convertCredentialToAccessKeySecret(credentials)
	if err != nil {
		return nil, core.CreateErr(c, err, "failed to convert credentials to git auth method")
	}

	endpoint, err := core.InputValueById[string](c, n, ni.Core_storage_provider_s3_v1_Input_endpoint)
	if err != nil {
		return nil, err
	}

	return S3StorageProvider{
		accessKey: accessKey,
		secretKey: accessSecret,
		region:    region,
		bucket:    bucket,
		endpoint:  endpoint,
	}, nil
}

func createS3Config(accessKey, secretKey, region, endpoint string) (aws.Config, error) {

	var cfgOptions []func(*config.LoadOptions) error

	if endpoint == "" && region == "" {
		return aws.Config{}, errors.New("endpoint and region are both not set")
	}

	if accessKey != "" || secretKey != "" {
		staticProvider := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
		cfgOptions = append(cfgOptions, config.WithCredentialsProvider(staticProvider))
	}

	if region != "" {
		cfgOptions = append(cfgOptions, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), cfgOptions...)
	if endpoint == "" && region != "" {
		// somehow the auto detection of the endpoint never worked for me, so use this instead
		cfg.BaseEndpoint = aws.String(fmt.Sprintf("https://s3.%s.amazonaws.com", region))
	} else {
		if !strings.HasPrefix(endpoint, "http") {
			endpoint = "https://" + endpoint
		}
		cfg.BaseEndpoint = aws.String(endpoint)
	}

	if err != nil {
		return aws.Config{}, core.CreateErr(nil, err, "failed to load AWS sdk config")
	}

	cfg.RequestChecksumCalculation = aws.RequestChecksumCalculationUnset
	cfg.Logger = nil

	return cfg, nil
}

func init() {
	err := core.RegisterNodeFactory(storageProviderS3Definition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StorageProviderS3{}, nil
	})
	if err != nil {
		panic(err)
	}
}

type S3StorageProvider struct {
	accessKey string
	secretKey string
	endpoint  string
	region    string
	bucket    string
}

func (n S3StorageProvider) GetName() string {
	return "aws-s3"
}

func (n S3StorageProvider) ListObjects(dir string) (StorageList, error) {
	cfg, err := createS3Config(n.accessKey, n.secretKey, n.region, n.endpoint)
	if err != nil {
		return StorageList{}, err
	}

	if strings.HasPrefix(dir, "/") {
		return StorageList{}, errors.New("dir path must not start with a slash '/'")
	}

	dir = strings.TrimRight(dir, "/") + "/"

	s3Client := s3.NewFromConfig(cfg)
	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(n.bucket),
		Prefix:    aws.String(dir),
		Delimiter: aws.String("/"),
	}

	var dirs []string
	var objects []string

	truncatedListing := true
	for truncatedListing {
		resp, err := s3Client.ListObjectsV2(context.Background(), input)
		if err != nil {
			return StorageList{}, err
		}

		for _, item := range resp.Contents {
			// the root dir is included in the listing, we don't want it
			if *item.Key == dir {
				continue
			}
			objects = append(objects, *item.Key)
		}

		for _, prefix := range resp.CommonPrefixes {
			// the root dir is included in the listing, we don't want it
			if *prefix.Prefix == dir {
				continue
			}

			*prefix.Prefix = strings.TrimSuffix(*prefix.Prefix, "/")
			dirs = append(dirs, *prefix.Prefix)
		}

		input.ContinuationToken = resp.NextContinuationToken
		truncatedListing = *resp.IsTruncated
	}

	return StorageList{
		Objects: objects,
		Dirs:    dirs,
	}, nil
}

func (n S3StorageProvider) UploadObject(name string, data io.Reader) error {
	cfg, err := createS3Config(n.accessKey, n.secretKey, n.region, n.endpoint)
	if err != nil {
		return err
	}

	s3Client := s3.NewFromConfig(cfg)
	uploader := manager.NewUploader(s3Client)

	_, err = uploader.Upload(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(n.bucket),
		Key:    aws.String(name),
		Body:   data,
	})
	if err != nil {
		return err
	}

	return nil
}

func (n S3StorageProvider) CanClone(src core.StorageProvider) bool {
	_, ok := src.(S3StorageProvider)
	return ok
}

func (n S3StorageProvider) CloneObject(dstName string, src core.StorageProvider, srcName string) error {

	srcProvider, ok := src.(S3StorageProvider)
	if !ok {
		// should never happen, before clone, call CanClone()
		return core.CreateErr(nil, nil, "source provider is not an AWS S3 provider")
	}

	cfg, err := createS3Config(n.accessKey, n.secretKey, n.region, n.endpoint)
	if err != nil {
		return err
	}

	s3Client := s3.NewFromConfig(cfg)

	_, err = s3Client.CopyObject(context.Background(), &s3.CopyObjectInput{
		Bucket:     aws.String(n.bucket),
		CopySource: aws.String(srcProvider.bucket + "/" + srcName),
		Key:        aws.String(dstName),
	})
	if err != nil {
		return err
	}

	return nil
}

func (n S3StorageProvider) DownloadObject(name string) (io.Reader, error) {
	cfg, err := createS3Config(n.accessKey, n.secretKey, n.region, n.endpoint)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(cfg)

	resp, err := s3Client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(n.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func (n S3StorageProvider) DeleteFile(path string) error {
	cfg, err := createS3Config(n.accessKey, n.secretKey, n.region, n.endpoint)
	if err != nil {
		return err
	}

	s3Client := s3.NewFromConfig(cfg)

	deleteInput := &s3.DeleteObjectInput{
		Bucket: aws.String(n.bucket),
		Key:    aws.String(path),
	}

	_, err = s3Client.DeleteObject(context.Background(), deleteInput)
	if err != nil {
		return err
	}

	return nil
}

func convertCredentialToAccessKeySecret(cred core.Credentials) (string, string, error) {
	if upCred, ok := cred.(*UserPassCredentials); ok {
		return upCred.Username, upCred.Password, nil
	} else if akCred, ok := cred.(AccessKeyCredentials); ok {
		return akCred.AccessKey, akCred.AccessPassword, nil
	}
	return "", "", errors.New("unsupported credential type for access key/secret")
}
