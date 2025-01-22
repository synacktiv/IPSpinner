package azure

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"ipspinner/utils"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Launcher object
type CloudShell struct {
	provider            *Azure
	account             *Account
	socket              *websocket.Conn
	name                string
	preferredLocation   string
	socketCreatedTime   int64
	nbTotalRequestsSent int
	socketClosed        bool
	isAvailable         bool // can send new requests (socket has been renewed)
	stopped             bool
}

const maxCloudShellLifeTime = 10 * 60

func (instance CloudShell) SummarizeState() string {
	return fmt.Sprintf("Launcher %s : nbTotalRequestsSent=%d, socketCreatedTime=%d, socketClosed=%t, isAvailable=%t", instance.GetName(), instance.GetNbTotalReqSent(), instance.socketCreatedTime, instance.socketClosed, instance.isAvailable)
}

func CreateCloudShellAccount(provider *Azure, subscriptionID string) (Account, error) {
	username := "ips.cs." + utils.GenerateRandomPrefix(10) //nolint:revive,gomnd
	password := utils.GenerateRandomPassword(15)           //nolint:revive,gomnd

	account, accountErr := provider.GetAdminAccount().CreateAccount(username, password, true)

	if accountErr != nil {
		return Account{}, accountErr
	}

	addToSubErr := provider.GetAdminAccount().AddAccountAsContributorToSubscription(&account, subscriptionID)

	if addToSubErr != nil {
		return Account{}, addToSubErr
	}

	return account, nil
}

func CreateCloudShell(provider *Azure, subscriptionID, preferredLocation string, account Account) (*CloudShell, error) {
	utils.Logger.Info().Str("launcher", account.username).Msg("Creating launcher.")

	azureWebSocket := CloudShell{
		name:                account.username,
		preferredLocation:   preferredLocation,
		nbTotalRequestsSent: 0,
		provider:            provider,
		account:             &account,
		socketClosed:        true,
		isAvailable:         true,
	}

	updateCSPrefErr := account.UpdateCloudshellPreferences(preferredLocation, subscriptionID)

	if updateCSPrefErr != nil {
		azureWebSocket.isAvailable = false

		return &azureWebSocket, updateCSPrefErr
	}

	loadErr := azureWebSocket.loadWebSocketConnection()

	return &azureWebSocket, loadErr
}

func (instance *CloudShell) GetName() string {
	return instance.name
}

// Returns the current socket (and renews it if it is too old, closed or does not exist)
func (instance *CloudShell) GetSocket() (*websocket.Conn, error) {
	if instance.isTooOld() || instance.socket == nil || instance.socketClosed {
		if instance.isTooOld() {
			if closeErr := instance.CloseCurrentSocket(); closeErr != nil {
				return nil, closeErr
			}
		}

		loadErr := instance.loadWebSocketConnection()

		if loadErr != nil {
			return nil, loadErr
		}
	}

	return instance.socket, nil
}

// Creates a new web socket (and if the max is reached, restart all of them)
func (instance *CloudShell) loadWebSocketConnection() error {
	const maxCloudShellErr = "Exceeded 20 concurrent sessions. Please click the restart button."

	socket, socketErr := instance.createNewWebSocketConnection()

	if socketErr != nil {
		// If the max has been reached, restarts and recreates
		if strings.Contains(socketErr.Error(), maxCloudShellErr) {
			restartErr := instance.restartCloudShells()

			if restartErr != nil {
				return restartErr
			}

			newSocket, newSocketErr := instance.createNewWebSocketConnection()

			if newSocketErr != nil {
				return newSocketErr
			}

			instance.socket = newSocket
			instance.socketClosed = false
			instance.socketCreatedTime = time.Now().Unix()

			return nil
		}

		return socketErr
	}

	instance.socket = socket
	instance.socketClosed = false
	instance.socketCreatedTime = time.Now().Unix()

	return nil
}

