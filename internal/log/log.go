package log

import (
	"io"
	"log/slog"
	"os"
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
		Level:      LevelDebug,
		TimeFormat: time.Kitchen,
	})
	slog.SetDefault(slog.New(h))
}

func Debug(msg string, args ...any) { slog.Debug(msg, args...) }
func Info(msg string, args ...any)  { slog.Info(msg, args...) }
func Warn(msg string, args ...any)  { slog.Warn(msg, args...) }
func Error(msg string, args ...any) { slog.Error(msg, args...) }

func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
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

	switch w.level {
	case LevelDebug:
		Debug(msg)
	case LevelWarn:
		Warn(msg)
	case LevelError:
		Error(msg)
	default:
		Info(msg)
	}
	return len(p), nil
}
