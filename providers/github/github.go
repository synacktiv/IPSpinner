package github

import (
	"context"
	"errors"
	"fmt"
	"ipspinner/utils"
)

type Infos struct {
	Accept     string
	APIVersion string
	Token      string
	Username   string
}

// Provider object
type GitHub struct {
	repositories []*Repository
	infos        Infos
	stopped      bool
}

//nolint:revive
func (instance GitHub) GetName() string {
	return "GitHub"
}

func (instance GitHub) GetInfos() Infos {
	return instance.infos
}

func (instance GitHub) SummarizeState() string {
	if !instance.IsStopped() {
		return fmt.Sprintf("Provider %s is running with %d launcher(s).", instance.GetName(), len(instance.GetLaunchers()))
	}

	return fmt.Sprintf("Provider %s is stopped.", instance.GetName())
}

func (instance *GitHub) GetAvailableLaunchers() []utils.Launcher {
	launchers := make([]utils.Launcher, 0)

	for _, launcher := range instance.GetLaunchers() {
		if launcher.IsAvailable() {
			launchers = append(launchers, launcher)
		}
	}

	return launchers
}

func (instance *GitHub) GetLaunchers() []utils.Launcher {
	launchers := make([]utils.Launcher, 0)

	for _, repository := range instance.repositories {
		launchers = append(launchers, repository)
	}

	return launchers
}

func (instance GitHub) GetNbTotalReqSent() int {
	count := 0

	for _, launcher := range instance.GetLaunchers() {
		count += launcher.GetNbTotalReqSent()
	}

	return count
}

func (instance GitHub) IsStopped() bool {
	return instance.stopped
}

func (instance *GitHub) Clear() bool {
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

// Creates and initializes a new instance of the GitHub provider object
// revive:disable:unused-parameter
func Initialize(ctx context.Context, allConfigs *utils.AllConfigs) (GitHub, error) {
	instance := GitHub{stopped: false}

	utils.Logger.Info().Str("provider", instance.GetName()).Msg("Configuring provider.")

	instance.infos = Infos{
		Accept:     "application/vnd.github+json",
		APIVersion: "2022-11-28",
		Token:      allConfigs.ProvidersConfig.GitHubToken,
		Username:   allConfigs.ProvidersConfig.GitHubUsername,
	}

	if allConfigs.ProvidersConfig.GitHubGAEnabled {
		instance.loadRepositoryLaunchers()
	}

	if len(instance.GetLaunchers()) == 0 {
		return instance, errors.New("no launchers could have been created")
	}

	return instance, nil
}

// revive:enable:unused-parameter

func (instance *GitHub) loadRepositoryLaunchers() {
	launcher, launcherErr := CreateRepositoryLauncher(instance)

	if launcherErr != nil {
		utils.Logger.Error().Err(launcherErr).Msg("Cannot create repository workers launcher.")
	} else {
		instance.repositories = append(instance.repositories, launcher)
	}
}
