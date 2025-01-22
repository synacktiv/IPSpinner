package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"ipspinner/providers"
	"ipspinner/utils"
	"net/http"
	"strconv"
	"time"

	"github.com/elazarl/goproxy"
)

// Setups the proxy certificate authority
func setupProxyCA(caCert, caKey []byte) error {
	goproxyCa, err := tls.X509KeyPair(caCert, caKey)

	if err != nil {
		return err
	}

	if goproxyCa.Leaf, err = x509.ParseCertificate(goproxyCa.Certificate[0]); err != nil {
		return err
	}

	goproxy.GoproxyCa = goproxyCa
	goproxy.OkConnect = &goproxy.ConnectAction{Action: goproxy.ConnectAccept, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	goproxy.MitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	goproxy.HTTPMitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectHTTPMitm, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	goproxy.RejectConnect = &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}

	return nil
}

// Retrieves or generates CA certificates for the proxy
func prepareProxyCertificates(allConfigs *utils.AllConfigs) error {
	var caCert []byte

	var caKey []byte

	// If no CA certificate and key have been given
	if allConfigs.ProxyConfig.CaCertFile == "" || allConfigs.ProxyConfig.CaCertKeyFile == "" {
		var caErr error

		// It generates a random self-signed CA certificate and its key
		utils.Logger.Info().Msg("Generating a new CA certificate.")

		caCert, caKey, caErr = utils.GenerateRSACACertificate()

		if caErr != nil {
			return caErr
		}
	} else { // Otherwise, it tries to read the provided certicate and key
		var caCertErr error
		var caKeyErr error

		utils.Logger.Info().Str("caCertPath", allConfigs.ProxyConfig.CaCertFile).Str("caCertKeyPath", allConfigs.ProxyConfig.CaCertKeyFile).Msg("Retrieving the provided CA certificate.")

		caCert, caCertErr = utils.ReadFileContent(allConfigs.ProxyConfig.CaCertFile)

		if caCertErr != nil {
			return caCertErr
		}

		caKey, caKeyErr = utils.ReadFileContent(allConfigs.ProxyConfig.CaCertKeyFile)

		if caKeyErr != nil {
			return caKeyErr
		}
	}

	// It setups the certificate and key on the proxy
	err := setupProxyCA(caCert, caKey)

	if err != nil {
		return err
	}

	// If the user has asked for exporting the certificate and the key
	if allConfigs.CommandParameters.ExportCaCert {
		exportedCertName := "ipspinner-ca-cert.pem"
		exportedCertKeyName := "ipspinner-ca-cert-key.pem"

		utils.Logger.Info().Str("exportedCaCertPath", exportedCertName).Str("exportedCaCertKeyPath", exportedCertKeyName).Msg("Exporting the CA certificate.")

		caCertErr := utils.WriteFileContent(exportedCertName, caCert)

		if caCertErr != nil {
			return caCertErr
		}

		caCertKeyErr := utils.WriteFileContent(exportedCertKeyName, caKey)

		if caCertKeyErr != nil {
			return caCertKeyErr
		}
	}

	return nil
}

