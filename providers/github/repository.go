package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"ipspinner/utils"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Repository struct {
	provider            *GitHub
	sendingRequests     map[string]*Worker
	name                string
	aesKeyHex           string
	nbTotalRequestsSent int
	stopped             bool
}

type Worker struct {
	closed  bool
	channel chan WorkerResponse
}

type WorkerResponse struct {
	ResponseData utils.HTTPResponseData
	Error        error
}

func CreateRepositoryLauncher(provider *GitHub) (*Repository, error) {
	instance := Repository{
		name:                strings.ReplaceAll(utils.GenerateRandomSentence(3), " ", "-"),
		provider:            provider,
		nbTotalRequestsSent: 0,
		sendingRequests:     map[string]*Worker{},
		stopped:             false,
		aesKeyHex:           "",
	}

	utils.Logger.Info().Str("launcher", instance.GetName()).Msg("Creating launcher.")

	aesKey, aesKeyErr := utils.Aes256GenerateKey()

	if aesKeyErr != nil {
		return &instance, aesKeyErr
	}

	instance.aesKeyHex = aesKey

	// Creates the github repository
	createErr, addErr := instance.PrepareSprayerRepository()

	if createErr != nil {
		return &instance, createErr
	}

	// If the repository has been successfully created but the files could not have been added => it deletes the repository
	if addErr != nil {
		deleteErr := DeleteRepository(instance.provider.GetInfos(), instance.GetName())

		if deleteErr != nil {
			utils.Logger.Error().Err(deleteErr).Str("repositoryName", instance.GetName()).Msg("Cannot delete the previously created repository.")
		}

		return &instance, addErr
	}

	// Launches the go routine for constantly reading workflow runs
	go instance.LoopWorkflows()

	return &instance, nil
}

func (instance Repository) GetName() string {
	return instance.name
}

func (instance *Repository) GetProvider() utils.Provider {
	return instance.provider
}

func (instance Repository) GetNbTotalReqSent() int {
	return instance.nbTotalRequestsSent
}

func (instance Repository) SummarizeState() string {
	return fmt.Sprintf("Launcher %s : nbTotalRequestsSent=%d, name=%s", instance.GetName(), instance.GetNbTotalReqSent(), instance.GetName())
}

//nolint:revive
func (instance *Repository) PreloadHosts(ctx context.Context, allPreloadHosts []*url.URL) {
	utils.Logger.Info().Str("provider", instance.GetName()).Msg("This provider cannot preload hosts.")
}

func (instance *Repository) SendRequest(ctx context.Context, reqData utils.HTTPRequestData, allConfigs *utils.AllConfigs) (utils.HTTPResponseData, string, error) { //nolint:revive
	headersStr := ""

	if reqData.Headers != nil {
		for headerKey, headerVal := range reqData.Headers {
			headersStr += headerKey + "\n" + fmt.Sprintf("%v", headerVal) + "\n"
		}

		if len(headersStr) > 0 {
			headersStr = headersStr[:(len(headersStr) - 1)]
		}
	}

	bodyStr := ""

	if reqData.Body != nil {
		bodyStr = reqData.Body.String()
	}

	encryptedMethod, encryptedMethodErr := utils.Aes256Encrypt(reqData.Method, instance.aesKeyHex)
	if encryptedMethodErr != nil {
		return utils.HTTPResponseData{}, "", encryptedMethodErr
	}

	encryptedURL, encryptedURLErr := utils.Aes256Encrypt(reqData.URL.String(), instance.aesKeyHex)
	if encryptedURLErr != nil {
		return utils.HTTPResponseData{}, "", encryptedURLErr
	}

	encryptedHeaders, encryptedHeadersErr := utils.Aes256Encrypt(headersStr, instance.aesKeyHex)
	if encryptedHeadersErr != nil {
		return utils.HTTPResponseData{}, "", encryptedHeadersErr
	}

	encryptedBody, encryptedBodyErr := utils.Aes256Encrypt(bodyStr, instance.aesKeyHex)
	if encryptedBodyErr != nil {
		return utils.HTTPResponseData{}, "", encryptedBodyErr
	}

	runIdentifier, dispatchErr := DispatchWorkflow(
		instance.provider.GetInfos(),
		instance.GetName(),
		map[string]any{
			"methodEnc":  encryptedMethod,
			"urlEnc":     encryptedURL,
			"headersEnc": encryptedHeaders,
			"bodyEnc":    encryptedBody,
		})

	if dispatchErr != nil {
		return utils.HTTPResponseData{}, "", dispatchErr
	}

	worker := CreateWorker()

	instance.sendingRequests[runIdentifier] = &worker

	instance.nbTotalRequestsSent++

	githubResponse := <-worker.GetChannel()

	worker.Close()

	return githubResponse.ResponseData, fmt.Sprintf("runIdentifier=%s", runIdentifier), githubResponse.Error
}

