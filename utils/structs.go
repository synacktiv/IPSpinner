package utils

import (
	"bytes"
	"net/url"
)

type HTTPRequestData struct {
	URL                *url.URL
	Method             string
	Headers            map[string]any
	Body               *bytes.Buffer
	FollowRedirections bool
}

type HTTPResponseData struct {
	StatusCode int
	Headers    map[string]any
	Body       []byte
}

type HTTPRequestJSONData struct {
	URL                *url.URL
	Method             string
	Headers            map[string]any
	Body               map[string]any
	FollowRedirections bool
}

type HTTPResponseJSONData struct {
	StatusCode int
	Headers    map[string]any
	Body       map[string]any
}

type ProvidersConfig struct {
	AWSRegions                              []string
	AWSProfile                              string
	AWSAccessKey                            string
	AWSSecretKey                            string
	AWSSessionToken                         string
	AWSAGEnabled                            bool
	AWSAGMaxInstances                       int
	AWSAGRotateNbRequests                   int
	AWSAGForwardedForRange                  string
	AWSAGInstanceTitlePrefix                string
	AWSAGInstanceDeploymentDescription      string
	AWSAGInstanceDeploymentStageDescription string
	AWSAGInstanceDeploymentStageName        string
	GitHubGAEnabled                         bool
	GitHubUsername                          string
	GitHubToken                             string
	AzureAdminEmail                         string
	AzureAdminPassword                      string
	AzureTenantID                           string
	AzureSubscriptionID                     string
	AzureAccountsFile                       string
	AzureCSEnabled                          bool
	AzureCSPreferredLocations               []string
	AzureCSNbInstances                      int
}

type ProxyConfig struct {
	PreloadHostsFile                string
	WhitelistHostsFile              string
	BlacklistHostsFile              string
	CaCertFile                      string
	CaCertKeyFile                   string
	DebugResponseHeaders            bool
	UserAgentsFile                  string
	WaitForLauncherAvailableTimeout int
}

type CommandParameters struct {
	Host          string
	Port          int
	Verbose1      bool
	Verbose2      bool
	Verbose3      bool
	ExportCaCert  bool
	ConfigINIPath string
}

type AllConfigs struct {
	ProvidersConfig   ProvidersConfig
	ProxyConfig       ProxyConfig
	CommandParameters CommandParameters
}
