package output

import (
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func NewLogger(level int, displayLogs bool) logr.Logger {
	logLevel := GetLevel(level)
	var loggerLevel zapcore.Level

	// covert our verboseLevel to zap logLevel
	switch logLevel {
	case InfoLevel:
		loggerLevel = zapcore.InfoLevel
	case DebugLevel:
		loggerLevel = zapcore.DebugLevel
	case WarnLevel:
		loggerLevel = zapcore.WarnLevel
	case DebugErrorLevel, ErrorLevel:
		loggerLevel = zapcore.ErrorLevel
	case PanicLevel:
		loggerLevel = zapcore.PanicLevel
	default:
		loggerLevel = zapcore.InfoLevel
	}

	out := os.Stderr
	if displayLogs {
		out = os.Stdout
	}
	return zap.New(zap.UseDevMode(false), zap.WriteTo(out), zap.Level(loggerLevel))
}

func GetLevel(level int) VerboseLevel {
	logLevel := InfoLevel
	if level > int(MaxLevel) || level < int(MinLevel) {
		return logLevel
	}
	return VerboseLevel(level)
}