//nolint:revive
func (instance *Repository) IsAvailable() bool {
	return true
}

func (instance *Repository) IsStopped() bool {
	return instance.stopped
}

func (instance *Repository) Clear() bool {
	utils.Logger.Info().Str("launcher", instance.GetName()).Msg("Clearing launcher.")

	deleteErr := DeleteRepository(instance.provider.GetInfos(), instance.GetName())

	utils.Logger.Debug().Str("repositoryName", instance.GetName()).Msg("Deleting GitHub repository.")

	if deleteErr != nil {
		utils.Logger.Error().Err(deleteErr).Str("repositoryName", instance.GetName()).Msg("Error while deleting GitHub repository.")

		return false
	}

	instance.stopped = true

	return true
}

// This function is used to launch a loop to constantly read workflow runs and tries to retrieve its log for the responses' data
func (instance *Repository) LoopWorkflows() {
	for !instance.stopped {
		// Large time to avoid reaching the GitHub API rate limit (5000 req/h) when there are no waiting requests
		time.Sleep(time.Duration(max(5, 10/(len(instance.sendingRequests)+1))) * time.Second) //nolint:gomnd

		runs, runsErr := GetWorkflowRuns(instance.provider.GetInfos(), instance.GetName())

		if runsErr != nil {
			utils.Logger.Warn().Err(runsErr).Msg("Can not retrieve workflow runs.")

			continue
		}

		// For each run, it retrieves its jobs
		for _, run := range runs {
			if !slices.Contains([]string{"completed", "cancelled", "failure", "skipped", "success"}, run.Status) {
				continue
			}

			jobs, jobsErr := GetWorkflowJobs(instance.provider.GetInfos(), instance.GetName(), run.ID)

			if jobsErr != nil {
				utils.Logger.Warn().Err(jobsErr).Int64("runId", run.ID).Msg("Can not retrieve workflow jobs.")

				continue
			}

			var workflowWorker *Worker

			var runIdentifier string

			// It tries to identify which job corresponds to which request
			for _, job := range jobs {
				for _, step := range job.Steps {
					// If the step name (which will be in the Workflow ID Provider) matches one sending request => it retrieves the corresponding chanel (on which the proxy is waiting for this request)
					if currentworker, ok := instance.sendingRequests[step.Name]; ok {
						workflowWorker = currentworker
						runIdentifier = step.Name

						break
					}
				}

				if workflowWorker != nil {
					break
				}
			}

			// If the chanel has been found (the workflow has been identified)
			if workflowWorker != nil {
				for _, job := range jobs {
					if job.Name == "Request" {
						// It checks if the request job has finished
						if !slices.Contains([]string{"in_progress", "queued"}, job.Status) {
							// It starts in a go routine the response data extraction
							go func(jobID int64, runID int64, currentWorker *Worker) {
								githubResponse := instance.ExtractResponseDataFromJobLogs(jobID)

								delete(instance.sendingRequests, runIdentifier)

								if !currentWorker.IsClosed() {
									currentWorker.GetChannel() <- githubResponse
								} else {
									utils.Logger.Warn().Int64("runId", runID).Msg("The worker channel is closed (the client may have terminated the connection).")
								}

								deleteErr := DeleteWorkflowRun(instance.provider.GetInfos(), instance.GetName(), runID)

								if deleteErr != nil {
									utils.Logger.Warn().Err(deleteErr).Int64("runId", runID).Msg("Cannot delete the workflow run.")
								}
							}(job.ID, run.ID, workflowWorker)

							break
						}
					}
				}
			}
		}
	}
}

func (instance *Repository) PrepareSprayerRepository() (create, other error) {
	createErr := CreateRepository(instance.provider.GetInfos(), instance.GetName())

	if createErr != nil {
		return createErr, nil
	}

	addAesKeyErr := CreateOrUpdateRepositorySecret(instance.provider.GetInfos(), instance.GetName(), "AES256_KEY_HEX", instance.aesKeyHex)

	if addAesKeyErr != nil {
		return nil, addAesKeyErr
	}

	addErr1 := AddFileToRepository(instance.provider.GetInfos(), instance.GetName(), "sprayer.py", SPRAYER_PY_FILE_BASE64, "add sprayer.yml")

	if addErr1 != nil {
		return nil, addErr1
	}

	addErr2 := AddFileToRepository(instance.provider.GetInfos(), instance.GetName(), "requirements.txt", REQUIREMENTS_TXT_FILE_BASE64, "add requirements.txt")

	if addErr2 != nil {
		return nil, addErr2
	}

	addErr3 := AddFileToRepository(instance.provider.GetInfos(), instance.GetName(), ".github/workflows/sprayer.yml", SPRAYER_YML_FILE_BASE64, "add sprayer.yml")

	if addErr3 != nil {
		return nil, addErr3
	}

	return nil, nil
}

