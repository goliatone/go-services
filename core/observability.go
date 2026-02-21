package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	goerrors "github.com/goliatone/go-errors"
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
		enrichErrorFields(contextFields, err)
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

func enrichErrorFields(fields map[string]any, err error) {
	if len(fields) == 0 || err == nil {
		return
	}

	var richErr *goerrors.Error
	if !goerrors.As(err, &richErr) || richErr == nil {
		return
	}

	if richErr.Category != "" {
		fields["error_category"] = richErr.Category.String()
	}
	if richErr.Code != 0 {
		fields["error_code"] = richErr.Code
	}
	if strings.TrimSpace(richErr.TextCode) != "" {
		fields["error_text_code"] = strings.TrimSpace(richErr.TextCode)
	}
	fields["error_severity"] = richErr.GetSeverity().String()
	if loc := richErr.GetLocation(); loc != nil {
		fields["error_location"] = loc.String()
	}

	requestID := strings.TrimSpace(richErr.RequestID)
	if requestID == "" {
		requestID = firstNonEmptyString(fields, "request_id", "trace_id")
		if requestID == "" && len(richErr.Metadata) > 0 {
			requestID = firstNonEmptyAny(richErr.Metadata, "request_id", "trace_id")
		}
		if requestID != "" {
			richErr.WithRequestID(requestID)
		}
	}
	if requestID != "" {
		fields["request_id"] = requestID
	}

	if validationErrors := richErr.AllValidationErrors(); len(validationErrors) > 0 {
		fields["error_validation_errors"] = validationErrors
	}

	if len(richErr.Metadata) > 0 {
		fields["error_metadata"] = RedactSensitiveMap(richErr.Metadata)
		if _, ok := fields["trace_id"]; !ok {
			if traceID := firstNonEmptyAny(richErr.Metadata, "trace_id"); traceID != "" {
				fields["trace_id"] = traceID
			}
		}
	}
}

func firstNonEmptyString(fields map[string]any, keys ...string) string {
	if len(fields) == 0 {
		return ""
	}
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(fields[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func firstNonEmptyAny(fields map[string]any, keys ...string) string {
	return firstNonEmptyString(fields, keys...)
}
