package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"ipspinner/utils"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigateway/types"
)

/*
	All credit to the peeps as Black Hills Information Security, this is simply a Golang implementation of their FireProx tool.
	https://github.com/ustayready/fireprox
*/

// Launcher object
type FireProx struct {
	provider                           *AWS
	apiGatewaysPerRegion               map[string][]*APIGateway
	creatingAPIGatewayInstanceInRegion []string // contains regions
	maxAPIGatewayInstances             int
	fireProxTitlePrefix                string
	fireProxDeploymentDescription      string
	fireProxDeploymentStageDescription string
	fireProxDeploymentStageName        string
	rotateAPIGateway                   int // nb requests for rotating
	stopped                            bool
}

type APIGateway struct {
	launcher          *FireProx
	awsConfig         *awssdk.Config
	AllRegisteredURLs []*url.URL
	Title             string
	RestAPIID         string
	Deleted           bool
	Updating          bool
	Deleting          bool
	NbReqSent         int
}

func CreateFireProx(provider *AWS, maxFPInstances int, fpTitlePrefix, fpDeploymentDescription, fpDeploymentStageDescription, fpDeploymentStageName string, rotateAPIGateway int) (*FireProx, error) {
	instance := FireProx{
		provider:                           provider,
		apiGatewaysPerRegion:               make(map[string][]*APIGateway, 0),
		creatingAPIGatewayInstanceInRegion: make([]string, 0),
		maxAPIGatewayInstances:             maxFPInstances,
		fireProxTitlePrefix:                fpTitlePrefix,
		fireProxDeploymentDescription:      fpDeploymentDescription,
		fireProxDeploymentStageDescription: fpDeploymentStageDescription,
		fireProxDeploymentStageName:        fpDeploymentStageName,
		rotateAPIGateway:                   rotateAPIGateway,
	}

	utils.Logger.Info().Str("launcher", instance.GetName()).Msg("Creating launcher.")

	return &instance, nil
}

//nolint:revive
func (instance FireProx) GetName() string {
	return "FireProx (via API Gateways)"
}

func (instance *FireProx) GetProvider() utils.Provider {
	return instance.provider
}

func (instance FireProx) GetNbTotalReqSent() int {
	nbTotal := 0

	for _, gateways := range instance.apiGatewaysPerRegion {
		for _, gateway := range gateways {
			nbTotal += gateway.NbReqSent
		}
	}

	return nbTotal
}

func (instance FireProx) SummarizeState() string {
	return fmt.Sprintf("Launcher %s : nbTotalRequestsSent=%d, nbAPIGateways=%d", instance.GetName(), instance.GetNbTotalReqSent(), instance.getNbAPIGatewayInstances())
}

// Returns the number of configured and not deleted FireProx instances
func (instance FireProx) getNbAPIGatewayInstances() int {
	nb := 0

	for _, instances := range instance.apiGatewaysPerRegion {
		for _, instance := range instances {
			if !instance.Deleted && !instance.Deleting {
				nb++
			}
		}
	}

	return nb
}

