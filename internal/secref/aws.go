package secref

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type AWSSecretsManagerConfig struct {
	Region  string
	Profile string
}

type DefaultSource struct {
	EnvSource

	cfg     AWSSecretsManagerConfig
	osStore OSStore

	once      sync.Once
	client    secretsManagerClient
	clientErr error
}

type secretsManagerClient interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

func NewDefaultSource(cfg AWSSecretsManagerConfig) *DefaultSource {
	return &DefaultSource{cfg: cfg, osStore: NewOSStore()}
}

func NewDefaultSourceWithOSStore(cfg AWSSecretsManagerConfig, store OSStore) *DefaultSource {
	return &DefaultSource{cfg: cfg, osStore: store}
}

func (s *DefaultSource) GetOSSecretString(ctx context.Context, id string) (string, error) {
	if s == nil || s.osStore == nil {
		return "", ErrOSStoreUnavailable
	}
	value, err := s.osStore.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func (s *DefaultSource) GetAWSSecretString(ctx context.Context, secretID string) (string, error) {
	secretID = strings.TrimSpace(secretID)
	if secretID == "" {
		return "", fmt.Errorf("aws secret id is empty")
	}
	client, err := s.awsClient(ctx)
	if err != nil {
		return "", err
	}
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return "", err
	}
	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	if len(out.SecretBinary) > 0 {
		return "", fmt.Errorf("aws secret %q has SecretBinary; only SecretString is supported", secretID)
	}
	return "", fmt.Errorf("aws secret %q has no SecretString", secretID)
}

func (s *DefaultSource) awsClient(ctx context.Context) (secretsManagerClient, error) {
	s.once.Do(func() {
		opts := []func(*awsconfig.LoadOptions) error{}
		if region := strings.TrimSpace(s.cfg.Region); region != "" {
			opts = append(opts, awsconfig.WithRegion(region))
		}
		if profile := strings.TrimSpace(s.cfg.Profile); profile != "" {
			opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			s.clientErr = err
			return
		}
		s.client = secretsmanager.NewFromConfig(cfg)
	})
	if s.clientErr != nil {
		return nil, s.clientErr
	}
	if s.client == nil {
		return nil, fmt.Errorf("aws secrets manager client is nil")
	}
	return s.client, nil
}
