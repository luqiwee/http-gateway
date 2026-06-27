package log

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Field = zap.Field

type AccessEvent struct {
	Route      string
	Method     string
	Path       string
	Status     int
	Duration   time.Duration
	RemoteAddr string
	Upstream   string
}

type standardLogger struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
	writer *asyncWriteSyncer
}

var std = newStandardLogger(os.Stdout)

func Debugf(format string, args ...any) {
	std.sugar.Debugf(format, args...)
}

func Infof(format string, args ...any) {
	std.sugar.Infof(format, args...)
}

func Warnf(format string, args ...any) {
	std.sugar.Warnf(format, args...)
}

func Errorf(format string, args ...any) {
	std.sugar.Errorf(format, args...)
}

func Fatalf(format string, args ...any) {
	std.sugar.Errorf(format, args...)
	_ = Sync()
	os.Exit(1)
}

func Info(message string, fields ...Field) {
	std.logger.Info(message, fields...)
}

func Error(message string, fields ...Field) {
	std.logger.Error(message, fields...)
}

func Access(event AccessEvent) {
	Info("access",
		String("route", event.Route),
		String("method", event.Method),
		String("path", event.Path),
		Int("status", event.Status),
		Int64("duration_ms", event.Duration.Milliseconds()),
		String("remote_addr", event.RemoteAddr),
		String("upstream", event.Upstream),
	)
}

func Sync() error {
	return std.logger.Sync()
}

func Dropped() uint64 {
	return std.writer.Dropped()
}

func String(key string, value string) Field {
	return zap.String(key, value)
}

func Int(key string, value int) Field {
	return zap.Int(key, value)
}

func Int64(key string, value int64) Field {
	return zap.Int64(key, value)
}

func ErrorField(err error) Field {
	return zap.Error(err)
}

func newStandardLogger(output *os.File) *standardLogger {
	writer := newAsyncWriteSyncer(output, defaultAsyncBufferSize)
	encoderConfig := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		writer,
		zap.NewAtomicLevelAt(zap.InfoLevel),
	)
	logger := zap.New(core, zap.ErrorOutput(zapcore.AddSync(os.Stderr)))
	return &standardLogger{
		logger: logger,
		sugar:  logger.Sugar(),
		writer: writer,
	}
}
