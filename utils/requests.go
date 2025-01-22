package utils

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Sends a HTTP request with the provided request json data and receives a json-body response
func SendJSONRequest(reqJSONData HTTPRequestJSONData) (HTTPResponseJSONData, error) {
	body := &bytes.Buffer{}

	if reqJSONData.Body != nil {
		reqBodyErr := json.NewEncoder(body).Encode(reqJSONData.Body)

		if reqBodyErr != nil {
			return HTTPResponseJSONData{}, reqBodyErr
		}
	} else {
		body = nil
	}

	if _, ok := reqJSONData.Headers["Content-Type"]; !ok {
		reqJSONData.Headers["Content-Type"] = "application/json"
	}

	reqData := HTTPRequestData{
		URL:                reqJSONData.URL,
		Method:             reqJSONData.Method,
		Headers:            reqJSONData.Headers,
		Body:               body,
		FollowRedirections: reqJSONData.FollowRedirections,
	}

	resp, respErr := SendRequest(reqData)

	if respErr != nil {
		return HTTPResponseJSONData{}, respErr
	}

	var bodyJSON map[string]any

	if len(resp.Body) > 0 {
		respBodyErr := json.Unmarshal(resp.Body, &bodyJSON)

		if respBodyErr != nil {
			return HTTPResponseJSONData{}, respBodyErr
		}
	}

	return HTTPResponseJSONData{
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
		Body:       bodyJSON,
	}, nil
}

// Sends a HTTP request with the provided request data
func SendRequest(reqData HTTPRequestData) (HTTPResponseData, error) {
	body := reqData.Body

	if body == nil {
		body = &bytes.Buffer{}
	}

	req, reqErr := http.NewRequest(reqData.Method, reqData.URL.String(), body)

	if reqErr != nil {
		return HTTPResponseData{}, reqErr
	}

	// Sets the request headers
	for key, value := range reqData.Headers {
		req.Header.Set(key, fmt.Sprintf("%v", value))
	}

	checkRedirect := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	if reqData.FollowRedirections {
		checkRedirect = nil
	}

	// Sends the request
	client := GetHTTPClient(checkRedirect)

	resp, respErr := client.Do(req)

	if respErr != nil {
		return HTTPResponseData{}, respErr
	}

	defer resp.Body.Close()

	// Reads the response body
	respBody, respBodyErr := io.ReadAll(resp.Body)

	if respBodyErr != nil {
		return HTTPResponseData{}, respBodyErr
	}

	// Parses the response headers
	responseHeaders := make(map[string]any)

	for key, value := range resp.Header {
		if len(value) > 0 {
			responseHeaders[key] = value[0]
		}
	}

	return HTTPResponseData{
		StatusCode: resp.StatusCode,
		Headers:    responseHeaders,
		Body:       respBody,
	}, nil
}

func GetHTTPClient(checkRedirect func(req *http.Request, via []*http.Request) error) *http.Client {
	return &http.Client{
		CheckRedirect: checkRedirect,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}