// Preloads all given hosts by creating one or multiple FireProx instances with all URLs ready to fetch
func (instance *FireProx) PreloadHosts(ctx context.Context, allPreloadHosts []*url.URL) {
	for _, awsConfig := range instance.provider.awsConfigs {
		// If the maximum of created instances has been reached
		if instance.getNbAPIGatewayInstances() >= instance.maxAPIGatewayInstances {
			utils.Logger.Warn().Int("nbAPIGatewayInstancesRunning", instance.getNbAPIGatewayInstances()).Int("maxAPIGatewayInstances", instance.maxAPIGatewayInstances).Str("region", awsConfig.Region).Msg("Can not preload hosts for AWS in this region because the maximum number of FireProx instances has been reached.")

			continue
		}

		title := fmt.Sprintf("%s_%s", instance.fireProxTitlePrefix, strconv.FormatInt(time.Now().UnixMicro(), 10)) //nolint:revive

		// If more than utils.MaxResourcePerFireProxInstance need to be added, it subdivises them into multiple sublists for creating multiple instances
		subdivisedPreloadHosts := utils.SubdiviseSlice(allPreloadHosts, utils.MaxResourcePerFireProxInstance)

		for _, preloadHosts := range subdivisedPreloadHosts {
			if instance.getNbAPIGatewayInstances() >= instance.maxAPIGatewayInstances {
				utils.Logger.Warn().Int("max", instance.maxAPIGatewayInstances).Msg("The maximum number of API Gateway instances has been reached, can not create new ones for the remaining preloading hosts.")

				continue
			}

			// It creates a new FireProx instance
			newAPIGatewayInstance, newAPIGatewayInstanceErr := instance.CreateAPIGateway(ctx, awsConfig, preloadHosts, title)

			// If it can not create the instance, it rejects the request
			if newAPIGatewayInstanceErr != nil {
				utils.Logger.Warn().Err(newAPIGatewayInstanceErr).Str("region", awsConfig.Region).Msg("Can not create the API Gateway instance for the preloading hosts in this region.")

				continue
			}

			utils.Logger.Debug().Int("preloadHostsNb", len(preloadHosts)).Str("region", awsConfig.Region).Str("createdInstanceID", newAPIGatewayInstance.RestAPIID).Msg("Creating API Gateway instance for preloading hosts.")

			// It stores the FireProx instance
			instance.apiGatewaysPerRegion[awsConfig.Region] = append(utils.GetOrDefault(instance.apiGatewaysPerRegion, awsConfig.Region, []*APIGateway{}), newAPIGatewayInstance)
		}
	}
}

// Creates an API Gateway instance
func (instance *FireProx) CreateAPIGateway(ctx context.Context, awsConfig *awssdk.Config, urls []*url.URL, title string) (*APIGateway, error) {
	if len(urls) > utils.MaxResourcePerFireProxInstance {
		return nil, fmt.Errorf("can not create a FireProx instance with this number of resources (%d)", len(urls))
	}

	client := apigateway.NewFromConfig(*awsConfig)

	templateBytes := APIGateway{
		launcher:          instance,
		Title:             title,
		AllRegisteredURLs: urls,
		awsConfig:         awsConfig,
	}.generateOpenAPISpecification()

	importRestAPIResp, importRestAPIErr := client.ImportRestApi(ctx, &apigateway.ImportRestApiInput{
		Body: templateBytes,
		Parameters: map[string]string{
			"endpointConfigurationTypes": "REGIONAL",
		},
	}, apigateway.WithAPIOptions())

	if importRestAPIErr != nil {
		return nil, importRestAPIErr
	}

	deploymentDescription := instance.fireProxDeploymentDescription
	deploymentStageDescription := instance.fireProxDeploymentStageDescription
	deploymentStageName := instance.fireProxDeploymentStageName

	_, createDeploymentErr := client.CreateDeployment(ctx, &apigateway.CreateDeploymentInput{
		Description:      &deploymentDescription,
		StageDescription: &deploymentStageDescription,
		StageName:        &deploymentStageName,
		RestApiId:        importRestAPIResp.Id,
	}, apigateway.WithAPIOptions())

	if createDeploymentErr != nil {
		return nil, createDeploymentErr
	}

	apiGateway := APIGateway{
		launcher:          instance,
		Title:             title,
		Deleted:           false,
		RestAPIID:         *importRestAPIResp.Id,
		awsConfig:         awsConfig,
		AllRegisteredURLs: urls,
		NbReqSent:         0,
		Updating:          false,
	}

	return &apiGateway, nil
}

