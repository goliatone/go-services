package gologger

import (
	"context"
	"testing"

	glog "github.com/goliatone/go-logger/glog"
)

func TestResolveDeterministicFallback(t *testing.T) {
	loggerOnly := &capturingLogger{id: "logger"}
	providerLogger := &capturingLogger{id: "provider"}
	provider := &capturingProvider{logger: providerLogger}

	var resolvedProvider glog.LoggerProvider
	_, resolved := Resolve("services", provider, loggerOnly)
	got := resolved.(*capturingLogger)
	if got.id != "provider" {
		t.Fatalf("expected provider logger precedence, got %q", got.id)
	}

	resolvedProvider, resolved = Resolve("services", nil, loggerOnly)
	got = resolved.(*capturingLogger)
	if got.id != "logger" {
		t.Fatalf("expected direct logger when provider is nil, got %q", got.id)
	}
	if resolvedProvider == nil {
		t.Fatalf("expected provider wrapper from logger")
	}

	_, resolved = Resolve("services", nil, nil)
	if resolved == nil {
		t.Fatalf("expected nop logger fallback")
	}
}

func TestGoJobBridgeCompatibility(t *testing.T) {
	providerLogger := &capturingLogger{id: "provider"}
	provider := &capturingProvider{logger: providerLogger}

	_, _, jobProvider, jobLogger := ResolveForJob("services", provider, nil)
	if jobProvider == nil {
		t.Fatalf("expected go-job provider bridge")
	}
	if jobLogger == nil {
		t.Fatalf("expected go-job logger bridge")
	}

	bridged := jobProvider.GetLogger("services")
	bridged.Info("hello", "k", "v")

	captured := providerLogger.lastInfo
	if captured.msg != "hello" {
		t.Fatalf("expected bridged message, got %q", captured.msg)
	}
	if captured.args[0] != "k" || captured.args[1] != "v" {
		t.Fatalf("expected bridged args, got %#v", captured.args)
	}
}

var (
	_ glog.Logger         = (*capturingLogger)(nil)
	_ glog.LoggerProvider = (*capturingProvider)(nil)
)

type capturingProvider struct {
	logger *capturingLogger
}

func (p *capturingProvider) GetLogger(string) glog.Logger {
	if p == nil || p.logger == nil {
		return glog.Nop()
	}
	return p.logger
}

type infoCall struct {
	msg  string
	args []any
}

type capturingLogger struct {
	id       string
	lastInfo infoCall
}

func (l *capturingLogger) Trace(string, ...any) {}
func (l *capturingLogger) Debug(string, ...any) {}
func (l *capturingLogger) Warn(string, ...any)  {}
func (l *capturingLogger) Error(string, ...any) {}
func (l *capturingLogger) Fatal(string, ...any) {}

func (l *capturingLogger) Info(msg string, args ...any) {
	l.lastInfo = infoCall{
		msg:  msg,
		args: append([]any(nil), args...),
	}
}

func (l *capturingLogger) WithContext(context.Context) glog.Logger {
	return l
}
