package uniai

import (
	"context"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
)

func ResolveBedrockCredentials(ctx context.Context, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("bedrock config is nil")
	}

	opts := []func(*awsconfig.LoadOptions) error{}
	if region := strings.TrimSpace(cfg.AwsRegion); region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile := strings.TrimSpace(cfg.AwsProfile); profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}

	accessKey := strings.TrimSpace(cfg.AwsKey)
	secretKey := strings.TrimSpace(cfg.AwsSecret)
	sessionToken := strings.TrimSpace(cfg.AwsSessionToken)
	hasAccessKey := accessKey != ""
	hasSecretKey := secretKey != ""
	hasSessionToken := sessionToken != ""

	if hasAccessKey && hasSecretKey {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			awscredentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		))
	} else if hasAccessKey || hasSecretKey || hasSessionToken {
		return fmt.Errorf("bedrock static credentials incomplete: aws_key and aws_secret must both be set when either is provided (aws_session_token is optional)")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("resolve bedrock aws config: %w", err)
	}
	creds, err := awsCfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("retrieve bedrock aws credentials: %w", err)
	}

	cfg.AwsKey = strings.TrimSpace(creds.AccessKeyID)
	cfg.AwsSecret = strings.TrimSpace(creds.SecretAccessKey)
	cfg.AwsSessionToken = strings.TrimSpace(creds.SessionToken)
	if strings.TrimSpace(cfg.AwsRegion) == "" {
		cfg.AwsRegion = strings.TrimSpace(awsCfg.Region)
	}
	return nil
}