// It returns a list in which for each region, it stores a FireProx instance that already targets the URL or if there are no ones, an instance which still can receive new URLs
func (instance *FireProx) GetOneAPIGatewayInstancesEachRegionCanTargetURL(url1 *url.URL) []*APIGateway {
	apiGatewayInstances := []*APIGateway{}

	for region := range instance.apiGatewaysPerRegion {
		apiGateway := instance.GetAPIGatewayInstanceCanTargetURLInRegion(url1, region)

		if apiGateway != nil {
			apiGatewayInstances = append(apiGatewayInstances, apiGateway)
		}
	}

	return apiGatewayInstances
}

// func (instance *APIGateway) WaitTillAvailable(maxSeconds int) bool {
// 	count := 0

// 	for !instance.Available && count < (maxSeconds/0.05) {
// 		count += 1

// 		time.Sleep(50 * time.Millisecond)
// 	}

// 	return instance.Available
// }

// It returns a FireProx instance that already targets the URL or if there are no ones, an instance which still can receive new URLs
func (instance *FireProx) GetAPIGatewayInstanceCanTargetURLInRegion(url1 *url.URL, targetRegion string) *APIGateway {
	allInstancesInRegion := instance.apiGatewaysPerRegion[targetRegion]

	slices.Reverse(allInstancesInRegion) // to avoid race conditions while renewing

	var availableInstance *APIGateway

	for _, apiGateway := range allInstancesInRegion {
		if apiGateway.Deleted || apiGateway.Deleting {
			continue
		}

		if apiGateway.DoesTargetURL(url1) {
			return apiGateway
		} else if apiGateway.CanStillIncrease() {
			availableInstance = apiGateway
		}
	}

	return availableInstance
}

// Sends a request through one of the FireProx instances
func (instance *FireProx) SendRequest(ctx context.Context, requestData utils.HTTPRequestData, allConfigs *utils.AllConfigs) (utils.HTTPResponseData, string, error) {
	// Retrieves one available instance for this job
	apiGatewayInstance, apiGatewayInstanceErr := instance.getAPIGatewayInstance(ctx, requestData)

	if apiGatewayInstanceErr != nil {
		return utils.HTTPResponseData{}, "", apiGatewayInstanceErr
	}

	apiGatewayURL, apiGatewayURLErr := apiGatewayInstance.GetURLForReachingGivenURL(requestData.URL)

	if apiGatewayURLErr != nil {
		return utils.HTTPResponseData{}, fmt.Sprintf("apiGatewayID=%s, region=%s", apiGatewayInstance.RestAPIID, apiGatewayInstance.awsConfig.Region), apiGatewayURLErr
	}

	headers := map[string]any{}

	if requestData.Headers != nil {
		headers = requestData.Headers
	}

	// Creates a random IP from the given CIDR
	randomIPFromRange, randomIPFromRangeErr := utils.RandomIPFromCIDR(allConfigs.ProvidersConfig.AWSAGForwardedForRange)

	if randomIPFromRangeErr != nil {
		return utils.HTTPResponseData{}, fmt.Sprintf("apiGatewayID=%s, region=%s", apiGatewayInstance.RestAPIID, apiGatewayInstance.awsConfig.Region), randomIPFromRangeErr
	}

	// Sets the random IP as the proxy source
	headers["X-My-X-Forwarded-For"] = randomIPFromRange

	body := &bytes.Buffer{}

	if requestData.Body != nil {
		body = requestData.Body
	}

	resp, respErr := utils.SendRequest(utils.HTTPRequestData{
		URL:     apiGatewayURL,
		Method:  requestData.Method,
		Body:    body,
		Headers: headers,
	})

	if respErr != nil {
		return utils.HTTPResponseData{}, fmt.Sprintf("apiGatewayID=%s, xForwardedFor=%s, region=%s", apiGatewayInstance.RestAPIID, randomIPFromRange.String(), apiGatewayInstance.awsConfig.Region), respErr
	}

	// Increments the number of request sent with this API Gateway instance
	apiGatewayInstance.NbReqSent++

	return resp, fmt.Sprintf("apiGatewayID=%s, xForwardedFor=%s, region=%s", apiGatewayInstance.RestAPIID, randomIPFromRange.String(), apiGatewayInstance.awsConfig.Region), nil
}

