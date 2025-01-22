package providers

import (
	"context"
	"ipspinner/providers/aws"
	"ipspinner/providers/azure"
	"ipspinner/providers/github"
	"ipspinner/utils"
	"net/url"
	"strings"
)

// Initializes and returns all available providers
func LoadProviders(ctx context.Context, allConfigs *utils.AllConfigs) []utils.Provider {
	utils.Logger.Info().Msg("Loading providers, please do not press ctrl+c.")

	providers := []utils.Provider{}

	// For AWS FireProx
	if allConfigs.ProvidersConfig.AWSAGEnabled {
		provider, providerErr := aws.Initialize(ctx, allConfigs)

		if providerErr != nil {
			utils.Logger.Error().Err(providerErr).Str("provider", provider.GetName()).Msg("Can not load the provider.")

			provider.Clear()
		} else {
			providers = append(providers, provider)
		}
	}

	// For Github Actions
	if allConfigs.ProvidersConfig.GitHubGAEnabled {
		if strings.EqualFold(allConfigs.ProvidersConfig.GitHubUsername, "synacktiv") {
			utils.Logger.Error().Msg("Are you crazy ? Do not run IPSpinner on the Github Synacktiv account, we may be banned...")
		} else {
			provider, providerErr := github.Initialize(ctx, allConfigs)

			if providerErr != nil {
				utils.Logger.Error().Err(providerErr).Str("provider", provider.GetName()).Msg("Can not load the provider.")

				provider.Clear()
			} else {
				providers = append(providers, &provider)
			}
		}
	}

	// For Azure Cloudshell
	if allConfigs.ProvidersConfig.AzureCSEnabled {
		provider, providerErr := azure.Initialize(ctx, allConfigs)

		if providerErr != nil {
			utils.Logger.Error().Err(providerErr).Str("provider", provider.GetName()).Msg("Can not load the provider.")

			provider.Clear()
		} else {
			providers = append(providers, provider)
		}
	}

	preloadHosts := utils.ParseHostsFile(allConfigs.ProxyConfig.PreloadHostsFile)
	whitelistHosts := utils.ParseHostsFile(allConfigs.ProxyConfig.WhitelistHostsFile)
	blacklistHosts := utils.ParseHostsFile(allConfigs.ProxyConfig.BlacklistHostsFile)

	newPreloadHosts := []*url.URL{}

	// If a whitelist or a blacklist is provided, it checks if URLs in preload hosts list are suitable
	// The whitelist is prioritary on the blacklist
	if len(whitelistHosts) > 0 || len(blacklistHosts) > 0 {
		for _, preloadHost := range preloadHosts {
			if len(whitelistHosts) > 0 { // Whitelist is used
				if utils.DoesURLListContainsBaseURL(whitelistHosts, preloadHost) {
					newPreloadHosts = append(newPreloadHosts, preloadHost)
				} else {
					utils.Logger.Warn().Str("host", preloadHost.String()).Msg("The host has been removed from the preloading hosts because it is not mentioned in the whitelist.")
				}
			} else if len(blacklistHosts) > 0 { // if the whitelist is not used and the blacklist is used
				if utils.DoesURLListContainsBaseURL(blacklistHosts, preloadHost) {
					utils.Logger.Warn().Str("host", preloadHost.String()).Msg("The host has been removed from the preloading hosts because it is mentioned in the blacklist.")
				} else {
					newPreloadHosts = append(newPreloadHosts, preloadHost)
				}
			}
		}
	} else {
		newPreloadHosts = preloadHosts
	}

	if len(newPreloadHosts) > 0 {
		for _, provider := range providers {
			for _, launcher := range provider.GetLaunchers() {
				launcher.PreloadHosts(ctx, newPreloadHosts)
			}
		}
	}

	return providers
}

// Clears all configured providers
func ClearProviders(providers []utils.Provider) {
	result := true

	for _, provider := range providers {
		result = result && provider.Clear()
	}

	if result {
		utils.Logger.Info().Msg("All providers have been cleared.")
	} else {
		utils.Logger.Error().Msg("Some providers have not been cleared.")
	}
}

func GetAllLaunchers(providers []utils.Provider) []*utils.Launcher {
	launchers := make([]*utils.Launcher, 0)

	for _, provider := range providers {
		for _, launcher := range provider.GetLaunchers() {
			launcherCopy := launcher

			launchers = append(launchers, &launcherCopy)
		}
	}

	return launchers
}

func GetAllAvailableLaunchers(providers []utils.Provider) []*utils.Launcher {
	launchers := make([]*utils.Launcher, 0)

	for _, provider := range providers {
		for _, launcher := range provider.GetAvailableLaunchers() {
			launcherCopy := launcher

			launchers = append(launchers, &launcherCopy)
		}
	}

	return launchers
}
