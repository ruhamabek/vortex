package logger

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type contextKey string
const CorrelationIDKey contextKey = "correlation_id"
var globalLogger *zap.Logger

func Init(enviroment string)(*zap.Logger, error){
	var cfg zap.Config
	if enviroment == "production" {
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.TimeKey = "timestamp"
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	l, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	globalLogger = l
	return l, nil
}

func L() *zap.Logger{
	if globalLogger == nil {
		l,_ := zap.NewDevelopment()
		return l
	}

	return globalLogger
}

func WithContext(ctx context.Context) *zap.Logger {
	l := L()
	if ctx == nil {
		return l
	}

	if cid, ok := ctx.Value(CorrelationIDKey).(string); ok && cid != ""  {
		return l.With((zap.String("correlation_id", cid)))
	}

	return l
}