package common

import (
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func NewLogger(verboseness int) logr.Logger {
	var level zapcore.Level

	switch verboseness {
	case 1:
		level = zapcore.InfoLevel
	case 2:
		level = zapcore.DebugLevel
	default:
		level = zapcore.ErrorLevel
	}

	return zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr), zap.Level(level))
}
