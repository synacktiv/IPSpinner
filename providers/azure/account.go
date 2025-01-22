package azure

import (
	"context"
	"errors"
	"fmt"
	"ipspinner/utils"
	"net/http"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

type Account struct {
	username               string
	ID                     string
	userPrincipalName      string
	tenantID               string
	tokenCredential        *azcore.TokenCredential
	createdRoleAssignments []AccountRoleAssignment
	needToBeCleared        bool
}

type AccountRoleAssignment struct {
	subscriptionID   string
	roleAssignmentID string
}

// ID and userPrincipalName can be empty
func ConnectAccount(email, password, tenantID string, needToBeCleared bool, id, userPrincipalName string) (Account, error) {
	var cred azcore.TokenCredential

	cred, credsErr := getAzIdentityFromNewUsernamePasswordCredential(email, password, tenantID)

	if credsErr != nil {
		return Account{}, credsErr
	}

	username := strings.Split(email, "@")[0]

	account := Account{
		username:          username,
		tokenCredential:   &cred,
		tenantID:          tenantID,
		needToBeCleared:   needToBeCleared,
		ID:                id,
		userPrincipalName: userPrincipalName,
	}

	return account, nil
}

func (account *Account) loadUserInformations() error {
	token, tokenErr := account.GetAccessToken([]string{"https://graph.microsoft.com/.default"})

	if tokenErr != nil {
		return tokenErr
	}

	headers := map[string]any{
		"Authorization": utils.PrepareBearerHeader(token.Token),
		"Content-Type":  "application/json",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	reqURL, reqURLErr := url.Parse("https://graph.microsoft.com/v1.0/me")

	if reqURLErr != nil {
		return reqURLErr
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqURL,
		Method:  "GET",
		Headers: headers,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("cannot retrieve account's informations")
	}

	accountID, accountIDRes := resp.Body["id"].(string)
	if !accountIDRes {
		return errors.New("can not retrieve account's ID")
	}

	account.ID = accountID

	upn, upnRes := resp.Body["userPrincipalName"].(string)
	if !upnRes {
		return errors.New("can not retrieve account's UPN")
	}

	account.userPrincipalName = upn

	return nil
}

func (account *Account) GetID() (string, error) {
	if len(account.ID) > 0 {
		return account.ID, nil
	}

	err := account.loadUserInformations()

	return account.ID, err
}

func (account *Account) GetUserPrincipalName() (string, error) {
	if len(account.userPrincipalName) > 0 {
		return account.userPrincipalName, nil
	}

	err := account.loadUserInformations()

	return account.userPrincipalName, err
}

func (adminAccount *Account) AddAccountAsContributorToSubscription(userAccount *Account, subscriptionID string) error { //nolint:stylecheck
	token, tokenErr := adminAccount.GetAccessToken([]string{"https://management.core.windows.net/.default"})

	if tokenErr != nil {
		return tokenErr
	}

	headers := map[string]any{
		"Authorization": utils.PrepareBearerHeader(token.Token),
		"Accept":        "*/*",
		"Content-Type":  "application/json",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	roleAssignmentID := utils.GenerateUUIDv4()

	reqURL, reqURLErr := url.Parse("https://management.azure.com/batch?api-version=2020-06-01")

	if reqURLErr != nil {
		return reqURLErr
	}

	userAccountID, userAccountIDErr := userAccount.GetID()

	if userAccountIDErr != nil {
		return userAccountIDErr
	}

	reqData := map[string]any{
		"requests": []map[string]any{
			{
				"content": map[string]any{
					"Id": roleAssignmentID,
					"Properties": map[string]any{
						"Id":               roleAssignmentID,
						"PrincipalId":      userAccountID,
						"PrincipalType":    "User",
						"RoleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/b24988ac-6180-42a0-ab88-20f7382dd24c",
						"Scope":            fmt.Sprintf("/subscriptions/%s", subscriptionID),
						"Condition":        nil,
						"ConditionVersion": nil,
					},
				},
				"httpMethod": "PUT",
				"name":       "b131bce0-1e07-4786-a666-2fa3c7235004",
				"requestHeaderDetails": map[string]any{
					"commandName": "Microsoft_Azure_AD.AddRoleAssignments.batch",
				},
				"url": fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.Authorization/roleAssignments/%s?api-version=2020-04-01-preview", subscriptionID, roleAssignmentID),
			},
		},
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqURL,
		Method:  "POST",
		Headers: headers,
		Body:    reqData,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot add %s to the subscription #%s as Contributor", userAccount.username, subscriptionID)
	}

	userAccount.createdRoleAssignments = append(userAccount.createdRoleAssignments, AccountRoleAssignment{
		subscriptionID:   subscriptionID,
		roleAssignmentID: roleAssignmentID,
	})

	return nil
}

func (adminAccount *Account) DeleteAccount(userAccount *Account) error {
	token, tokenErr := adminAccount.GetAccessToken([]string{"https://graph.microsoft.com/.default"})

	if tokenErr != nil {
		return tokenErr
	}

	headers := map[string]any{
		"Authorization": fmt.Sprintf("Bearer %s", token.Token),
		"Accept":        "*/*",
		"Content-Type":  "application/json",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	reqURL, reqURLErr := url.Parse("https://graph.microsoft.com/v1.0/$batch")

	if reqURLErr != nil {
		return reqURLErr
	}

	userAccountID, userAccountIDErr := userAccount.GetID()

	if userAccountIDErr != nil {
		return userAccountIDErr
	}

	reqData := map[string]any{
		"requests": []map[string]any{
			{
				"id":     userAccountID,
				"method": "DELETE",
				"url":    fmt.Sprintf("/users/%s", userAccountID),
			},
		},
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqURL,
		Method:  "POST",
		Headers: headers,
		Body:    reqData,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot delete %s user account", userAccount.username)
	}

	return nil
}

func (adminAccount *Account) DeleteCreatedRoleAssignments(userAccount *Account) error {
	token, tokenErr := adminAccount.GetAccessToken([]string{"https://management.core.windows.net/.default"})

	if tokenErr != nil {
		return tokenErr
	}

	headers := map[string]any{
		"Authorization": fmt.Sprintf("Bearer %s", token.Token),
		"Accept":        "*/*",
		"Content-Type":  "application/json",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	reqURL, reqURLErr := url.Parse("https://management.azure.com/batch?api-version=2020-06-01")

	if reqURLErr != nil {
		return reqURLErr
	}

	allRequests := []map[string]any{}

	for _, roleAssignment := range userAccount.createdRoleAssignments {
		allRequests = append(allRequests, map[string]any{
			"httpMethod": "DELETE",
			"name":       utils.GenerateUUIDv4(),
			"requestHeaderDetails": map[string]any{
				"commandName": "Microsoft_Azure_AD.DeleteRoleAssignment.batch",
			},
			"url": fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.Authorization/roleAssignments/%s?api-version=2020-04-01-preview", roleAssignment.subscriptionID, roleAssignment.roleAssignmentID),
		})
	}

	reqData := map[string]any{
		"requests": allRequests,
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqURL,
		Method:  "POST",
		Headers: headers,
		Body:    reqData,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot delete %s user account's created role assignments", userAccount.username)
	}

	return nil
}

func (adminAccount *Account) CreateAccount(username, password string, needToBeCleared bool) (Account, error) {
	token, tokenErr := adminAccount.GetAccessToken([]string{"https://graph.microsoft.com/.default"})

	if tokenErr != nil {
		return Account{}, tokenErr
	}

	adminEmail, adminEmailErr := adminAccount.GetUserPrincipalName()

	if adminEmailErr != nil {
		return Account{}, adminEmailErr
	}

	if len(strings.Split(adminEmail, "@")) < 2 {
		return Account{}, fmt.Errorf("cannot determine Azure domain")
	}

	domain := strings.Split(adminEmail, "@")[1]

	headers := map[string]any{
		"Authorization": fmt.Sprintf("Bearer %s", token.Token),
		"Accept":        "*/*",
		"Content-Type":  "application/json",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	reqURL, reqURLErr := url.Parse("https://graph.microsoft.com/v1.0/$batch")

	if reqURLErr != nil {
		return Account{}, reqURLErr
	}

	email := fmt.Sprintf("%s@%s", username, domain)

	reqData := map[string]any{
		"requests": []map[string]any{
			{
				"id":     utils.GenerateUUIDv4(),
				"method": "POST",
				"url":    "/users",
				"body": map[string]any{
					"accountEnabled": true,
					"displayName":    username,
					"passwordProfile": map[string]any{
						"forceChangePasswordNextSignIn": false,
						"password":                      password,
					},
					"mailNickname":      username,
					"userPrincipalName": email,
				},
				"headers": map[string]any{
					"Content-Type": "application/json",
				},
			},
		},
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqURL,
		Method:  "POST",
		Headers: headers,
		Body:    reqData,
	})

	if respErr != nil {
		return Account{}, respErr
	}

	var statusCode float64

	bodyResponses, ok := resp.Body["responses"].([]any)
	if !ok {
		return Account{}, errors.New("can not parse body responses")
	}

	bodyResponse, ok := bodyResponses[0].(map[string]any)
	if !ok {
		return Account{}, errors.New("can not parse body response")
	}

	bodyResponseStatusCode, ok := bodyResponse["status"].(float64)
	if !ok {
		return Account{}, errors.New("can not parse body status code")
	}

	statusCode = bodyResponseStatusCode

	if statusCode != http.StatusCreated {
		statusMessage := "Unknown error"

		if statusCode > 300 { //nolint:revive,gomnd
			responses, ok := resp.Body["responses"].([]any)
			if !ok || len(responses) == 0 {
				return Account{}, errors.New("unable to cast or empty responses")
			}

			response, ok := responses[0].(map[string]any)
			if !ok {
				return Account{}, errors.New("unable to cast response to map")
			}

			body, ok := response["body"].(map[string]any)
			if !ok {
				return Account{}, errors.New("unable to cast body to map")
			}

			errorField, ok := body["error"].(map[string]any)
			if !ok {
				return Account{}, errors.New("unable to cast error to map")
			}

			message, ok := errorField["message"].(string)
			if !ok {
				return Account{}, errors.New("unable to cast message to string")
			}

			statusMessage = message
		}

		return Account{}, fmt.Errorf("cannot create %s user account (%s)", username, statusMessage)
	}

	responses, ok := resp.Body["responses"].([]any)
	if !ok || len(responses) == 0 {
		return Account{}, errors.New("unable to cast or empty responses")
	}

	response, ok := responses[0].(map[string]any)
	if !ok {
		return Account{}, errors.New("unable to cast response to map")
	}

	body, ok := response["body"].(map[string]any)
	if !ok {
		return Account{}, errors.New("unable to cast body to map")
	}

	accountID, ok := body["id"].(string)
	if !ok {
		return Account{}, errors.New("unable to cast id to string")
	}

	accountUserPrincipalName, ok := body["userPrincipalName"].(string)
	if !ok {
		return Account{}, errors.New("unable to cast userPrincipalName to string")
	}

	return ConnectAccount(email, password, adminAccount.tenantID, needToBeCleared, accountID, accountUserPrincipalName)
}

func (account *Account) GetAccessToken(scopes []string) (azcore.AccessToken, error) {
	token, tokenErr := (*account.tokenCredential).GetToken(context.Background(), policy.TokenRequestOptions{
		Scopes: scopes,
	})

	return token, tokenErr
}

//nolint:gocritic
func (account *Account) GetCredentials() *azcore.TokenCredential {
	return account.tokenCredential
}

// func (account *Account) createAccount(username, password string) (Account, error) {

// }

func getAzIdentityFromNewUsernamePasswordCredential(email, password, tenantID string) (*azidentity.UsernamePasswordCredential, error) {
	// Connection from tenantID, clientID, username and password
	// Client ID : the client (application) ID of an App Registration in the tenant.
	// https://learn.microsoft.com/en-us/javascript/api/@azure/identity/usernamepasswordcredential?view=azure-node-latest
	cred, err := azidentity.NewUsernamePasswordCredential(tenantID, "04b07795-8ddb-461a-bbee-02f9e1bf7b46", email, password, nil)

	return cred, err
}
