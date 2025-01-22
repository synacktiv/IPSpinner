package aws

import (
	"context"
	"ipspinner/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const awsMaxAttempts = 30

// Returns an AWS config with provided credentials and information:
func GetConfig(ctx context.Context, accessKey, secretKey, sessionToken, region string) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)),
		config.WithRegion(region),
		config.WithRetryMaxAttempts(awsMaxAttempts),
		config.WithHTTPClient(utils.GetHTTPClient(nil)),
	)

	return cfg, err
}