// Deletes the FireProx instance (all remaining API Gateways)
func (instance *FireProx) Clear() bool {
	utils.Logger.Info().Str("launcher", instance.GetName()).Msg("Clearing launcher.")

	result := true

	for _, apiGatewayInstances := range instance.apiGatewaysPerRegion {
		for _, apiGatewayInstance := range apiGatewayInstances {
			if apiGatewayInstance.Deleted {
				continue
			}

			utils.Logger.Debug().Str("apiGatewayInstanceID", apiGatewayInstance.RestAPIID).Msg("Deleting API Gateway instance.")

			if err := apiGatewayInstance.Delete(); err != nil {
				utils.Logger.Error().Err(err).Str("apiGatewayInstanceID", apiGatewayInstance.RestAPIID).Msg("Error while deleting API Gateway instance.")

				result = false
			}
		}
	}

	instance.stopped = result

	return result
}

//nolint:revive
func (instance *FireProx) IsAvailable() bool {
	return true
}

func (instance *FireProx) IsStopped() bool {
	return instance.stopped
}

// Tries to return a FireProx instance which can target the URL in the requestData
// - If new instances can be created
//
//   - Chooses a random region
//
//   - It retrieves an available instance in the region (which already targets the url or which can accept new urls)
//
//   - If no instance is available, it checks if a new one is already being created
//
//   - If there is still no instance available
//
//   - It creates a new one
//
//   - Else (if an instance was available)
//
//   - If it does not target the URL, it adds it
//
// - Else
//
//   - It takes a random available instance in all regions
//
//   - If there are no available instance => raise error
//
//   - Else => adds url if necessary
//
// It also handles instance renewing
func (instance *FireProx) getAPIGatewayInstance(ctx context.Context, requestData utils.HTTPRequestData) (*APIGateway, error) {
	var apiGatewayInstance *APIGateway

	// If new instances can be created
	if instance.getNbAPIGatewayInstances() < instance.maxAPIGatewayInstances {
		// Chooses a random region among all available awssdk.Config
		chosenRegion := utils.RandomKeyOfMap(instance.provider.awsConfigs)

		// It retrieves an instance in the region which already targets the URL or which is still not full to add URLs
		apiGateway := instance.GetAPIGatewayInstanceCanTargetURLInRegion(requestData.URL, chosenRegion)

		// If no instance was found, it checks if an instance is already being created
		if apiGateway == nil && slices.Contains(instance.creatingAPIGatewayInstanceInRegion, chosenRegion) {
			// If a FireProx instance is already being created in the region, it waits for it
			for slices.Contains(instance.creatingAPIGatewayInstanceInRegion, chosenRegion) {
				time.Sleep(50 * time.Millisecond) //nolint:gomnd
			}

			apiGateway = instance.GetAPIGatewayInstanceCanTargetURLInRegion(requestData.URL, chosenRegion)
		}

		// If no instance is available, it creates a new one
		if apiGateway == nil {
			// It temporarily stores the region into
			instance.creatingAPIGatewayInstanceInRegion = append(instance.creatingAPIGatewayInstanceInRegion, chosenRegion)

			title := fmt.Sprintf("%s_%s", instance.fireProxTitlePrefix, strconv.FormatInt(time.Now().UnixMicro(), 10)) //nolint:revive

			// It creates a new API Gateway instance
			newAPIGatewayInstance, newAPIGatewayInstanceErr := instance.CreateAPIGateway(ctx, instance.provider.awsConfigs[chosenRegion], []*url.URL{requestData.URL}, title)

			// If it can not create the instance, it rejects the request
			// In addition, it removes the creation from creatingAPIGatewayInstanceInRegion
			if newAPIGatewayInstanceErr != nil {
				instance.creatingAPIGatewayInstanceInRegion = utils.DeleteElementFromSlice(instance.creatingAPIGatewayInstanceInRegion, chosenRegion)

				return nil, newAPIGatewayInstanceErr
			}

			apiGateway = newAPIGatewayInstance

			utils.Logger.Debug().Str("url", requestData.URL.String()).Str("region", chosenRegion).Str("createdInstanceID", newAPIGatewayInstance.RestAPIID).Msg("Creating API Gateway instance.")

			// It stores the API Gateway instance
			instance.apiGatewaysPerRegion[chosenRegion] = append(utils.GetOrDefault(instance.apiGatewaysPerRegion, chosenRegion, []*APIGateway{}), apiGateway)

			// It removes it from the creation list
			instance.creatingAPIGatewayInstanceInRegion = utils.DeleteElementFromSlice(instance.creatingAPIGatewayInstanceInRegion, chosenRegion)
		} else if !apiGateway.DoesTargetURL(requestData.URL) { // Otherwise, if an instance is available but does not already targets the URL, it adds it
			apiGatewayErr := apiGateway.AddNewURL(ctx, requestData.URL)

			if apiGatewayErr != nil {
				return nil, apiGatewayErr
			}
		}

		apiGatewayInstance = apiGateway
	} else { // Otherwise, it retrieves the instances that already target the URL or the instances which can receive new URLs in the other regions
		apiGatewayInstancesAvailable := instance.GetOneAPIGatewayInstancesEachRegionCanTargetURL(requestData.URL)

		if len(apiGatewayInstancesAvailable) == 0 {
			return nil, errors.New("the maximum number of FireProx instances has been reached and no previous instance can target this URL because they have reached the maximum number of resources per instance")
		}

		apiGatewayInstance = utils.RandomElementInSlice(apiGatewayInstancesAvailable)

		if !apiGatewayInstance.DoesTargetURL(requestData.URL) {
			apiGatewayErr := apiGatewayInstance.AddNewURL(ctx, requestData.URL)

			if apiGatewayErr != nil {
				return nil, apiGatewayErr
			}
		}
	}

	// If the instance needs to be rotated
	if instance.rotateAPIGateway > 0 && apiGatewayInstance.NbReqSent > 0 && apiGatewayInstance.NbReqSent%instance.rotateAPIGateway == 0 {
		region := apiGatewayInstance.awsConfig.Region

		// Considers it deleted to avoid race conditions (multiple renews before it gets deleted)
		apiGatewayInstance.Deleting = true
		instance.creatingAPIGatewayInstanceInRegion = append(instance.creatingAPIGatewayInstanceInRegion, region) //nolint:wsl

		// It renews it
		newAPIGatewayInstance, newAPIGatewayInstanceErr := apiGatewayInstance.Renew()

		if newAPIGatewayInstanceErr != nil {
			return nil, newAPIGatewayInstanceErr
		}

		utils.Logger.Debug().Str("region", region).Str("previousInstanceID", apiGatewayInstance.RestAPIID).Str("newInstanceID", newAPIGatewayInstance.RestAPIID).Str("url", requestData.URL.String()).Msg("Renewing API Gateway instance.")

		instance.apiGatewaysPerRegion[region] = append(instance.apiGatewaysPerRegion[region], newAPIGatewayInstance)

		instance.creatingAPIGatewayInstanceInRegion = utils.DeleteElementFromSlice(instance.creatingAPIGatewayInstanceInRegion, region)

		// It deletes the previous instance in a go routine
		instanceToDelete := apiGatewayInstance

		go func() {
			deleteErr := instanceToDelete.Delete()

			if deleteErr != nil {
				utils.Logger.Error().Err(deleteErr).Str("instanceID", instanceToDelete.RestAPIID).Msg("Can not delete API Gateway instance.")
			}
		}()

		apiGatewayInstance = newAPIGatewayInstance
	}

	return apiGatewayInstance, nil
}