// Restart all cloud shells (allows to renew the IP)
func (instance *CloudShell) restartCloudShells() error {
	accessToken, accessTokenErr := instance.account.GetAccessToken([]string{"https://management.core.windows.net/user_impersonation"})

	if accessTokenErr != nil {
		return accessTokenErr
	}

	headers := map[string]any{
		"Authorization": fmt.Sprintf("Bearer %s", accessToken.Token),
		"Referer":       "https://ux.console.azure.com",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	restartURL, restartURLErr := url.Parse("https://management.azure.com/providers/Microsoft.Portal/consoles/default?api-version=2023-02-01-preview")

	if restartURLErr != nil {
		return restartURLErr
	}

	respRestart, respRestartErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     restartURL,
		Method:  "DELETE",
		Headers: headers,
		Body:    map[string]any{},
	})

	if respRestartErr != nil {
		return respRestartErr
	}

	if respRestart.StatusCode != http.StatusOK {
		return errors.New("cannot restart Cloud Shell sessions")
	}

	return nil
}

// Creates a web socket connection
func (instance *CloudShell) createNewWebSocketConnection() (*websocket.Conn, error) {
	accessToken, accessTokenErr := instance.account.GetAccessToken([]string{"https://management.core.windows.net/user_impersonation"})

	if accessTokenErr != nil {
		return nil, accessTokenErr
	}

	headers := map[string]any{
		"Authorization": fmt.Sprintf("Bearer %s", accessToken.Token),
		"Referer":       "https://ux.console.azure.com",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	// Retrieving Cloud Shell URL
	reqCreateURL, reqCreateURLErr := url.Parse("https://management.azure.com/providers/Microsoft.Portal/consoles/default?api-version=2023-02-01-preview")

	if reqCreateURLErr != nil {
		return nil, reqCreateURLErr
	}

	reqCreateData := map[string]any{
		"properties": map[string]string{
			"osType": "linux",
		},
	}

	respCreate, respCreateErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqCreateURL,
		Method:  "PUT",
		Headers: headers,
		Body:    reqCreateData,
	})

	if respCreateErr != nil {
		return nil, respCreateErr
	}

	if respCreate.StatusCode != http.StatusOK && respCreate.StatusCode != http.StatusCreated {
		if errorMap, ok := respCreate.Body["error"].(map[string]any); ok {
			return nil, errors.New(errorMap["message"].(string))
		}

		return nil, fmt.Errorf("%v", respCreate.Body)
	}

	properties, ok := respCreate.Body["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("unable to cast properties to map")
	}

	rawCCURL, ok := properties["uri"].(string)
	if !ok {
		return nil, errors.New("unable to cast uri to string")
	}

	// Retrieving token
	reqCCURL, reqCCURLErr := url.Parse(rawCCURL + "/authorize")

	if reqCCURLErr != nil {
		return nil, reqCCURLErr
	}

	respGetToken, respGetTokenErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqCCURL,
		Method:  "POST",
		Headers: headers,
		Body:    map[string]any{},
	})

	if respGetTokenErr != nil {
		return nil, respGetTokenErr
	}

	token := respGetToken.Body["token"]

	// Retrieving websocket URL
	reqGetSocketURL, reqGetSocketURLErr := url.Parse(rawCCURL + "/terminals?cols=103&rows=13&version=2019-01-01&shell=bash")

	if reqGetSocketURLErr != nil {
		return nil, reqGetSocketURLErr
	}

	respGetSocketURL, respGetSocketURLErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqGetSocketURL,
		Method:  "POST",
		Headers: headers,
		Body:    map[string]any{},
	})

	if respGetSocketURLErr != nil {
		return nil, respGetSocketURLErr
	}

	if respGetSocketURL.StatusCode != http.StatusOK {
		return nil, errors.New(respGetSocketURL.Body["error"].(map[string]any)["message"].(string))
	}

	// Parsing URLs
	socketURL, socketURLErr := url.Parse(respGetSocketURL.Body["socketUri"].(string))

	if socketURLErr != nil {
		return nil, socketURLErr
	}

	ccURL, ccURLErr := url.Parse(rawCCURL)

	if ccURLErr != nil {
		return nil, ccURLErr
	}

	// Formatting Cloud Shell URL
	host := ccURL.Host
	path := strings.Trim(ccURL.Path, "/")
	id := strings.Trim(socketURL.Path, "/")

	shellURL := fmt.Sprintf("wss://%s/$hc/%s/terminals/%s", host, path, id)

	socketHeaders := http.Header{}
	socketHeaders.Add("Cookie", fmt.Sprintf("auth-token=%s", token))

	dialer := websocket.Dialer{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}

	// Creates the socket connection
	socketConnection, _, socketConnectionErr := dialer.Dial(shellURL, socketHeaders) //nolint:bodyclose //closing process handled somewhere else

	if socketConnectionErr != nil {
		return nil, socketConnectionErr
	}

	instance.socket = socketConnection
	instance.socketCreatedTime = time.Now().Unix()

	return socketConnection, nil
}

