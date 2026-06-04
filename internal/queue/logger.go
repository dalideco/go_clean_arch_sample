package queue

import (
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/dali/go_clean_arch_sample/internal/log"
)

// asynqLogger bridges asynq's printf-style Logger interface to internal/log.
// asynq calls Debug/Info/Warn/Error/Fatal with variadic args; we fmt.Sprint
// them once and forward the rendered text as the structured log message.
// Same shape and intent as postgres/gorm_logger.go.
type asynqLogger struct{}

// NewAsynqLogger returns the bridge logger to plug into asynq.Config.
func NewAsynqLogger() asynq.Logger { return asynqLogger{} }

func (asynqLogger) Debug(args ...any) { log.Debug(fmt.Sprint(args...)) }
func (asynqLogger) Info(args ...any)  { log.Info(fmt.Sprint(args...)) }
func (asynqLogger) Warn(args ...any)  { log.Warn(fmt.Sprint(args...)) }
func (asynqLogger) Error(args ...any) { log.Error(fmt.Sprint(args...)) }
func (asynqLogger) Fatal(args ...any) { log.Fatal(fmt.Sprint(args...)) }
