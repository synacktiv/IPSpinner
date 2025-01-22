package utils

import (
	"context"
	"net/url"
)

type Provider interface {
	GetName() string
	SummarizeState() string
	GetAvailableLaunchers() []Launcher
	GetLaunchers() []Launcher
	GetNbTotalReqSent() int
	IsStopped() bool
	Clear() bool
}

type Launcher interface {
	GetName() string
	GetProvider() Provider
	GetNbTotalReqSent() int
	SummarizeState() string
	PreloadHosts(context.Context, []*url.URL)
	SendRequest(context.Context, HTTPRequestData, *AllConfigs) (HTTPResponseData, string, error)
	IsAvailable() bool
	IsStopped() bool
	Clear() bool
}