func (instance *CloudShell) isTooOld() bool {
	return (time.Now().Unix() - instance.socketCreatedTime) > maxCloudShellLifeTime
}

// Waits for the cloud shell response and parses it
func (instance *CloudShell) waitForCloudShellResponse() (utils.HTTPResponseData, error) {
	const RESP_START_PREFIX = "RESP_START" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_ERROR_PREFIX = "RESP_ERR" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_END_PREFIX = "RESP_END" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_STATUS_PREFIX = "RESP_STATUS_ENC" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_HEADERS_PREFIX = "RESP_HEADERS_ENC" //nolint:revive,stylecheck //uppercase more readable here

	const RESP_BODY_PREFIX = "RESP_BODY_ENC" //nolint:revive,stylecheck //uppercase more readable here

	fullResponseContent := ""

	for !instance.socketClosed {
		// Retrieves the last received message
		_, message, messageErr := instance.socket.ReadMessage()

		if messageErr != nil {
			// Additional isClosed condition in the case of the socket connection has been closed while waiting for a new message
			if instance.socketClosed {
				break
			}

			// If the socket connection has been closed
			if !websocket.IsUnexpectedCloseError(messageErr, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				instance.socketClosed = true
			}

			utils.Logger.Error().Str("provider", instance.provider.GetName()).Str("err", messageErr.Error()).Msg("An error occurred while reading websocket message.")

			continue
		}

		messageStr := string(message)

		fullResponseContent += messageStr

		// If the whole information has still not been received
		if !(strings.Contains(fullResponseContent, RESP_START_PREFIX) && (strings.Contains(fullResponseContent, RESP_ERROR_PREFIX) || strings.Contains(fullResponseContent, RESP_END_PREFIX))) {
			continue
		}

		responseInfos := strings.Split(fullResponseContent, "\r\n")

		// If the request has succeeded
		if strings.Contains(fullResponseContent, "RESP_END") {
			// It retrieves the request status
			rawRespStatus := responseInfos[getSliceFirstIndexStartingWith(responseInfos, RESP_STATUS_PREFIX+" ")][(len(RESP_STATUS_PREFIX) + 1):]

			respStatusDecodedBytes, respStatusDecodedBytesErr := base64.StdEncoding.DecodeString(rawRespStatus)

			if respStatusDecodedBytesErr != nil {
				return utils.HTTPResponseData{}, respStatusDecodedBytesErr
			}

			respStatus, respStatusErr := strconv.Atoi(string(respStatusDecodedBytes))

			if respStatusErr != nil {
				return utils.HTTPResponseData{}, respStatusErr
			}

			// It retrieves the response headers
			rawRespHeaders := responseInfos[getSliceFirstIndexStartingWith(responseInfos, RESP_HEADERS_PREFIX+" ")][(len(RESP_HEADERS_PREFIX) + 1):]

			respHeadersDecodedBytes, respHeadersDecodedBytesErr := base64.StdEncoding.DecodeString(rawRespHeaders)

			if respHeadersDecodedBytesErr != nil {
				return utils.HTTPResponseData{}, respHeadersDecodedBytesErr
			}

			respHeadersLines := strings.Split(string(respHeadersDecodedBytes), "\n")

			respHeaders := make(map[string]any)

			for i := 0; i < len(respHeadersLines); i += 2 {
				if i+1 < len(respHeadersLines) {
					key := respHeadersLines[i]
					value := respHeadersLines[i+1]
					respHeaders[key] = value
				}
			}

			// It retrieves the response body
			respBodyFirstIndex := getSliceFirstIndexStartingWith(responseInfos, RESP_BODY_PREFIX+" ")
			respBodyLastIndex := getSliceLastIndexStartingWith(responseInfos, RESP_BODY_PREFIX+" ")

			rawRespBody := ""
			for i := respBodyFirstIndex; i <= respBodyLastIndex; i++ {
				rawRespBody += responseInfos[i][len(RESP_BODY_PREFIX)+1:]
			}

			respBodyDecodedBytes, respBodyDecodedBytesErr := base64.StdEncoding.DecodeString(rawRespBody)

			if respBodyDecodedBytesErr != nil {
				return utils.HTTPResponseData{}, respBodyDecodedBytesErr
			}

			// Finally, it sends the response data
			return utils.HTTPResponseData{
				StatusCode: respStatus,
				Headers:    respHeaders,
				Body:       respBodyDecodedBytes,
			}, nil
		} else if strings.Contains(fullResponseContent, "RESP_ERR") { // Otherwise, if the request has failed
			return utils.HTTPResponseData{}, fmt.Errorf("an error occurred while executing the request")
		}
	}

	return utils.HTTPResponseData{}, errors.New("the websocket connection has been closed")
}