func (instance APIGateway) generateOpenAPISpecification() []byte {
	specification := `
	{
		"swagger": "2.0",
		"info": {
		  "version": "{{version_date}}",
		  "title": "{{title}}"
		},
		"basePath": "/",
		"schemes": [
		  "https"
		],
		"paths": {
		  "/": {
			"get": {
			  "parameters": [
				{
				  "name": "proxy",
				  "in": "path",
				  "required": true,
				  "type": "string"
				},
				{
				  "name": "X-My-X-Forwarded-For",
				  "in": "header",
				  "required": false,
				  "type": "string"
				}
			  ],
			  "responses": {},
			  "x-amazon-apigateway-integration": {
				"uri": "https://amazon.com/",
				"responses": {
				  "default": {
					"statusCode": "200"
				  }
				},
				"requestParameters": {
				  "integration.request.path.proxy": "method.request.path.proxy",
				  "integration.request.header.X-Forwarded-For": "method.request.header.X-My-X-Forwarded-For"
				},
				"passthroughBehavior": "when_no_match",
				"httpMethod": "ANY",
				"tlsConfig" : {
				  "insecureSkipVerification" : true
				},
				"cacheNamespace": "{{cacheNamespace}}",
				"cacheKeyParameters": [
				  "method.request.path.proxy"
				],
				"type": "http_proxy"
			  }
			}
		  }
		  {{baseURLsPart}}
		}
	  }
	`

	baseURLsPart := ""

	for _, urlTo := range instance.AllRegisteredURLs {
		toAdd := `,
		"/{{identifier}}/": {
		  "x-amazon-apigateway-any-method": {
			"produces": [
			  "application/json"
			],
			"parameters": [
			  {
				"name": "proxy",
				"in": "path",
				"required": true,
				"type": "string"
			  },
			  {
				"name": "X-My-X-Forwarded-For",
				"in": "header",
				"required": false,
				"type": "string"
			  }
			],
			"responses": {},
			"x-amazon-apigateway-integration": {
			  "uri": "{{baseURL}}/{proxy}",
			  "responses": {
				"default": {
				  "statusCode": "200"
				}
			  },
			  "requestParameters": {
		  "integration.request.path.proxy": "method.request.path.proxy",
				"integration.request.header.X-Forwarded-For": "method.request.header.X-My-X-Forwarded-For"
			  },
			  "passthroughBehavior": "when_no_match",
			  "httpMethod": "ANY",
			  "tlsConfig" : {
				"insecureSkipVerification" : true
			  },
			  "cacheNamespace": "{{cacheNamespace}}",
			  "cacheKeyParameters": [
				"method.request.path.proxy"
			  ],
			  "type": "http_proxy"
			}
		  }
		},
		"/{{identifier}}/{proxy+}": {
		  "x-amazon-apigateway-any-method": {
			"produces": [
			  "application/json"
			],
			"parameters": [
			  {
				"name": "proxy",
				"in": "path",
				"required": true,
				"type": "string"
			  },
			  {
				"name": "X-My-X-Forwarded-For",
				"in": "header",
				"required": false,
				"type": "string"
			  }
			],
			"responses": {},
			"x-amazon-apigateway-integration": {
			  "uri": "{{baseURL}}/{proxy}",
			  "responses": {
				"default": {
				  "statusCode": "200"
				}
			  },
			  "requestParameters": {
		  "integration.request.path.proxy": "method.request.path.proxy",
				"integration.request.header.X-Forwarded-For": "method.request.header.X-My-X-Forwarded-For"
			  },
			  "passthroughBehavior": "when_no_match",
			  "httpMethod": "ANY",
			  "tlsConfig" : {
				"insecureSkipVerification" : true
			  },
			  "cacheNamespace": "irx7tm",
			  "cacheKeyParameters": [
				"method.request.path.proxy"
			  ],
			  "type": "http_proxy"
			}
		  }
		}`

		baseURL := utils.GetBaseURL(urlTo)

		identifier, identifierErr := getAPIGatewayURLIdentifierForReachingGivenURL(urlTo)

		if identifierErr != nil {
			utils.Logger.Error().Err(identifierErr).Str("urlTo", urlTo.String()).Msg("Can not parse base URL for adding it to the API Gateway config.")

			continue
		}

		toAdd = strings.ReplaceAll(toAdd, "{{identifier}}", identifier)
		toAdd = strings.ReplaceAll(toAdd, "{{baseURL}}", baseURL)

		baseURLsPart += toAdd
	}

	cacheNamespace := utils.GenerateRandomPrefix(7) //nolint:revive,gomnd

	currentTime := time.Now()

	versionDate := currentTime.Format("2006-01-02T15:04:05Z")

	specification = strings.ReplaceAll(specification, "{{title}}", strings.ReplaceAll(instance.Title, " ", "_"))
	specification = strings.ReplaceAll(specification, "{{version_date}}", versionDate)
	specification = strings.ReplaceAll(specification, "{{baseURLsPart}}", baseURLsPart)
	specification = strings.ReplaceAll(specification, "{{cacheNamespace}}", cacheNamespace)

	return []byte(specification)
}

