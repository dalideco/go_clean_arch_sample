package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

func init() {
	h := tint.NewHandler(os.Stderr, &tint.Options{
		AddSource:  true,
		Level:      LevelDebug,
		TimeFormat: time.Kitchen,
	})
	slog.SetDefault(slog.New(h))
}

func Debug(msg string, args ...any) { logAt(2, slog.LevelDebug, msg, args...) }
func Info(msg string, args ...any)  { logAt(2, slog.LevelInfo, msg, args...) }
func Warn(msg string, args ...any)  { logAt(2, slog.LevelWarn, msg, args...) }
func Error(msg string, args ...any) { logAt(2, slog.LevelError, msg, args...) }

func Fatal(msg string, args ...any) {
	logAt(2, slog.LevelError, msg, args...)
	os.Exit(1)
}

// logAt records a log entry attributed to the call site `skip` frames above
// logAt itself. skip=1 → direct caller of logAt; skip=2 → caller's caller
// (i.e., the user code that invoked the public wrapper).
func logAt(skip int, level slog.Level, msg string, args ...any) {
	ctx := context.Background()
	logger := slog.Default()
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(skip+1, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = logger.Handler().Handle(ctx, r)
}

// Writer returns an io.Writer that forwards each Write call to the logger at
// the given level. Useful for adapting libraries that take an io.Writer (e.g.
// gin.DefaultWriter) into our structured logger.
func Writer(level Level) io.Writer {
	return slogWriter{level: level}
}

type slogWriter struct {
	level Level
}

func (w slogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	msg = strings.TrimPrefix(msg, "[GIN-debug] ")
	msg = strings.TrimPrefix(msg, "[GIN] ")
	logAt(1, w.level, msg)
	return len(p), nil
}