func (account *Account) UpdateCloudshellPreferences(preferredLocation, subscriptionID string) error {
	token, tokenErr := account.GetAccessToken([]string{"https://management.core.windows.net/.default"})

	if tokenErr != nil {
		return tokenErr
	}

	headers := map[string]any{
		"Authorization": utils.PrepareBearerHeader(token.Token),
		"Accept":        "*/*",
		"Content-Type":  "application/json",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36",
	}

	reqURL, reqURLErr := url.Parse("https://management.azure.com/providers/Microsoft.Portal/userSettings/cloudconsole?api-version=2023-02-01-preview")

	if reqURLErr != nil {
		return reqURLErr
	}

	reqData := map[string]any{
		"properties": map[string]any{
			"preferredOsType":   "",
			"preferredLocation": preferredLocation,
			"storageProfile":    nil,
			"terminalSettings": map[string]any{
				"fontSize":  "medium",
				"fontStyle": "monospace",
			},
			"vnetSettings":       nil,
			"userSubscription":   subscriptionID,
			"sessionType":        "Ephemeral",
			"networkType":        "Default",
			"preferredShellType": "bash",
		},
	}

	resp, respErr := utils.SendJSONRequest(utils.HTTPRequestJSONData{
		URL:     reqURL,
		Method:  "PUT",
		Headers: headers,
		Body:    reqData,
	})

	if respErr != nil {
		return respErr
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot update %s user account's cloudshell preferences", account.username)
	}

	return nil
}

func PrepareCloudShellCommand(reqData utils.HTTPRequestData) string {
	// It prepares and encodes the request headers to a base64 format
	headersStr := ""

	if reqData.Headers != nil {
		for headerKey, headerVal := range reqData.Headers {
			headersStr += headerKey + "\n" + fmt.Sprintf("%v", headerVal) + "\n"
		}

		if len(headersStr) > 0 {
			headersStr = headersStr[:(len(headersStr) - 1)]
		}
	}

	// It prepares and encodes the request body to a base64 format
	bodyStr := ""

	if reqData.Body != nil {
		bodyStr = reqData.Body.String()
	}

	cmd := fmt.Sprintf("pip3 install requests && python3 -c \"$(echo '%s' | base64 --decode)\" %s %s %s %s",
		SCRIPT_PY_FILE_BASE64,
		base64.StdEncoding.EncodeToString([]byte(reqData.Method)),
		base64.StdEncoding.EncodeToString([]byte(reqData.URL.String())),
		base64.StdEncoding.EncodeToString([]byte(headersStr)),
		base64.StdEncoding.EncodeToString([]byte(bodyStr)))

	return cmd
}

