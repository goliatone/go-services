package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Service) observeOperation(
	ctx context.Context,
	startedAt time.Time,
	operation string,
	err error,
	fields map[string]any,
) {
	if s == nil {
		return
	}
	operation = normalizeOperation(operation)
	if operation == "" {
		operation = "unknown"
	}
	status := "success"
	if err != nil {
		status = "failure"
	}

	contextFields := cloneFields(fields)
	contextFields["event_type"] = operation
	contextFields["status"] = status
	contextFields["duration_ms"] = time.Since(startedAt).Milliseconds()
	if err != nil {
		contextFields["error"] = err.Error()
	}

	tags := map[string]string{
		"operation": operation,
		"status":    status,
	}
	for _, key := range []string{"provider_id", "scope_type", "scope_id", "connection_id"} {
		if value := strings.TrimSpace(fmt.Sprint(contextFields[key])); value != "" && value != "<nil>" {
			tags[key] = value
		}
	}

	s.recordCounter(ctx, "services."+operation+".total", 1, tags)
	s.recordHistogram(ctx, "services."+operation+".duration_ms", float64(time.Since(startedAt).Milliseconds()), tags)

	if err != nil {
		s.logError(ctx, operation+" failed", contextFields)
		return
	}
	s.logInfo(ctx, operation+" succeeded", contextFields)
}

func (s *Service) logInfo(ctx context.Context, message string, fields map[string]any) {
	s.logWithLevel(ctx, "info", message, fields)
}

func (s *Service) logError(ctx context.Context, message string, fields map[string]any) {
	s.logWithLevel(ctx, "error", message, fields)
}

func (s *Service) logWithLevel(ctx context.Context, level string, message string, fields map[string]any) {
	if s == nil || s.logger == nil {
		return
	}
	logger := s.logger
	if ctx != nil {
		logger = logger.WithContext(ctx)
	}
	if fieldsLogger, ok := logger.(FieldsLogger); ok {
		logger = fieldsLogger.WithFields(cloneFields(fields))
	}
	args := flattenFields(fields)
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		logger.Error(message, args...)
	default:
		logger.Info(message, args...)
	}
}

func (s *Service) recordCounter(ctx context.Context, name string, value int64, tags map[string]string) {
	if s == nil || s.metricsRecorder == nil {
		return
	}
	s.metricsRecorder.IncCounter(ctx, strings.TrimSpace(name), value, cloneTags(tags))
}

func (s *Service) recordHistogram(ctx context.Context, name string, value float64, tags map[string]string) {
	if s == nil || s.metricsRecorder == nil {
		return
	}
	s.metricsRecorder.ObserveHistogram(ctx, strings.TrimSpace(name), value, cloneTags(tags))
}

func cloneFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return map[string]any{}
	}
	copied := make(map[string]any, len(fields))
	for key, value := range fields {
		copied[key] = value
	}
	return copied
}

func flattenFields(fields map[string]any) []any {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, key, fields[key])
	}
	return args
}

func normalizeOperation(operation string) string {
	operation = strings.TrimSpace(strings.ToLower(operation))
	operation = strings.ReplaceAll(operation, " ", "_")
	operation = strings.ReplaceAll(operation, "-", "_")
	return operation
}
