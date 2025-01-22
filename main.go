package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"ipspinner/providers"
	"ipspinner/proxy"
	"ipspinner/utils"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/ini.v1"
)

func main() {
	//////////////////////////// //nolint:revive
	// INITIALISATION SECTION //
	//////////////////////////// //nolint:revive
	commandParameters := parseFlags()

	utils.InitializeLogger(commandParameters.Verbose1, commandParameters.Verbose2)

	defer utils.CloseLogFile() //nolint:errcheck

	proxyConfig, providersConfig, parseConfigErr := parseConfig(commandParameters.ConfigINIPath)

	if parseConfigErr != nil {
		utils.Logger.Error().Err(parseConfigErr).Msg("Can not parse the configuration file.")

		return
	}

	allConfigs := utils.AllConfigs{
		ProvidersConfig:   providersConfig,
		ProxyConfig:       proxyConfig,
		CommandParameters: commandParameters,
	}

	////////////////// //nolint:revive
	// MAIN SECTION //
	////////////////// //nolint:revive
	// Impede ctrl+c
	sigtermChan := make(chan os.Signal, 1)

	signal.Notify(sigtermChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigtermChan
	}()

	providersLst := providers.LoadProviders(context.Background(), &allConfigs)

	if len(providers.GetAllLaunchers(providersLst)) == 0 {
		utils.Logger.Error().Msg("No launcher is available.")

		return
	}

	defer providers.ClearProviders(providersLst)

	summarizeStateRunning := launchSummarizeStateTask(&allConfigs, providersLst)

	srv, srvListenAddress, srvErr := launchProxy(context.Background(), &allConfigs, providersLst)

	if srvErr != nil {
		utils.Logger.Error().Err(srvErr).Str("listenAddress", srvListenAddress).Msg("Can not create and launch the proxy.")

		return
	}

	////////////////// //nolint:revive
	// STOP SECTION //
	////////////////// //nolint:revive
	// Waits for Ctrl+c
	sigChan := make(chan os.Signal, 1)

	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	*summarizeStateRunning = false

	utils.Logger.Info().Msg("Stopping proxy ...")

	srvCloseErr := srv.Close()

	if srvCloseErr != nil {
		utils.Logger.Error().Err(srvCloseErr).Msg("An error happened while stopping the proxy.")
	}
}

// Parses the configuration file
//
//nolint:revive
func parseConfig(configFilePath string) (utils.ProxyConfig, utils.ProvidersConfig, error) {
	cfg, err := ini.Load(configFilePath)

	if err != nil {
		return utils.ProxyConfig{}, utils.ProvidersConfig{}, errors.New("can not retrieve or parse the configuration file")
	}

	return utils.ProxyConfig{
			PreloadHostsFile:                cfg.Section("proxy").Key("preload_hosts_file").String(),
			WhitelistHostsFile:              cfg.Section("proxy").Key("whitelist_hosts_file").String(),
			BlacklistHostsFile:              cfg.Section("proxy").Key("blacklist_hosts_file").String(),
			CaCertFile:                      cfg.Section("proxy").Key("ca_cert_file").String(),
			CaCertKeyFile:                   cfg.Section("proxy").Key("ca_cert_key_file").String(),
			UserAgentsFile:                  cfg.Section("proxy").Key("user_agents_file").String(),
			DebugResponseHeaders:            cfg.Section("proxy").Key("debug_response_headers").MustBool(true),
			WaitForLauncherAvailableTimeout: cfg.Section("proxy").Key("wait_for_launcher_available_timeout").MustInt(60), //nolint:gomnd
		}, utils.ProvidersConfig{
			AWSRegions:                              cfg.Section("aws").Key("regions").Strings(","),
			AWSProfile:                              cfg.Section("aws").Key("profile").String(),
			AWSAccessKey:                            cfg.Section("aws").Key("access_key").String(),
			AWSSecretKey:                            cfg.Section("aws").Key("secret_key").String(),
			AWSSessionToken:                         cfg.Section("aws").Key("session_token").String(),
			AWSAGEnabled:                            cfg.Section("aws").Key("ag_enabled").MustBool(false),
			AWSAGMaxInstances:                       cfg.Section("aws").Key("ag_max_instances").MustInt(5),         //nolint:gomnd
			AWSAGRotateNbRequests:                   cfg.Section("aws").Key("ag_rotate_nb_requests").MustInt(5000), //nolint:gomnd
			AWSAGForwardedForRange:                  cfg.Section("aws").Key("ag_forwarded_for_range").MustString("35.180.0.0/16"),
			AWSAGInstanceTitlePrefix:                cfg.Section("aws").Key("ag_instance_title_prefix").MustString(utils.GenerateRandomSentence(1)),
			AWSAGInstanceDeploymentDescription:      cfg.Section("aws").Key("ag_instance_deployment_description").MustString(utils.GenerateRandomSentence(3)),
			AWSAGInstanceDeploymentStageDescription: cfg.Section("aws").Key("ag_instance_deployment_stage_description").MustString(utils.GenerateRandomSentence(3)),
			AWSAGInstanceDeploymentStageName:        cfg.Section("aws").Key("ag_instance_deployment_stage_name").MustString(utils.GenerateRandomPrefix(10)), //nolint:gomnd
			GitHubUsername:                          cfg.Section("github").Key("username").String(),
			GitHubToken:                             cfg.Section("github").Key("token").String(),
			GitHubGAEnabled:                         cfg.Section("github").Key("ga_enabled").MustBool(false),
			AzureAdminEmail:                         cfg.Section("azure").Key("admin_email").String(),
			AzureAdminPassword:                      cfg.Section("azure").Key("admin_password").String(),
			AzureTenantID:                           cfg.Section("azure").Key("tenant_id").String(),
			AzureSubscriptionID:                     cfg.Section("azure").Key("subscription_id").String(),
			AzureAccountsFile:                       cfg.Section("azure").Key("accounts_file").String(),
			AzureCSEnabled:                          cfg.Section("azure").Key("cs_enabled").MustBool(false),
			AzureCSPreferredLocations:               cfg.Section("azure").Key("cs_preferred_locations").Strings(","),
			AzureCSNbInstances:                      cfg.Section("azure").Key("cs_nb_instances").MustInt(5), //nolint:gomnd
		}, nil
}

