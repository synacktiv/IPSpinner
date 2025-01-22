package github

import (
	"errors"
	"fmt"
	"ipspinner/utils"
	"net/http"
	"net/url"
	"strings"

	"github.com/jefflinse/githubsecret"
)

type WorkflowRun struct {
	Name   string
	Status string
	ID     int64
}

type WorkflowJob struct {
	Name         string
	WorkflowName string
	Status       string
	ID           int64
	RunID        int64
	Steps        []WorkflowJobStep
}

type WorkflowJobStep struct {
	Name   string
	Status string
}

func (infos Infos) getHeaders() map[string]any {
	return map[string]any{
		"Accept":               infos.Accept,
		"X-GitHub-Api-Version": infos.APIVersion,
		"Authorization":        fmt.Sprintf("Bearer %s", infos.Token),
	}
}

// Creates a repository with the provided information
func CreateRepository(infos Infos, repositoryName string) error {
	reqBody := map[string]any{
		"name": repositoryName,
	}

	githubURL, githubURLErr := url.Parse("https://api.github.com/user/repos")

	if githubURLErr != nil {
		return githubURLErr
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     githubURL,
		Method:  "POST",
		Headers: infos.getHeaders(),
		Body:    reqBody,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusCreated {
		return errors.New(utils.GetOrDefault(resp.Body, "message", "Can not create the repository.").(string))
	}

	return nil
}

// Adds a file to the repository (file content encoded in base64)
func AddFileToRepository(infos Infos, repositoryName, path, fileContentB64, commitMessage string) error {
	reqBody := map[string]any{
		"message": commitMessage,
		"content": fileContentB64,
		"private": true,
	}

	path = strings.TrimPrefix(path, "/")

	githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", infos.Username, repositoryName, path))

	if githubURLErr != nil {
		return githubURLErr
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     githubURL,
		Method:  "PUT",
		Headers: infos.getHeaders(),
		Body:    reqBody,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusCreated {
		return errors.New(utils.GetOrDefault(resp.Body, "message", "Can not add the file to the directory.").(string))
	}

	return nil
}

// Dispatches the provided workflow
func DispatchWorkflow(infos Infos, repositoryName string, inputs map[string]any) (string, error) {
	runIdentifier := utils.GenerateRandomPrefix(10) //nolint:revive,gomnd // used to retrieve the workflow ID later as it is launched asynchronously : https://stackoverflow.com/questions/69479400/get-run-id-after-triggering-a-github-workflow-dispatch-event

	inputs["runIdentifier"] = runIdentifier

	reqBody := map[string]any{
		"ref":    "main",
		"inputs": inputs,
	}

	githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/workflows/sprayer.yml/dispatches", infos.Username, repositoryName))

	if githubURLErr != nil {
		return runIdentifier, githubURLErr
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     githubURL,
		Method:  "POST",
		Headers: infos.getHeaders(),
		Body:    reqBody,
	})

	if respErr != nil {
		return runIdentifier, respErr
	}

	if resp.StatusCode != http.StatusNoContent {
		return runIdentifier, errors.New(utils.GetOrDefault(resp.Body, "message", "Can not dispatch the workflow.").(string))
	}

	return runIdentifier, nil
}

// Retrieves all workflow runs in the provided repository
func GetWorkflowRuns(infos Infos, repositoryName string) ([]WorkflowRun, error) {
	allWorkflowRuns := []WorkflowRun{}

	perPage := 100 //nolint:revive
	page := 1
	maxPage := 1

	for page <= maxPage {
		githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs?page=%d&per_page=%d", infos.Username, repositoryName, page, perPage))

		if githubURLErr != nil {
			return allWorkflowRuns, githubURLErr
		}

		resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
			URL:     githubURL,
			Method:  "GET",
			Headers: infos.getHeaders(),
			Body:    nil,
		})

		if respErr != nil {
			return allWorkflowRuns, respErr
		}

		if resp.StatusCode != http.StatusOK {
			return allWorkflowRuns, errors.New(utils.GetOrDefault(resp.Body, "message", "Can not list workflow runs.").(string))
		}

		for _, rawWorkflowRun := range resp.Body["workflow_runs"].([]any) {
			allWorkflowRuns = append(allWorkflowRuns, WorkflowRun{
				ID:     int64(rawWorkflowRun.(map[string]any)["id"].(float64)),
				Name:   rawWorkflowRun.(map[string]any)["name"].(string),
				Status: rawWorkflowRun.(map[string]any)["status"].(string),
			})
		}

		maxPage = int(utils.GetOrDefault(resp.Body, "total_count", 1).(float64))/perPage + 1

		page++
	}

	return allWorkflowRuns, nil
}