// Deletes the given instance
func (instance *APIGateway) Delete() error {
	client := apigateway.NewFromConfig(*instance.awsConfig)

	_, deleteErr := client.DeleteRestApi(context.Background(), &apigateway.DeleteRestApiInput{
		RestApiId: &instance.RestAPIID,
	}, apigateway.WithAPIOptions())

	if deleteErr != nil {
		return deleteErr
	}

	instance.Deleted = true

	return nil
}

// Returns the API Gateway REST API endpoint URL
func (instance APIGateway) GetAPIGatewayStageURL() (*url.URL, error) {
	return url.Parse(fmt.Sprintf("https://%s.execute-api.%s.amazonaws.com/%s/", instance.RestAPIID, instance.awsConfig.Region, instance.launcher.fireProxDeploymentStageName))
}

// Creates a new FireProx instance with the same characteristics as the given one
func (instance *APIGateway) Renew() (*APIGateway, error) {
	return instance.launcher.CreateAPIGateway(context.Background(), instance.awsConfig, instance.AllRegisteredURLs, instance.Title)
}

// Checks if the given baseURL is already handled by this instance
func (instance *APIGateway) DoesTargetURL(url1 *url.URL) bool {
	baseURL := utils.GetBaseURL(url1)

	for _, urlRegistered := range instance.AllRegisteredURLs {
		if utils.GetBaseURL(urlRegistered) == baseURL {
			return true
		}
	}

	return false
}

