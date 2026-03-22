package logger

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const RequestID = "RequestID"

type Logger struct {
	l *zap.Logger
}

func New(cfgLog *zap.Config) (Logger, error) {
	cfgLog.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfgLog.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	logger, err := cfgLog.Build()
	if err != nil {
		return Logger{}, fmt.Errorf("create logger: %w", err)
	}
	return Logger{l: logger}, nil
}

func (l *Logger) addCtx(ctx context.Context, fields []zap.Field) []zap.Field {
	if v := ctx.Value(RequestID); v != nil {
		fields = append(fields, zap.String(RequestID, v.(string)))
	}
	return fields
}

func (l *Logger) Info(ctx context.Context, msg string, f ...zap.Field)  { l.l.Info(msg, l.addCtx(ctx, f)...) }
func (l *Logger) Warn(ctx context.Context, msg string, f ...zap.Field)  { l.l.Warn(msg, l.addCtx(ctx, f)...) }
func (l *Logger) Error(ctx context.Context, msg string, f ...zap.Field) { l.l.Error(msg, l.addCtx(ctx, f)...) }
func (l *Logger) Fatal(ctx context.Context, msg string, f ...zap.Field) { l.l.Fatal(msg, l.addCtx(ctx, f)...) }
