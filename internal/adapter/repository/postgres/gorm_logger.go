package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dali/go_project_sample/internal/log"
)

// gormLogger bridges gorm's printf-style logger.Interface to internal/log.
// gorm passes Info/Warn/Error a format string + args; we interpolate once
// here and forward the rendered message as the structured log msg. SQL is
// emitted via Trace below with proper key/value fields.
type gormLogger struct{}

func newGormLogger() logger.Interface { return &gormLogger{} }

func (l *gormLogger) LogMode(logger.LogLevel) logger.Interface { return l }

func (l *gormLogger) Info(_ context.Context, format string, args ...any) {
	log.Info(fmt.Sprintf(format, args...))
}

func (l *gormLogger) Warn(_ context.Context, format string, args ...any) {
	log.Warn(fmt.Sprintf(format, args...))
}

func (l *gormLogger) Error(_ context.Context, format string, args ...any) {
	log.Error(fmt.Sprintf(format, args...))
}

func (l *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		log.Error("sql error", "err", err, "elapsed", elapsed, "rows", rows, "sql", sql)
	case elapsed > 200*time.Millisecond:
		log.Warn("slow sql", "elapsed", elapsed, "rows", rows, "sql", sql)
	default:
		log.Debug("sql", "elapsed", elapsed, "rows", rows, "sql", sql)
	}
}
