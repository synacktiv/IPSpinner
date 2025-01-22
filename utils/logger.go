package utils

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// https://stackoverflow.com/questions/76858037/how-to-use-zerolog-to-filter-info-logs-to-stdout-and-error-logs-to-stderr
type SpecificLevelWriter struct {
	io.Writer
	Levels       []zerolog.Level
	DebugVerbose *bool
	TraceVerbose *bool
}

var Logger zerolog.Logger
var runLogFile *os.File

// Closes the log file
func CloseLogFile() error {
	return runLogFile.Close()
}

// Initialises the logger (specific rules and handlers)
func InitializeLogger(verbose1, verbose2 bool) {
	runLogFile, _ := os.OpenFile(
		LogFile,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		fileMode,
	)

	consolePartsToExclude := []string{zerolog.CallerFieldName}

	trueVar := true
	falseVar := false

	multi := zerolog.MultiLevelWriter(
		SpecificLevelWriter{
			Writer:       runLogFile,
			DebugVerbose: &trueVar,
			TraceVerbose: &falseVar,
		},
		SpecificLevelWriter{
			Writer:       zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC822Z, PartsExclude: consolePartsToExclude},
			DebugVerbose: &verbose1,
			TraceVerbose: &verbose2,
		},
	)

	Logger = zerolog.New(multi).With().Caller().Timestamp().Logger()
}

// Handler for the log level
// Does log or not according to the verbose mode
func (w SpecificLevelWriter) WriteLevel(level zerolog.Level, content []byte) (int, error) {
	if level == zerolog.TraceLevel && !*w.TraceVerbose {
		return len(content), nil
	}

	if level == zerolog.DebugLevel && !*w.DebugVerbose {
		return len(content), nil
	}

	return w.Write(content)
}