// Parses the command-line arguments
func parseFlags() utils.CommandParameters {
	var (
		host          string
		port          int
		verbose1      bool
		verbose2      bool
		verbose3      bool
		exportCaCert  bool
		configINIPath string
	)

	flag.StringVar(&host, "host", "127.0.0.1", "Proxy host")
	flag.IntVar(&port, "port", 8080, "Proxy port") //nolint:revive,gomnd
	flag.BoolVar(&verbose1, "v", false, "Verbose mode")
	flag.BoolVar(&verbose2, "vv", false, "Very verbose mode")
	flag.BoolVar(&verbose3, "vvv", false, "Very very verbose mode")
	flag.BoolVar(&exportCaCert, "export-ca-cert", false, "Export CA cert and key")
	flag.StringVar(&configINIPath, "config", "config.ini", "Config INI file path (check zad for file format)")

	flag.Parse()

	return utils.CommandParameters{
		Host:          host,
		Port:          port,
		Verbose1:      verbose1 || verbose2 || verbose3,
		Verbose2:      verbose2 || verbose3,
		Verbose3:      verbose3,
		ExportCaCert:  exportCaCert,
		ConfigINIPath: configINIPath,
	}
}

// Launches the summarize task and returns a bool pointer to stop it
func launchSummarizeStateTask(allConfigs *utils.AllConfigs, providersLst []utils.Provider) *bool {
	var running bool

	if allConfigs.CommandParameters.Verbose1 || allConfigs.CommandParameters.Verbose2 || allConfigs.CommandParameters.Verbose3 {
		running = true

		go func() {
			for running {
				time.Sleep(utils.SummarizeStateInterval * time.Second)

				if running { // If it has been stopped during the time.sleep
					for _, provider := range providersLst {
						utils.Logger.Debug().Str("provider", provider.GetName()).Msg(provider.SummarizeState())

						for _, launcher := range provider.GetLaunchers() {
							utils.Logger.Debug().Str("provider", provider.GetName()).Str("launcher", launcher.GetName()).Msg("  - " + launcher.SummarizeState())
						}
					}
				}
			}
		}()
	}

	return &running
}

// Launches the proxy with the given configuration and providers
func launchProxy(ctx context.Context, allConfigs *utils.AllConfigs, providersLst []utils.Provider) (*http.Server, string, error) {
	prxy, proxyErr := proxy.CreateProxy(ctx, allConfigs, providersLst)

	host := allConfigs.CommandParameters.Host

	port := allConfigs.CommandParameters.Port

	if port == 0 {
		port = 8080 //nolint:revive
	}

	listenAddress := fmt.Sprintf("%s:%d", host, port)

	if proxyErr != nil {
		return nil, listenAddress, proxyErr
	}

	srv := http.Server{Addr: listenAddress, Handler: prxy} //nolint:gosec

	// Starts the Proxy server
	go func() {
		utils.Logger.Info().Str("listenAddress", listenAddress).Msg("Proxy is running.")

		err := srv.ListenAndServe()

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			utils.Logger.Error().Str("listenAddress", listenAddress).Err(err).Msg("An error happened while launching the proxy.")

			providers.ClearProviders(providersLst)

			os.Exit(1) //nolint:revive
		}
	}()

	return &srv, listenAddress, nil
}