func (instance *CloudShell) SendRequest(ctx context.Context, reqData utils.HTTPRequestData, allConfigs *utils.AllConfigs) (utils.HTTPResponseData, string, error) { //nolint:revive
	// Sets this cloud shell unavailable to next requests
	instance.isAvailable = false

	cmd := PrepareCloudShellCommand(reqData)

	socket, socketErr := instance.GetSocket()

	if socketErr != nil {
		return utils.HTTPResponseData{}, fmt.Sprintf("location=%s", instance.preferredLocation), nil
	}

	// It sends the command
	writeErr := socket.WriteMessage(websocket.TextMessage, []byte(cmd+"\n"))

	if writeErr != nil {
		return utils.HTTPResponseData{}, fmt.Sprintf("location=%s", instance.preferredLocation), writeErr
	}

	instance.nbTotalRequestsSent++

	resp, respErr := instance.waitForCloudShellResponse()

	// In a go routine (to avoid waiting to send the response), it closes and renews the connection
	go func(instance *CloudShell) {
		closeErr := instance.CloseCurrentSocket()

		restartErr := instance.restartCloudShells()

		loadErr := instance.loadWebSocketConnection()

		if closeErr != nil {
			utils.Logger.Warn().Err(closeErr).Str("provider", instance.provider.GetName()).Msg("Cannot close web socket connection.")
		}

		if restartErr != nil {
			utils.Logger.Warn().Err(restartErr).Str("provider", instance.provider.GetName()).Msg("Cannot restart Cloud Shells.")
		}

		if loadErr != nil {
			utils.Logger.Warn().Err(loadErr).Str("provider", instance.provider.GetName()).Msg("Cannot load a new web socket connection.")
		}

		instance.isAvailable = true
	}(instance)

	if respErr != nil {
		return utils.HTTPResponseData{}, fmt.Sprintf("location=%s", instance.preferredLocation), respErr
	}

	return resp, fmt.Sprintf("location=%s", instance.preferredLocation), nil
}

// Closes current socket connection
func (instance *CloudShell) CloseCurrentSocket() error {
	if instance.socketClosed || instance.socket == nil {
		return nil
	}

	instance.socketClosed = true

	writeErr := instance.socket.WriteMessage(websocket.TextMessage, []byte("exit"))

	utils.Logger.Debug().Str("provider", instance.provider.GetName()).Msg("Closing web socket connection.")

	if writeErr != nil {
		instance.socketClosed = false

		return writeErr
	}

	closeErr := instance.socket.Close()

	if closeErr != nil {
		instance.socketClosed = false

		return closeErr
	}

	return nil
}

func (instance *CloudShell) IsAvailable() bool {
	return instance.isAvailable
}

func (instance *CloudShell) IsStopped() bool {
	return instance.stopped
}

//nolint:revive
func (instance *CloudShell) PreloadHosts(ctx context.Context, allPreloadHosts []*url.URL) {
	utils.Logger.Info().Str("provider", instance.GetName()).Msg("This provider cannot preload hosts.")
}

func (instance *CloudShell) GetProvider() utils.Provider {
	return instance.provider
}

func (instance *CloudShell) Clear() bool {
	utils.Logger.Info().Str("launcher", instance.GetName()).Msg("Clearing launcher.")

	fullyCleared := true

	closeErr := instance.CloseCurrentSocket()

	if closeErr != nil {
		fullyCleared = false

		utils.Logger.Error().Err(closeErr).Str("provider", instance.GetProvider().GetName()).Str("launcher", instance.GetName()).Msg("Cannot close the socket.")
	}

	if instance.account.needToBeCleared {
		utils.Logger.Debug().Str("account", instance.account.username).Msg("Deleting Azure account and associated role assignments.")

		deleteAssignmentsErr := instance.provider.GetAdminAccount().DeleteCreatedRoleAssignments(instance.account)

		if deleteAssignmentsErr != nil {
			fullyCleared = false

			utils.Logger.Error().Err(deleteAssignmentsErr).Str("provider", instance.GetProvider().GetName()).Str("launcher", instance.GetName()).Str("username", instance.account.username).Msg("Cannot delete account's role assignments.")
		}

		deleteAccountErr := instance.provider.GetAdminAccount().DeleteAccount(instance.account)

		if deleteAccountErr != nil {
			fullyCleared = false

			utils.Logger.Error().Err(deleteAccountErr).Str("provider", instance.GetProvider().GetName()).Str("launcher", instance.GetName()).Str("username", instance.account.username).Msg("Cannot delete account.")
		}
	}

	instance.stopped = fullyCleared

	return fullyCleared
}

func (instance CloudShell) GetNbTotalReqSent() int {
	return instance.nbTotalRequestsSent
}

func getSliceFirstIndexStartingWith(slice []string, startsWith string) int {
	for i, line := range slice {
		if strings.HasPrefix(line, startsWith) {
			return i
		}
	}

	return -1
}

func getSliceLastIndexStartingWith(slice []string, startsWith string) int {
	for i := (len(slice) - 1); i >= 0; i-- {
		line := slice[i]
		if strings.HasPrefix(line, startsWith) {
			return i
		}
	}

	return -1
}
