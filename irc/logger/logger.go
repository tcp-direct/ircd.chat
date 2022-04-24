// Copyright (c) 2017 Daniel Oaks <daniel@danieloaks.net>
// released under the MIT license

package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

var zl *zerolog.Logger

// Level represents the level to log messages at.
type Level int

const (
	// LogDebug represents debug messages.
	LogDebug Level = iota
	// LogInfo represents informational messages.
	LogInfo
	// LogWarning represents warnings.
	LogWarning
	// LogError represents errors.
	LogError
)

var zlMap = map[Level]zerolog.Level{
	LogDebug:   zerolog.DebugLevel,
	LogInfo:    zerolog.InfoLevel,
	LogWarning: zerolog.WarnLevel,
	LogError:   zerolog.ErrorLevel,
}

var (
	// LogLevelNames takes a config name and gives the real log level.
	LogLevelNames = map[string]Level{
		"debug":    LogDebug,
		"info":     LogInfo,
		"warn":     LogWarning,
		"warning":  LogWarning,
		"warnings": LogWarning,
		"error":    LogError,
		"errors":   LogError,
	}
	// LogLevelDisplayNames gives the display name to use for our log levels.
	LogLevelDisplayNames = map[Level]string{
		LogDebug:   "debug",
		LogInfo:    "info",
		LogWarning: "warn",
		LogError:   "error",
	}

	// these are old names for log types that might still appear in yaml configs,
	// but have been replaced in the code. this is used for canonicalization when
	// loading configs, but not during logging.
	typeAliases = map[string]string{
		"localconnect":    "connect",
		"localconnect-ip": "connect-ip",
	}
)

func resolveTypeAlias(typeName string) (result string) {
	if canonicalized, ok := typeAliases[typeName]; ok {
		return canonicalized
	}
	return typeName
}

// Manager is the main interface used to log debug/info/error messages.
type Manager struct {
	configMutex     sync.RWMutex
	loggers         []singleLogger
	stdoutWriteLock sync.Mutex // use one lock for both stdout and stderr
	fileWriteLock   sync.Mutex
	loggingRawIO    uint32
}

// LoggingConfig represents the configuration of a single logger.
type LoggingConfig struct {
	Method        string
	MethodStdout  bool
	MethodStderr  bool
	MethodFile    bool
	Filename      string
	TypeString    string   `yaml:"type"`
	Types         []string `yaml:"real-types"`
	ExcludedTypes []string `yaml:"real-excluded-types"`
	LevelString   string   `yaml:"level"`
	Level         Level    `yaml:"level-real"`
}

// NewManager returns a new log manager.
func NewManager(config []LoggingConfig) (*Manager, error) {
	var logger Manager

	if err := logger.ApplyConfig(config); err != nil {
		return nil, err
	}

	return &logger, nil
}

