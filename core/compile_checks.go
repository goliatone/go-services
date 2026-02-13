package core

import glog "github.com/goliatone/go-logger/glog"

var (
	_ Registry           = (*ProviderRegistry)(nil)
	_ InheritancePolicy  = (*StrictIsolationPolicy)(nil)
	_ IntegrationService = (*Service)(nil)

	_ Logger         = glog.Nop()
	_ LoggerProvider = glog.ProviderFromLogger(glog.Nop())
)
