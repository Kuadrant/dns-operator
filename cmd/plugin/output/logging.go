package output

import (
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type VerboseLevel int8

const (
	DebugLevel VerboseLevel = iota - 1
	InfoLevel
	WarnLevel
	ErrorLevel
	PanicLevel

	MinLevel     = DebugLevel
	MaxLevel     = PanicLevel
	DefaultLevel = int(MaxLevel)
)

func NewLogger(level int) logr.Logger {
	logLevel := InfoLevel
	if level <= int(MaxLevel) && level >= int(MinLevel) {
		logLevel = VerboseLevel(level)
	}

	var loggerLevel zapcore.Level

	// covert our verboseLevel to zap logLevel
	switch logLevel {
	case InfoLevel:
		loggerLevel = zapcore.InfoLevel
	case DebugLevel:
		loggerLevel = zapcore.DebugLevel
	case WarnLevel:
		loggerLevel = zapcore.WarnLevel
	case ErrorLevel:
		loggerLevel = zapcore.ErrorLevel
	case PanicLevel:
		loggerLevel = zapcore.PanicLevel
	default:
		loggerLevel = zapcore.InfoLevel
	}
	return zap.New(zap.UseDevMode(false), zap.WriteTo(os.Stderr), zap.Level(loggerLevel))
}