func (instance *Repository) ExtractResponseDataFromJobLogs(jobID int64) WorkerResponse {
	logs, logsErr := GetWorkflowJobLogs(instance.provider.GetInfos(), instance.GetName(), jobID)

	if logsErr != nil {
		return WorkerResponse{
			Error: logsErr,
		}
	}

	const RESP_ERROR_PREFIX = "RESP_ERR" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_STATUS_PREFIX = "RESP_STATUS_ENCRYPTED_HEX" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_HEADERS_PREFIX = "RESP_HEADERS_ENCRYPTED_HEX" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_BODY_PREFIX = "RESP_BODY_ENCRYPTED_HEX" //nolint:revive,stylecheck //uppercase more readable here

	responseData := utils.HTTPResponseData{
		StatusCode: -1,
		Headers:    nil,
		Body:       nil,
	}

	var statusCodeEncrStr *string

	var headersEncrStr *string

	var bodyEncrStr *string

	for _, log := range logs {
		logInfos := strings.Split(log, " ")

		if len(logInfos) < 3 {
			continue
		}

		if !slices.Contains([]string{RESP_STATUS_PREFIX, RESP_BODY_PREFIX, RESP_HEADERS_PREFIX, RESP_ERROR_PREFIX}, logInfos[1]) {
			continue
		}

		if logInfos[1] == RESP_ERROR_PREFIX {
			if len(logInfos) == 2 {
				return WorkerResponse{
					Error: errors.New("an error happened while treating request"),
				}
			}

			decodedBytes, decodedBytesErr := base64.StdEncoding.DecodeString(logInfos[2])

			if decodedBytesErr != nil {
				return WorkerResponse{
					Error: decodedBytesErr,
				}
			}

			return WorkerResponse{
				Error: errors.New(string(decodedBytes)),
			}
		}

		switch logInfos[1] {
		case RESP_STATUS_PREFIX:
			statusCodeEncrStr = &logInfos[2]
		case RESP_HEADERS_PREFIX:
			value := logInfos[2]

			if headersEncrStr != nil {
				value = *headersEncrStr + value
			}

			headersEncrStr = &value
		case RESP_BODY_PREFIX:
			value := logInfos[2]

			if bodyEncrStr != nil {
				value = *bodyEncrStr + value
			}

			bodyEncrStr = &value
		}
	}

	if statusCodeEncrStr != nil {
		data, dataErr := utils.Aes256Decrypt(*statusCodeEncrStr, instance.aesKeyHex)

		if dataErr != nil {
			return WorkerResponse{
				Error: dataErr,
			}
		}

		decodedInt, decodedIntErr := strconv.Atoi(data)

		if decodedIntErr != nil {
			return WorkerResponse{
				Error: decodedIntErr,
			}
		}

		responseData.StatusCode = decodedInt
	}

	if headersEncrStr != nil {
		data, dataErr := utils.Aes256Decrypt(*headersEncrStr, instance.aesKeyHex)

		if dataErr != nil {
			return WorkerResponse{
				Error: dataErr,
			}
		}

		lines := strings.Split(data, "\n")
		dataMap := make(map[string]any)

		for i := 0; i < len(lines); i += 2 {
			if i+1 < len(lines) {
				key := lines[i]
				value := lines[i+1]
				dataMap[key] = value
			}
		}

		responseData.Headers = dataMap
	}

	if bodyEncrStr != nil {
		data, dataErr := utils.Aes256Decrypt(*bodyEncrStr, instance.aesKeyHex)

		if dataErr != nil {
			return WorkerResponse{
				Error: dataErr,
			}
		}

		responseData.Body = []byte(data)
	}

	if responseData.StatusCode == -1 {
		return WorkerResponse{
			Error: errors.New("no response found in the job logs"),
		}
	}

	return WorkerResponse{
		Error:        nil,
		ResponseData: responseData,
	}
}

func CreateWorker() Worker {
	return Worker{
		channel: make(chan WorkerResponse),
		closed:  false,
	}
}

func (instance *Worker) Close() {
	instance.closed = true
}

func (instance *Worker) IsClosed() bool {
	return instance.closed || instance.channel == nil
}

func (instance *Worker) GetChannel() chan WorkerResponse {
	return instance.channel
}
