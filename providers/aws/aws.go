package aws

import (
	"context"
	"errors"
	"fmt"
	"ipspinner/utils"
	"path"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"gopkg.in/ini.v1"
)

type AWS struct {
	awsConfigs        map[string]*awssdk.Config
	fireProxInstances []*FireProx
	stopped           bool
}

//nolint:revive
func (instance AWS) GetName() string {
	return "AWS"
}

func (instance AWS) SummarizeState() string {
	if !instance.IsStopped() {
		return fmt.Sprintf("Provider %s is running with %d launcher(s).", instance.GetName(), len(instance.GetLaunchers()))
	}

	return fmt.Sprintf("Provider %s is stopped.", instance.GetName())
}

func (instance *AWS) GetAvailableLaunchers() []utils.Launcher {
	launchers := make([]utils.Launcher, 0)

	for _, launcher := range instance.GetLaunchers() {
		if launcher.IsAvailable() {
			launchers = append(launchers, launcher)
		}
	}

	return launchers
}

func (instance *AWS) GetLaunchers() []utils.Launcher {
	launchers := make([]utils.Launcher, 0)

	for _, fireProx := range instance.fireProxInstances {
		launchers = append(launchers, fireProx)
	}

	return launchers
}

func (instance AWS) GetNbTotalReqSent() int {
	count := 0

	for _, launcher := range instance.GetLaunchers() {
		count += launcher.GetNbTotalReqSent()
	}

	return count
}

func (instance AWS) IsStopped() bool {
	return instance.stopped
}

func (instance *AWS) Clear() bool {
	utils.Logger.Info().Str("provider", instance.GetName()).Msg("Clearing provider.")

	instance.stopped = true

	fullyCleared := true

	for _, launcher := range instance.GetLaunchers() {
		cleared := launcher.Clear()

		if !cleared {
			fullyCleared = false
		}
	}

	return fullyCleared
}

// Creates and initializes a new instance of the AWS provider object
func Initialize(ctx context.Context, allConfigs *utils.AllConfigs) (*AWS, error) {
	instance := AWS{stopped: false}

	utils.Logger.Info().Str("provider", instance.GetName()).Msg("Configuring provider.")

	awsConfigs, awsConfigsErr := loadAWSConfigs(ctx, allConfigs)

	if awsConfigsErr != nil {
		return &instance, awsConfigsErr
	}

	// If no awssdk.Config has been successfully instantiated => can not go further
	if len(awsConfigs) == 0 {
		return &instance, errors.New("no valid AWS configurations have been set up (please check the provided credentials and regions)")
	}

	instance.awsConfigs = awsConfigs

	if allConfigs.ProvidersConfig.AWSAGEnabled {
		instance.loadFireProxLaunchers(allConfigs)
	}

	if len(instance.GetLaunchers()) == 0 {
		return &instance, errors.New("no launchers could have been created")
	}

	return &instance, nil
}

func (instance *AWS) loadFireProxLaunchers(allConfigs *utils.AllConfigs) {
	maxFireProxInstances := allConfigs.ProvidersConfig.AWSAGMaxInstances
	fireProxTitlePrefix := allConfigs.ProvidersConfig.AWSAGInstanceTitlePrefix
	fireProxDeploymentDescription := allConfigs.ProvidersConfig.AWSAGInstanceDeploymentDescription
	fireProxDeploymentStageDescription := allConfigs.ProvidersConfig.AWSAGInstanceDeploymentStageDescription
	fireProxDeploymentStageName := allConfigs.ProvidersConfig.AWSAGInstanceDeploymentStageName
	rotateAPIGateway := allConfigs.ProvidersConfig.AWSAGRotateNbRequests

	fireProx, fireProxErr := CreateFireProx(instance, maxFireProxInstances, fireProxTitlePrefix, fireProxDeploymentDescription, fireProxDeploymentStageDescription, fireProxDeploymentStageName, rotateAPIGateway)

	if fireProxErr != nil {
		utils.Logger.Error().Err(fireProxErr).Msg("Cannot create FireProx launcher.")

		return
	}

	instance.fireProxInstances = append(instance.fireProxInstances, fireProx)
}

func loadAWSConfigs(ctx context.Context, allConfigs *utils.AllConfigs) (map[string]*awssdk.Config, error) {
	awsConfigs := make(map[string]*awssdk.Config, 0)

	accessKey := allConfigs.ProvidersConfig.AWSAccessKey
	secretKey := allConfigs.ProvidersConfig.AWSSecretKey
	sessionToken := allConfigs.ProvidersConfig.AWSSessionToken

	if len(allConfigs.ProvidersConfig.AWSProfile) > 0 {
		homeDirectory, homeDirectoryErr := utils.GetHomeDirectory()

		if homeDirectoryErr != nil {
			return awsConfigs, homeDirectoryErr
		}

		awsCredentialsFilePath := path.Join(homeDirectory, ".aws", "credentials")

		if !utils.FileExists(awsCredentialsFilePath) {
			return awsConfigs, fmt.Errorf("the aws credentials file does not exist or is not at its default location (%s)", awsCredentialsFilePath)
		}

		cfg, err := ini.Load(awsCredentialsFilePath)

		if err != nil {
			return awsConfigs, errors.New("cannot parse the aws credentials file")
		}

		if !cfg.HasSection(allConfigs.ProvidersConfig.AWSProfile) {
			return awsConfigs, fmt.Errorf("the user %s cannot be found in the aws credentials file", allConfigs.ProvidersConfig.AWSProfile)
		}

		accessKey = cfg.Section(allConfigs.ProvidersConfig.AWSProfile).Key("aws_access_key_id").String()
		secretKey = cfg.Section(allConfigs.ProvidersConfig.AWSProfile).Key("aws_secret_access_key").String()
		sessionToken = cfg.Section(allConfigs.ProvidersConfig.AWSProfile).Key("aws_session_token").String()
	}

	// Instantiates the awssdk.Config for each region
	for _, awsRegion := range allConfigs.ProvidersConfig.AWSRegions {
		utils.Logger.Info().Str("region", awsRegion).Msg("Creating AWS configuration.")

		awsConfig, err := GetConfig(ctx, accessKey, secretKey, sessionToken, awsRegion)

		if err != nil {
			utils.Logger.Warn().Err(err).Str("region", awsRegion).Msg("Can not authenticate to AWS with provided credentials on the region.")
		} else {
			awsConfigs[awsRegion] = &awsConfig
		}
	}

	return awsConfigs, nil
}
