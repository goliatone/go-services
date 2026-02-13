package core

import "context"

type NopMetricsRecorder struct{}

func (NopMetricsRecorder) IncCounter(context.Context, string, int64, map[string]string) {}

func (NopMetricsRecorder) ObserveHistogram(context.Context, string, float64, map[string]string) {}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return map[string]string{}
	}
	copied := make(map[string]string, len(tags))
	for key, value := range tags {
		copied[key] = value
	}
	return copied
}

var _ MetricsRecorder = NopMetricsRecorder{}
