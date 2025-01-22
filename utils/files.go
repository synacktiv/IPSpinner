package utils

import (
	"fmt"
	"net/url"
	"os"
	"os/user"
	"runtime"
	"strings"
)

const fileMode = 0600 //nolint:gocritic

// Reads the content of the given file
func ReadFileContent(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)

	if err != nil {
		return nil, err
	}

	return content, nil
}

// Write the given content into the given file
func WriteFileContent(filePath string, content []byte) error {
	err := os.WriteFile(filePath, content, fileMode)

	return err
}

// ReadFileLines reads the content of the given file and returns each line as a string slice
func ReadFileLines(filePath string) ([]string, error) {
	content, err := ReadFileContent(filePath)

	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")

	// Remove empty lines if any
	var cleanLines []string

	for _, line := range lines {
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	return cleanLines, nil
}

// Parses a host file (a text file where each line contains a domain name or a URL)
func ParseHostsFile(path string) []*url.URL {
	if path == "" {
		return []*url.URL{}
	}

	hostsStr, hostsStrErr := ReadFileLines(path)

	if hostsStrErr != nil {
		Logger.Warn().Err(hostsStrErr).Msg("Can not read preload hosts file.")

		return []*url.URL{}
	}

	hostLst := []*url.URL{}

	for _, hostStr := range hostsStr {
		// If this is only a domain name => it loads http and https endpoints
		if !strings.HasPrefix(hostStr, "http://") && !strings.HasPrefix(hostStr, "https://") {
			for _, prefix := range []string{"http://", "https://"} {
				newHostStr := prefix + hostStr
				host, hostErr := url.Parse(newHostStr)

				if hostErr != nil {
					Logger.Warn().Err(hostsStrErr).Str("host", newHostStr).Msg("Host can not be parsed as a valid URL.")

					continue
				}

				hostLst = append(hostLst, host)
			}
		} else {
			host, hostErr := url.Parse(hostStr)

			if hostErr != nil {
				Logger.Warn().Err(hostsStrErr).Str("host", hostStr).Msg("Host can not be parsed as a valid URL.")

				continue
			}

			hostLst = append(hostLst, host)
		}
	}

	return hostLst
}

func GetHomeDirectory() (string, error) {
	// Essayez d'abord d'utiliser os/user pour obtenir le répertoire home
	usr, err := user.Current()
	if err == nil {
		return usr.HomeDir, nil
	}

	// Si os/user échoue, utilisez les variables d'environnement
	if runtime.GOOS == "windows" {
		home := os.Getenv("USERPROFILE")

		if home != "" {
			return home, nil
		}
	} else {
		home := os.Getenv("HOME")

		if home != "" {
			return home, nil
		}
	}

	return "", fmt.Errorf("cannot retrieve the user home directory")
}

func FileExists(path string) bool {
	_, err := os.Stat(path)

	return !os.IsNotExist(err)
}
