package utils

import (
	"fmt"
	"net/url"
	"strings"
)

// A base URL is the [SCHEME]://[DOMAIN]:[PORT]
func GetBaseURL(urlObj *url.URL) string {
	scheme := strings.ToLower(urlObj.Scheme)
	host := strings.ToLower(urlObj.Host)

	if scheme == "" {
		scheme = "https"
	}

	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	port := urlObj.Port()

	if port == "" {
		if scheme == "https" {
			baseURL += ":443"
		} else {
			baseURL += ":80"
		}
	}

	return baseURL
}

// A path corresponds to the part after the host part
// Ex: the path for "https://google.com/search/test" is "/search/test"
func GetPathFromURL(urlObj *url.URL) string {
	schemeLength := 0

	if len(urlObj.Scheme) > 0 {
		schemeLength = len(urlObj.Scheme) + 3
	}

	prefixLength := schemeLength + len(urlObj.Host)

	minIndex := min(len(urlObj.String()), prefixLength)

	return urlObj.String()[minIndex:]
}

// Generates a random string with lowercase letters
func GenerateRandomPrefix(size int) string {
	const lowercaseLetters = "abcdefghijklmnopqrstuvwxyz"

	prefix := make([]byte, size)

	for i := 0; i < size; i++ {
		prefix[i] = lowercaseLetters[generateSecureRandomInt(len(lowercaseLetters))]
	}

	return string(prefix)
}

func CompareBaseURLs(url1, url2 *url.URL) bool {
	return GetBaseURL(url1) == GetBaseURL(url2)
}

func DoesURLListContainsBaseURL(urls []*url.URL, url1 *url.URL) bool {
	for _, url2 := range urls {
		if CompareBaseURLs(url1, url2) {
			return true
		}
	}

	return false
}