// Retrieves all workflow jobs in the provided repository / workflow
func GetWorkflowJobs(infos Infos, repositoryName string, workflowID int64) ([]WorkflowJob, error) {
	allWorkflowJobs := []WorkflowJob{}

	perPage := 100 //nolint:revive
	page := 1
	maxPage := 1

	for page <= maxPage {
		githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs/%d/jobs?page=%d&per_page=%d", infos.Username, repositoryName, workflowID, page, perPage))

		if githubURLErr != nil {
			return allWorkflowJobs, githubURLErr
		}

		resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
			URL:     githubURL,
			Method:  "GET",
			Headers: infos.getHeaders(),
			Body:    nil,
		})

		if respErr != nil {
			return allWorkflowJobs, respErr
		}

		if resp.StatusCode != http.StatusOK {
			return allWorkflowJobs, errors.New(utils.GetOrDefault(resp.Body, "message", "Can not list workflow jobs.").(string))
		}

		for _, rawWorkflowJob := range resp.Body["jobs"].([]any) {
			allWorkflowJobSteps := []WorkflowJobStep{}

			for _, rawWorkflowJobStep := range rawWorkflowJob.(map[string]any)["steps"].([]any) {
				allWorkflowJobSteps = append(allWorkflowJobSteps, WorkflowJobStep{
					Name:   rawWorkflowJobStep.(map[string]any)["name"].(string),
					Status: rawWorkflowJobStep.(map[string]any)["status"].(string),
				})
			}

			allWorkflowJobs = append(allWorkflowJobs, WorkflowJob{
				ID:           int64(rawWorkflowJob.(map[string]any)["id"].(float64)),
				RunID:        int64(rawWorkflowJob.(map[string]any)["run_id"].(float64)),
				Name:         rawWorkflowJob.(map[string]any)["name"].(string),
				Status:       rawWorkflowJob.(map[string]any)["status"].(string),
				WorkflowName: rawWorkflowJob.(map[string]any)["name"].(string),
				Steps:        allWorkflowJobSteps,
			})
		}

		maxPage = int(utils.GetOrDefault(resp.Body, "total_count", 1).(float64))/perPage + 1

		page++
	}

	return allWorkflowJobs, nil
}

// Retrieves the logs of the provided workflow job
func GetWorkflowJobLogs(infos Infos, repositoryName string, jobID int64) ([]string, error) {
	githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%d/logs", infos.Username, repositoryName, jobID))

	if githubURLErr != nil {
		return []string{}, githubURLErr
	}

	resp, respErr := utils.SendRequest(utils.HTTPRequestData{
		URL:                githubURL,
		Method:             "GET",
		Headers:            infos.getHeaders(),
		FollowRedirections: true,
		Body:               nil,
	})

	if respErr != nil {
		return []string{}, respErr
	}

	if resp.StatusCode != http.StatusOK {
		return []string{}, errors.New(string(resp.Body))
	}

	return strings.Split(string(resp.Body), "\n"), nil
}

// Deletes the provided workflow run
func DeleteWorkflowRun(infos Infos, repositoryName string, runID int64) error {
	githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs/%d", infos.Username, repositoryName, runID))

	if githubURLErr != nil {
		return githubURLErr
	}

	resp, respErr := utils.SendRequest(utils.HTTPRequestData{
		URL:     githubURL,
		Method:  "DELETE",
		Headers: infos.getHeaders(),
		Body:    nil,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusNoContent {
		return errors.New(string(resp.Body))
	}

	return nil
}

// Deletes the provided repository
func DeleteRepository(infos Infos, repositoryName string) error {
	githubURL, githubURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s", infos.Username, repositoryName))

	if githubURLErr != nil {
		return githubURLErr
	}

	resp, respErr := utils.SendRequest(utils.HTTPRequestData{
		URL:     githubURL,
		Method:  "DELETE",
		Headers: infos.getHeaders(),
		Body:    nil,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("can not delete the repository, status code: %d", resp.StatusCode)
	}

	return nil
}

// Creates or updates a GitHub repository secret
func CreateOrUpdateRepositorySecret(infos Infos, repositoryName, secretName, secretValue string) error {
	// First, it retrieves the repository public key
	githubPublicKeyURL, githubPublicKeyURLErr := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/secrets/public-key", infos.Username, repositoryName))

	if githubPublicKeyURLErr != nil {
		return githubPublicKeyURLErr
	}

	respPublicKey, respPublicKeyErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     githubPublicKeyURL,
		Method:  "GET",
		Headers: infos.getHeaders(),
		Body:    nil,
	})

	if respPublicKeyErr != nil {
		return respPublicKeyErr
	}

	if respPublicKey.StatusCode != http.StatusOK {
		return errors.New(utils.GetOrDefault(respPublicKey.Body, "message", "Can not retrieve the repository public key (1).").(string))
	}

	key, keyOk := respPublicKey.Body["key"].(string)
	keyID, keyIDOk := respPublicKey.Body["key_id"].(string)

	if !keyOk || !keyIDOk {
		return errors.New(utils.GetOrDefault(respPublicKey.Body, "message", "Can not retrieve the repository public key (2).").(string))
	}

	secretEncrypted, secretEncryptedErr := githubsecret.Encrypt(key, secretValue)

	if secretEncryptedErr != nil {
		return secretEncryptedErr
	}

	githubCreateOrUpdateURL, githubCreateOrUpdate := url.Parse(fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/secrets/%s", infos.Username, repositoryName, secretName))

	if githubCreateOrUpdate != nil {
		return githubCreateOrUpdate
	}

	reqCreateOrUpdateBody := map[string]any{
		"encrypted_value": secretEncrypted,
		"key_id":          keyID,
	}

	respCreateOrUpdate, respCreateOrUpdateErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     githubCreateOrUpdateURL,
		Method:  "PUT",
		Headers: infos.getHeaders(),
		Body:    reqCreateOrUpdateBody,
	})

	if respCreateOrUpdateErr != nil {
		return respCreateOrUpdateErr
	}

	if respCreateOrUpdate.StatusCode != http.StatusCreated {
		return fmt.Errorf("can not update repository secrets, status code: %d", respCreateOrUpdate.StatusCode)
	}

	return nil
}
