package core

import "maps"

import "context"

type NopMetricsRecorder struct{}

func (NopMetricsRecorder) IncCounter(context.Context, string, int64, map[string]string) {}

func (NopMetricsRecorder) ObserveHistogram(context.Context, string, float64, map[string]string) {}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return map[string]string{}
	}
	copied := make(map[string]string, len(tags))
	maps.Copy(copied, tags)
	return copied
}

var _ MetricsRecorder = NopMetricsRecorder{}
