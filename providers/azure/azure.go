package azure

import (
	"context"
	"errors"
	"fmt"
	"ipspinner/utils"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// Provider object
type Azure struct {
	precreatedAccounts []*Account
	cloudShells        []*CloudShell
	adminAccount       *Account
	stopped            bool
}

//nolint:revive
func (instance Azure) GetName() string {
	return "Azure"
}

func (instance Azure) IsStopped() bool {
	return instance.stopped
}

func (instance Azure) SummarizeState() string {
	if !instance.IsStopped() {
		return fmt.Sprintf("Provider %s is running with %d launcher(s).", instance.GetName(), len(instance.GetLaunchers()))
	}

	return fmt.Sprintf("Provider %s is stopped.", instance.GetName())
}

// Creates and initializes a new instance of the Azure provider object
func Initialize(ctx context.Context, allConfigs *utils.AllConfigs) (*Azure, error) { //nolint:revive
	instance := Azure{stopped: false}

	utils.Logger.Info().Str("provider", instance.GetName()).Msg("Configuring provider.")

	if allConfigs.ProvidersConfig.AzureAccountsFile == "" {
		adminAccount, adminAccountErr := ConnectAccount(allConfigs.ProvidersConfig.AzureAdminEmail, allConfigs.ProvidersConfig.AzureAdminPassword, allConfigs.ProvidersConfig.AzureTenantID, false, "", "")

		if adminAccountErr != nil {
			return &instance, adminAccountErr
		}

		instance.adminAccount = &adminAccount
	} else {
		loadAccountsErr := instance.loadAzurePrecreatedAccounts(allConfigs)

		if loadAccountsErr != nil {
			return &instance, loadAccountsErr
		}
	}

	if allConfigs.ProvidersConfig.AzureCSEnabled {
		instance.loadCloudShellLaunchers(allConfigs.ProvidersConfig.AzureCSNbInstances, allConfigs.ProvidersConfig.AzureSubscriptionID, allConfigs.ProvidersConfig.AzureCSPreferredLocations)
	}

	if len(instance.GetLaunchers()) == 0 {
		return &instance, errors.New("no launchers could have been created")
	}

	return &instance, nil
}

func (instance *Azure) loadAzurePrecreatedAccounts(allConfigs *utils.AllConfigs) error {
	utils.Logger.Debug().Str("provider", instance.GetName()).Msg("Loading precreated accounts.")

	tenantID := allConfigs.ProvidersConfig.AzureTenantID

	accountsFileLines, accountsFileLinesErr := utils.ReadFileLines(allConfigs.ProvidersConfig.AzureAccountsFile)

	if accountsFileLinesErr != nil {
		return accountsFileLinesErr
	}

	if len(accountsFileLines)%2 != 0 {
		return fmt.Errorf("the Azure accounts file does not respect the expected format: 2 lines per account (email, password)")
	}

	for i := 0; i < len(accountsFileLines)/2; i++ {
		email := accountsFileLines[i*2]
		password := accountsFileLines[i*2+1]

		account, accountErr := ConnectAccount(email, password, tenantID, false, "", "")

		if accountErr != nil {
			utils.Logger.Warn().Str("provider", instance.GetName()).Str("email", email).Msg("Cannot connect to this user.")

			continue
		}

		instance.precreatedAccounts = append(instance.precreatedAccounts, &account)
	}

	if len(instance.precreatedAccounts) == 0 {
		return fmt.Errorf("ipspinner was not able to connect to any of the precreated accounts")
	}

	return nil
}

func (instance *Azure) loadCloudShellLaunchers(nbLaunchers int, subscriptionID string, preferredLocations []string) {
	if len(preferredLocations) == 0 {
		preferredLocations = append(preferredLocations, "westeurope")
	}

	for i := 0; i < nbLaunchers; i++ {
		var account *Account

		if len(instance.precreatedAccounts) > 0 {
			if i >= len(instance.precreatedAccounts) {
				utils.Logger.Warn().Msg("No more precreated account available for creating a new CloudShell launcher.")

				continue
			}

			account = instance.precreatedAccounts[i]
		} else {
			accountCreated, accountCreatedErr := CreateCloudShellAccount(instance, subscriptionID)

			if accountCreatedErr != nil {
				utils.Logger.Error().Err(accountCreatedErr).Msg("Cannot create an account for this CloudShell launcher.")

				continue
			}

			account = &accountCreated
		}

		cloudShell, cloudShellErr := CreateCloudShell(instance, subscriptionID, preferredLocations[i%len(preferredLocations)], *account)

		if cloudShellErr != nil {
			utils.Logger.Error().Err(cloudShellErr).Msg("Cannot create cloud shell launcher.")

			// Waits for the account to be propagated
			time.Sleep(2 * time.Second)

			cloudShell.Clear()

			continue
		}

		instance.cloudShells = append(instance.cloudShells, cloudShell)
	}
}

func (instance *Azure) GetAvailableLaunchers() []utils.Launcher {
	launchers := make([]utils.Launcher, 0)

	for _, launcher := range instance.GetLaunchers() {
		if launcher.IsAvailable() {
			launchers = append(launchers, launcher)
		}
	}

	return launchers
}

func (instance *Azure) GetLaunchers() []utils.Launcher {
	launchers := make([]utils.Launcher, 0)

	for _, cloudShell := range instance.cloudShells {
		launchers = append(launchers, cloudShell)
	}

	return launchers
}

func (instance *Azure) GetAdminAccount() *Account {
	return instance.adminAccount
}

func (instance *Azure) Clear() bool {
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

func (instance Azure) GetNbTotalReqSent() int {
	count := 0

	for _, launcher := range instance.GetLaunchers() {
		count += launcher.GetNbTotalReqSent()
	}

	return count
}

//nolint:revive //tmp
func (instance Azure) CreateResourceGroup() {
	rgClient, rgClientErr := armresources.NewResourceGroupsClient("f89fac99-3ac9-4650-942f-acd69f471c20", *instance.adminAccount.tokenCredential, nil)

	fmt.Println(rgClientErr)

	location := "francecentral"

	createResp, createErr := rgClient.CreateOrUpdate(context.Background(), "rgtest", armresources.ResourceGroup{
		Location: &location,
	}, &armresources.ResourceGroupsClientCreateOrUpdateOptions{})

	fmt.Printf("%+v \n", createResp)

	fmt.Println(createErr)
}

//nolint:revive //tmp
func (instance Azure) DeleteResourceGroup() {
	rgClient, rgClientErr := armresources.NewResourceGroupsClient("f89fac99-3ac9-4650-942f-acd69f471c20", *instance.adminAccount.tokenCredential, nil)

	fmt.Println(rgClientErr)

	deleteResp, deleteErr := rgClient.BeginDelete(context.Background(), "rgtest", &armresources.ResourceGroupsClientBeginDeleteOptions{})

	fmt.Printf("%+v \n", deleteResp)

	fmt.Println(deleteErr)
}