// Returns the OnRequest handler
func generateOnRequestHandler(ctx context.Context, providersLst []utils.Provider, allConfigs *utils.AllConfigs) func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	requestCount := 0

	whitelistHosts := utils.ParseHostsFile(allConfigs.ProxyConfig.WhitelistHostsFile)
	blacklistHosts := utils.ParseHostsFile(allConfigs.ProxyConfig.BlacklistHostsFile)

	if len(whitelistHosts) > 0 {
		utils.Logger.Info().Int("nbHostsInWhitelist", len(whitelistHosts)).Msg("Hosts mentioned in the whitelist have been loaded.")

		if len(blacklistHosts) > 0 {
			utils.Logger.Warn().Msg("The blacklist has been ignored because a whitelist has been given.")
		}
	} else if len(blacklistHosts) > 0 {
		utils.Logger.Info().Int("nbHostsInBlacklist", len(blacklistHosts)).Msg("Hosts mentioned in the blacklist have been loaded.")
	}

	userAgents := []string{}

	if len(allConfigs.ProxyConfig.UserAgentsFile) > 0 {
		userAgentsLines, userAgentsErr := utils.ReadFileLines(allConfigs.ProxyConfig.UserAgentsFile)

		if userAgentsErr != nil {
			utils.Logger.Warn().Err(userAgentsErr).Msg("Can not read user agents file.")
		}

		userAgents = userAgentsLines
	}

	return func(req *http.Request, proxyCtx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if req == nil || req.URL == nil {
			return req, nil
		}

		// If the whitelist is set
		if len(whitelistHosts) > 0 {
			if !utils.DoesURLListContainsBaseURL(whitelistHosts, req.URL) { // Checks if the request URL is not in the whitelist
				utils.Logger.Warn().Str("host", req.URL.String()).Msg("Can not send a request to this host because it is not mentioned in the whitelist.")

				return nil, goproxy.NewResponse(req,
					goproxy.ContentTypeText,
					http.StatusBadGateway,
					"PROXY ERROR: Can not send a request to this host because it is not mentioned in the whitelist.")
			}
		} else if len(blacklistHosts) > 0 { // if the blacklist is set
			if utils.DoesURLListContainsBaseURL(blacklistHosts, req.URL) { // Checks if the request URL is in the blacklist
				utils.Logger.Warn().Str("host", req.URL.String()).Msg("Can not send a request to this host because it is mentioned in the blacklist.")

				return nil, goproxy.NewResponse(req,
					goproxy.ContentTypeText,
					http.StatusBadGateway,
					"PROXY ERROR: Can not send a request to this host because it is mentioned in the blacklist.")
			}
		}

		// Waits for an available launcher
		var maxRep = 1000 / 100 * allConfigs.ProxyConfig.WaitForLauncherAvailableTimeout // max WaitForLauncherAvailable seconds

		nbRep := 0

		for len(providers.GetAllAvailableLaunchers(providersLst)) == 0 {
			nbRep++

			if nbRep >= maxRep {
				return nil, goproxy.NewResponse(req,
					goproxy.ContentTypeText,
					http.StatusBadGateway,
					"PROXY ERROR: Timeout - no launcher seem to be available.")
			}

			time.Sleep(100 * time.Millisecond) //nolint:gomnd
		}

		// It takes a random launcher among all available launchers
		launcher := utils.RandomElementInSlice(providers.GetAllAvailableLaunchers(providersLst))

		headers := map[string]any{}

		for name, headersLst := range req.Header {
			for _, h := range headersLst {
				headers[name] = h
			}
		}

		if len(userAgents) > 0 {
			headers["User-Agent"] = utils.RandomElementInSlice(userAgents)
		}

		body, bodyErr := io.ReadAll(req.Body)

		if bodyErr != nil {
			utils.Logger.Error().Err(bodyErr).Msg("Error reading request body.")

			return nil, goproxy.NewResponse(req,
				goproxy.ContentTypeText,
				http.StatusBadGateway,
				"PROXY ERROR: Error reading request body: "+bodyErr.Error())
		}

		respData, respHeaderCustom, respDataErr := (*launcher).SendRequest(ctx, utils.HTTPRequestData{
			URL:     req.URL,
			Method:  req.Method,
			Headers: headers,
			Body:    bytes.NewBuffer(body),
		}, allConfigs)

		if respDataErr != nil {
			utils.Logger.Error().Err(respDataErr).Msg("Error while processing request.")

			return nil, goproxy.NewResponse(req,
				goproxy.ContentTypeText,
				http.StatusBadGateway,
				"PROXY ERROR: Error while processing request: "+respDataErr.Error())
		}

		resp := goproxy.NewResponse(req,
			utils.GetOrDefault(respData.Headers, "Content-Type", goproxy.ContentTypeText).(string),
			respData.StatusCode,
			string(respData.Body))

		for headerName, headerVal := range respData.Headers {
			resp.Header.Set(headerName, headerVal.(string))
		}

		if allConfigs.ProxyConfig.DebugResponseHeaders {
			resp.Header.Set(utils.IPSpinnerResponseHeaderPrefix+"Provider", (*launcher).GetProvider().GetName())
			resp.Header.Set(utils.IPSpinnerResponseHeaderPrefix+"Launcher", (*launcher).GetName())
			resp.Header.Set(utils.IPSpinnerResponseHeaderPrefix+"Provider-NbTotalReqSent", strconv.Itoa((*launcher).GetProvider().GetNbTotalReqSent()))
			resp.Header.Set(utils.IPSpinnerResponseHeaderPrefix+"Launcher-NbTotalReqSent", strconv.Itoa((*launcher).GetNbTotalReqSent()))

			if respHeaderCustom != "" {
				resp.Header.Set(utils.IPSpinnerResponseHeaderPrefix+"Launcher-Custom", respHeaderCustom)
			}
		}

		requestCount++

		utils.Logger.Trace().Str("from", req.URL.String()).Str("to-provider", (*launcher).GetProvider().GetName()).Str("to-launcher", (*launcher).GetName()).Str("method", req.Method).Msg(fmt.Sprintf("Redirecting request #%d.", requestCount))

		return req, resp
	}
}

// Creates a proxy instance
func CreateProxy(ctx context.Context, allConfigs *utils.AllConfigs, providersLst []utils.Provider) (*goproxy.ProxyHttpServer, error) {
	prepareErr := prepareProxyCertificates(allConfigs)

	if prepareErr != nil {
		return nil, prepareErr
	}

	// It instantiates the goproxy instance
	proxy := goproxy.NewProxyHttpServer()

	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(generateOnRequestHandler(ctx, providersLst, allConfigs)) //nolint:bodyclose

	proxy.Verbose = allConfigs.CommandParameters.Verbose3

	return proxy, nil
}
