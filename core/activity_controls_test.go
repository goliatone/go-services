package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestOperationalActivitySink_NonBlockingFallbackWhenQueueIsFull(t *testing.T) {
	primary := &blockingActivitySink{block: make(chan struct{})}
	fallback := &bufferCapturingActivitySink{}
	sink, err := NewOperationalActivitySink(primary, fallback, ActivityRetentionPolicy{}, 1)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer func() {
		close(primary.block)
		sink.Close()
	}()

	entry := ServiceActivityEntry{ID: "a", Action: "first"}
	if err := sink.Record(context.Background(), entry); err != nil {
		t.Fatalf("record first: %v", err)
	}

	start := time.Now()
	err = sink.Record(context.Background(), ServiceActivityEntry{ID: "b", Action: "second"})
	if err != nil {
		t.Fatalf("record fallback entry: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("expected non-blocking fallback write")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fallback.mu.Lock()
		count := len(fallback.entries)
		fallback.mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected fallback sink to capture saturated write")
}

func TestOperationalActivitySink_FallbackOnPrimaryError(t *testing.T) {
	primary := &errorActivitySink{}
	fallback := &bufferCapturingActivitySink{}
	sink, err := NewOperationalActivitySink(primary, fallback, ActivityRetentionPolicy{}, 4)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer sink.Close()

	if err := sink.Record(context.Background(), ServiceActivityEntry{ID: "x", Action: "fail"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fallback.mu.Lock()
		count := len(fallback.entries)
		fallback.mu.Unlock()
		if count == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected fallback write after primary failure")
}

func TestOperationalActivitySink_EnforceRetention(t *testing.T) {
	pruner := &stubPruner{deleted: 7}
	sink, err := NewOperationalActivitySink(pruner, nil, ActivityRetentionPolicy{
		TTL:    24 * time.Hour,
		RowCap: 100,
	}, 4)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer sink.Close()

	deleted, err := sink.EnforceRetention(context.Background())
	if err != nil {
		t.Fatalf("enforce retention: %v", err)
	}
	if deleted != 7 {
		t.Fatalf("expected deleted=7, got %d", deleted)
	}
	if pruner.lastPolicy.RowCap != 100 || pruner.lastPolicy.TTL != 24*time.Hour {
		t.Fatalf("expected policy propagation")
	}
}

type blockingActivitySink struct {
	block chan struct{}
}

func (s *blockingActivitySink) Record(context.Context, ServiceActivityEntry) error {
	<-s.block
	return nil
}

func (s *blockingActivitySink) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	return ServicesActivityPage{}, nil
}

type errorActivitySink struct{}

func (errorActivitySink) Record(context.Context, ServiceActivityEntry) error {
	return errors.New("primary write failed")
}

func (errorActivitySink) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	return ServicesActivityPage{}, nil
}

type bufferCapturingActivitySink struct {
	mu      sync.Mutex
	entries []ServiceActivityEntry
	last    ServiceActivityEntry
}

func (s *bufferCapturingActivitySink) Record(_ context.Context, entry ServiceActivityEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = entry
	s.entries = append(s.entries, entry)
	return nil
}

func (s *bufferCapturingActivitySink) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]ServiceActivityEntry(nil), s.entries...)
	return ServicesActivityPage{Items: items, Total: len(items)}, nil
}

type stubPruner struct {
	lastPolicy ActivityRetentionPolicy
	deleted    int
}

func (s *stubPruner) Record(context.Context, ServiceActivityEntry) error {
	return nil
}

func (s *stubPruner) List(context.Context, ServicesActivityFilter) (ServicesActivityPage, error) {
	return ServicesActivityPage{}, nil
}

func (s *stubPruner) Prune(_ context.Context, policy ActivityRetentionPolicy) (int, error) {
	s.lastPolicy = policy
	return s.deleted, nil
}

var (
	_ ServicesActivitySink    = (*blockingActivitySink)(nil)
	_ ServicesActivitySink    = (*errorActivitySink)(nil)
	_ ServicesActivitySink    = (*bufferCapturingActivitySink)(nil)
	_ ActivityRetentionPruner = (*stubPruner)(nil)
)
