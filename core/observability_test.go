package core

import (
	"context"
	"sync"
	"testing"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

type capturedCounter struct {
	name  string
	value int64
	tags  map[string]string
}

type capturedHistogram struct {
	name  string
	value float64
	tags  map[string]string
}

type captureMetricsRecorder struct {
	mu         sync.Mutex
	counters   []capturedCounter
	histograms []capturedHistogram
}

func (m *captureMetricsRecorder) IncCounter(_ context.Context, name string, value int64, tags map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = append(m.counters, capturedCounter{name: name, value: value, tags: cloneTags(tags)})
}

func (m *captureMetricsRecorder) ObserveHistogram(_ context.Context, name string, value float64, tags map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms = append(m.histograms, capturedHistogram{name: name, value: value, tags: cloneTags(tags)})
}

type capturedLog struct {
	level  string
	msg    string
	fields map[string]any
}

type captureLogger struct {
	mu       *sync.Mutex
	records  *[]capturedLog
	defaults map[string]any
}

func newCaptureLogger() *captureLogger {
	records := []capturedLog{}
	return &captureLogger{mu: &sync.Mutex{}, records: &records, defaults: map[string]any{}}
}

func (l *captureLogger) WithFields(fields map[string]any) Logger {
	merged := cloneFieldMap(l.defaults)
	for key, value := range fields {
		merged[key] = value
	}
	return &captureLogger{mu: l.mu, records: l.records, defaults: merged}
}

func (l *captureLogger) Trace(msg string, args ...any) { l.record("trace", msg, args...) }
func (l *captureLogger) Debug(msg string, args ...any) { l.record("debug", msg, args...) }
func (l *captureLogger) Info(msg string, args ...any)  { l.record("info", msg, args...) }
func (l *captureLogger) Warn(msg string, args ...any)  { l.record("warn", msg, args...) }
func (l *captureLogger) Error(msg string, args ...any) { l.record("error", msg, args...) }
func (l *captureLogger) Fatal(msg string, args ...any) { l.record("fatal", msg, args...) }

func (l *captureLogger) WithContext(context.Context) Logger {
	return &captureLogger{mu: l.mu, records: l.records, defaults: cloneFieldMap(l.defaults)}
}

func (l *captureLogger) record(level string, msg string, args ...any) {
	fields := cloneFieldMap(l.defaults)
	for index := 0; index+1 < len(args); index += 2 {
		key, ok := args[index].(string)
		if !ok {
			continue
		}
		fields[key] = args[index+1]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	*l.records = append(*l.records, capturedLog{level: level, msg: msg, fields: fields})
}

func (l *captureLogger) snapshot() []capturedLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	items := *l.records
	out := make([]capturedLog, len(items))
	copy(out, items)
	return out
}

func cloneFieldMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func TestServiceObservability_ConnectSuccess(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	metrics := &captureMetricsRecorder{}
	logger := newCaptureLogger()
	svc, err := NewService(DefaultConfig(),
		WithRegistry(registry),
		WithMetricsRecorder(metrics),
		WithLoggerProvider(stubLoggerProvider{logger: logger}),
		WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.Connect(context.Background(), ConnectRequest{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "user", ID: "usr_1"},
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if !hasCounter(metrics.counters, "services.connect.total", "success") {
		t.Fatalf("expected services.connect.total success counter")
	}
	if !hasHistogram(metrics.histograms, "services.connect.duration_ms", "success") {
		t.Fatalf("expected services.connect.duration_ms histogram")
	}
	if !hasLog(logger.snapshot(), "info", "connect succeeded", "connect") {
		t.Fatalf("expected connect succeeded structured log")
	}
}

func TestServiceObservability_InvokeCapabilityFailure(t *testing.T) {
	metrics := &captureMetricsRecorder{}
	logger := newCaptureLogger()
	svc, err := NewService(DefaultConfig(),
		WithMetricsRecorder(metrics),
		WithLoggerProvider(stubLoggerProvider{logger: logger}),
		WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.InvokeCapability(context.Background(), InvokeCapabilityRequest{
		ProviderID: "missing",
		Scope:      ScopeRef{Type: "user", ID: "usr_1"},
		Capability: "repo.read",
	})
	if err == nil {
		t.Fatalf("expected invoke capability error for missing provider")
	}
	if !hasCounter(metrics.counters, "services.invoke_capability.total", "failure") {
		t.Fatalf("expected invoke capability failure counter")
	}
	if !hasLog(logger.snapshot(), "error", "invoke_capability failed", "invoke_capability") {
		t.Fatalf("expected invoke capability failure log")
	}
}

func TestServiceObservability_EnrichesStructuredErrorFields(t *testing.T) {
	metrics := &captureMetricsRecorder{}
	logger := newCaptureLogger()
	svc, err := NewService(DefaultConfig(),
		WithMetricsRecorder(metrics),
		WithLoggerProvider(stubLoggerProvider{logger: logger}),
		WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	richErr := goerrors.New("provider timeout", goerrors.CategoryExternal).
		WithCode(502).
		WithTextCode(ServiceErrorExternalFailure).
		WithSeverity(goerrors.SeverityCritical).
		WithMetadata(map[string]any{
			"trace_id":     "trace_123",
			"request_id":   "req_123",
			"refresh_token": "secret_refresh_token",
		})
	svc.observeOperation(
		context.Background(),
		time.Now().UTC().Add(-100*time.Millisecond),
		"provider_operation",
		richErr,
		map[string]any{"provider_id": "github"},
	)

	records := logger.snapshot()
	if len(records) == 0 {
		t.Fatalf("expected logs to be emitted")
	}
	last := records[len(records)-1]
	if last.fields["error_category"] != "external" {
		t.Fatalf("expected error_category external, got %#v", last.fields["error_category"])
	}
	if last.fields["error_text_code"] != ServiceErrorExternalFailure {
		t.Fatalf("expected error_text_code %q, got %#v", ServiceErrorExternalFailure, last.fields["error_text_code"])
	}
	if last.fields["error_severity"] != goerrors.SeverityCritical.String() {
		t.Fatalf("expected critical severity, got %#v", last.fields["error_severity"])
	}
	if last.fields["request_id"] != "req_123" {
		t.Fatalf("expected request_id propagation, got %#v", last.fields["request_id"])
	}
	if last.fields["trace_id"] != "trace_123" {
		t.Fatalf("expected trace_id propagation, got %#v", last.fields["trace_id"])
	}

	metadata, ok := last.fields["error_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected redacted error_metadata map, got %#v", last.fields["error_metadata"])
	}
	if metadata["refresh_token"] != RedactedValue {
		t.Fatalf("expected refresh_token to be redacted, got %#v", metadata["refresh_token"])
	}
}

func hasCounter(items []capturedCounter, name string, status string) bool {
	for _, item := range items {
		if item.name == name && item.tags["status"] == status {
			return true
		}
	}
	return false
}

func hasHistogram(items []capturedHistogram, name string, status string) bool {
	for _, item := range items {
		if item.name == name && item.tags["status"] == status {
			return true
		}
	}
	return false
}

func hasLog(items []capturedLog, level string, message string, eventType string) bool {
	for _, item := range items {
		if item.level != level {
			continue
		}
		if item.msg != message {
			continue
		}
		if item.fields["event_type"] == eventType {
			return true
		}
	}
	return false
}