// ApplyConfig applies the given config to this logger (rehashes the config, in other words).
func (logger *Manager) ApplyConfig(config []LoggingConfig) error {
	logger.configMutex.Lock()
	defer logger.configMutex.Unlock()

	for _, logger := range logger.loggers {
		logger.Close()
	}

	logger.loggers = nil
	atomic.StoreUint32(&logger.loggingRawIO, 0)

	// for safety, this deep-copies all mutable data in `config`
	// XXX let's keep it that way
	var lastErr error
	for _, logConfig := range config {
		typeMap := make(map[string]bool)
		for _, name := range logConfig.Types {
			typeMap[resolveTypeAlias(name)] = true
		}
		excludedTypeMap := make(map[string]bool)
		for _, name := range logConfig.ExcludedTypes {
			excludedTypeMap[resolveTypeAlias(name)] = true
		}

		sLogger := singleLogger{
			MethodSTDOUT: logConfig.MethodStdout,
			MethodSTDERR: logConfig.MethodStderr,
			MethodFile: fileMethod{
				Enabled:  logConfig.MethodFile,
				Filename: logConfig.Filename,
			},
			Level:           logConfig.Level,
			Types:           typeMap,
			ExcludedTypes:   excludedTypeMap,
			stdoutWriteLock: &logger.stdoutWriteLock,
			fileWriteLock:   &logger.fileWriteLock,
		}
		ioEnabled := typeMap["userinput"] || typeMap["useroutput"] || (typeMap["*"] && !(excludedTypeMap["userinput"] && excludedTypeMap["useroutput"]))
		// raw I/O is only logged at level debug;
		if ioEnabled && logConfig.Level == LogDebug {
			atomic.StoreUint32(&logger.loggingRawIO, 1)
		}

		var outs = []io.Writer{zerolog.NewConsoleWriter()}

		if sLogger.MethodFile.Enabled {
			file, err := os.OpenFile(sLogger.MethodFile.Filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
			if err != nil {
				lastErr = fmt.Errorf("Could not open log file %s [%s]", sLogger.MethodFile.Filename, err.Error())
			}
			outs = append(outs, file)
			sLogger.MethodFile.File = file
		}
		zw := zerolog.MultiLevelWriter(outs...)
		bzw := bufio.NewWriter(zw)
		z := zerolog.New(zw)
		sLogger.ZL = &z
		sLogger.MethodFile.Writer = bzw
		logger.loggers = append(logger.loggers, sLogger)
	}

	return lastErr
}

// IsLoggingRawIO returns true if raw user input and output is being logged.
func (logger *Manager) IsLoggingRawIO() bool {
	return atomic.LoadUint32(&logger.loggingRawIO) == 1
}

// Log logs the given message with the given details.
func (logger *Manager) Log(level Level, logType string, messageParts ...string) {
	logger.configMutex.RLock()
	defer logger.configMutex.RUnlock()

	for _, singleLogger := range logger.loggers {
		singleLogger.Log(level, logType, messageParts...)
	}
}

// Debug logs the given message as a debug message.
func (logger *Manager) Debug(logType string, messageParts ...string) {
	logger.Log(LogDebug, logType, messageParts...)
}

// Info logs the given message as an info message.
func (logger *Manager) Info(logType string, messageParts ...string) {
	logger.Log(LogInfo, logType, messageParts...)
}

// Warning logs the given message as a warning message.
func (logger *Manager) Warning(logType string, messageParts ...string) {
	logger.Log(LogWarning, logType, messageParts...)
}

// Error logs the given message as an error message.
func (logger *Manager) Error(logType string, messageParts ...string) {
	logger.Log(LogError, logType, messageParts...)
}

type fileMethod struct {
	Enabled  bool
	Filename string
	File     *os.File
	Writer   *bufio.Writer
}

// singleLogger represents a single logger instance.
type singleLogger struct {
	stdoutWriteLock *sync.Mutex
	fileWriteLock   *sync.Mutex
	MethodSTDOUT    bool
	MethodSTDERR    bool
	MethodFile      fileMethod
	Level           Level
	Types           map[string]bool
	ExcludedTypes   map[string]bool
	ZL              *zerolog.Logger
}

func (logger *singleLogger) Close() error {
	if logger.MethodFile.Enabled {
		flushErr := logger.MethodFile.Writer.Flush()
		closeErr := logger.MethodFile.File.Close()
		if flushErr != nil {
			return flushErr
		}
		return closeErr
	}
	return nil
}

// Log logs the given message with the given details.
func (logger *singleLogger) Log(level Level, logType string, messageParts ...string) {
	// no logging enabled
	if !(logger.MethodSTDOUT || logger.MethodSTDERR || logger.MethodFile.Enabled) {
		return
	}

	// ensure we're logging to the given level
	if level < logger.Level {
		return
	}

	// ensure we're capturing this logType
	capturing := (logger.Types["*"] || logger.Types[logType]) && !logger.ExcludedTypes["*"] && !logger.ExcludedTypes[logType]
	if !capturing {
		return
	}

	// assemble full line

	var rawBuf bytes.Buffer
	// XXX magic number here: 10 is len("connect-ip"), the longest log category name
	// in current use. it's not a big deal if this number gets out of date.

	// output
	if logger.MethodSTDOUT {
		switch level {
		case LogInfo:
			logger.ZL.Info().Strs("parts", messageParts).Msg(logType)
		case LogWarning:
			logger.ZL.Warn().Strs("parts", messageParts).Msg(logType)
		case LogError:
			logger.ZL.Error().Strs("parts", messageParts).Msg(logType)
		case LogDebug:
			logger.ZL.Debug().Strs("parts", messageParts).Msg(logType)
		}
	}
	if logger.MethodSTDERR {
		logger.stdoutWriteLock.Lock()
		os.Stderr.Write(rawBuf.Bytes())
		logger.stdoutWriteLock.Unlock()
	}
	if logger.MethodFile.Enabled {
		logger.fileWriteLock.Lock()
		logger.MethodFile.Writer.Write(rawBuf.Bytes())
		logger.MethodFile.Writer.Flush()
		logger.fileWriteLock.Unlock()
	}
}