// Returns the API Gateway endpoint (with the right url path) to reach the given URL through AWS API Gateways
func (instance APIGateway) GetURLForReachingGivenURL(url1 *url.URL) (*url.URL, error) {
	fireProxURLId, fireProxURLIdErr := getAPIGatewayURLIdentifierForReachingGivenURL(url1)

	if fireProxURLIdErr != nil {
		return nil, fireProxURLIdErr
	}

	path := utils.GetPathFromURL(url1)

	fireProxStageURL, fireProxStageURLErr := instance.GetAPIGatewayStageURL()

	if fireProxStageURLErr != nil {
		return nil, fireProxStageURLErr
	}

	return url.Parse(fmt.Sprintf("%s%s%s", fireProxStageURL, fireProxURLId, path))
}

// Adds the new baseURL to this instance and redeploy the REST API
func (instance *APIGateway) AddNewURL(ctx context.Context, url1 *url.URL) error {
	if !instance.CanStillIncrease() {
		return errors.New("the maximum of resource per API Gateway instance has been reached for this instance")
	}

	// It checks if the url has not already been added (to avoid race conditions)
	for _, registeredURL := range instance.AllRegisteredURLs {
		if utils.CompareBaseURLs(registeredURL, url1) {
			return nil
		}
	}

	for instance.Updating {
		time.Sleep(200 * time.Millisecond) //nolint:gomnd
	}

	instance.Updating = true

	client := apigateway.NewFromConfig(*instance.awsConfig)

	// Adds the base URL
	instance.AllRegisteredURLs = append(instance.AllRegisteredURLs, url1)

	// Refreshes the OpenAPI specification
	_, restAPIErr := client.PutRestApi(ctx, &apigateway.PutRestApiInput{
		Body: instance.generateOpenAPISpecification(),
		Parameters: map[string]string{
			"endpointConfigurationTypes": "REGIONAL",
		},
		RestApiId: &instance.RestAPIID,
		Mode:      types.PutModeOverwrite,
	}, apigateway.WithAPIOptions())

	if restAPIErr != nil {
		instance.AllRegisteredURLs = utils.DeleteElementFromSlice(instance.AllRegisteredURLs, url1)

		instance.Updating = false

		return restAPIErr
	}

	deploymentDescription := instance.launcher.fireProxDeploymentDescription
	deploymentStageDescription := instance.launcher.fireProxDeploymentStageDescription
	deploymentStageName := instance.launcher.fireProxDeploymentStageName

	// Redeploys the API Gateway
	_, createDeploymentErr := client.CreateDeployment(ctx, &apigateway.CreateDeploymentInput{
		Description:      &deploymentDescription,
		StageDescription: &deploymentStageDescription,
		StageName:        &deploymentStageName,
		RestApiId:        &instance.RestAPIID,
	}, apigateway.WithAPIOptions())

	if createDeploymentErr != nil {
		instance.AllRegisteredURLs = utils.DeleteElementFromSlice(instance.AllRegisteredURLs, url1)

		instance.Updating = false

		return createDeploymentErr
	}

	reachURL, reachURLErr := instance.GetURLForReachingGivenURL(url1) // URL for reaching the added baseURL through the FireProx instance

	if reachURLErr != nil {
		instance.Updating = false

		return reachURLErr
	}

	// Checks if the deployment has been done and propagated (checks if the added URL is reachable through the gateway)
	// It sends requests (for a maximum duration of maxDuration seconds) to the corresponding API Gateway URL and checks if it redirects to the targeted server
	startTime := time.Now()
	maxDuration := 15 * time.Second //nolint:revive,gomnd

	for time.Since(startTime) < maxDuration {
		time.Sleep(250 * time.Millisecond) //nolint:gomnd

		response, responseErr := http.Get(reachURL.String())

		if responseErr != nil {
			continue
		}

		body, bodyErr := io.ReadAll(response.Body)

		closeErr := response.Body.Close()

		if closeErr != nil {
			continue
		}

		if bodyErr != nil {
			continue
		}

		if !strings.Contains(string(body), `{"message":"Missing Authentication Token"}`) { // If it is propagated, it stops the loop
			break
		}
	}

	// Additional waiting time for avoiding "Missing Authentication Token" (403) responses (not fully propagated instances)
	time.Sleep(3000 * time.Millisecond) //nolint:gomnd

	instance.Updating = false

	return nil
}

// Checks if new URLs can be added
func (instance *APIGateway) CanStillIncrease() bool {
	return len(instance.AllRegisteredURLs) < utils.MaxResourcePerFireProxInstance
}

// Corresponds to the identifier for the API Gateway URL to redirect to the right target
// Ex: https_google.fr:443
func getAPIGatewayURLIdentifierForReachingGivenURL(urlTo *url.URL) (string, error) {
	baseURL := utils.GetBaseURL(urlTo)

	baseURLParsed, baseURLParsedErr := url.Parse(baseURL) // The URL is parsed again in order to add the port (if not added by default)

	if baseURLParsedErr != nil {
		return "", baseURLParsedErr
	}

	return fmt.Sprintf("%s_%s", baseURLParsed.Scheme, baseURLParsed.Host), baseURLParsedErr
}
