package gologger

import (
	job "github.com/goliatone/go-job"
	glog "github.com/goliatone/go-logger/glog"
)

// Resolve uses deterministic precedence provider > logger > nop.
func Resolve(name string, provider glog.LoggerProvider, logger glog.Logger) (glog.LoggerProvider, glog.Logger) {
	return glog.Resolve(name, provider, logger)
}

// ToJobProvider maps a glog provider to the go-job logger provider contract.
func ToJobProvider(provider glog.LoggerProvider) job.LoggerProvider {
	if provider == nil {
		return nil
	}
	return job.GoLoggerProvider(provider)
}

// ToJobLogger maps a glog logger to the go-job logger contract.
func ToJobLogger(logger glog.Logger) job.Logger {
	if logger == nil {
		return nil
	}
	return job.GoLogger(logger)
}

// ResolveForJob resolves glog logger/provider then returns equivalent go-job adapters.
func ResolveForJob(
	name string,
	provider glog.LoggerProvider,
	logger glog.Logger,
) (glog.LoggerProvider, glog.Logger, job.LoggerProvider, job.Logger) {
	resolvedProvider, resolvedLogger := Resolve(name, provider, logger)
	return resolvedProvider, resolvedLogger, ToJobProvider(resolvedProvider), ToJobLogger(resolvedLogger)
}
