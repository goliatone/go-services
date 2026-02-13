package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ActivityRetentionPolicy struct {
	TTL    time.Duration
	RowCap int
}

type ActivityRetentionPruner interface {
	Prune(ctx context.Context, policy ActivityRetentionPolicy) (deleted int, err error)
}

type OperationalActivitySink struct {
	primary  ServicesActivitySink
	fallback ServicesActivitySink
	policy   ActivityRetentionPolicy
	pruner   ActivityRetentionPruner

	queue chan ServiceActivityEntry
	now   func() time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func NewOperationalActivitySink(
	primary ServicesActivitySink,
	fallback ServicesActivitySink,
	policy ActivityRetentionPolicy,
	bufferSize int,
) (*OperationalActivitySink, error) {
	if primary == nil {
		return nil, fmt.Errorf("core: primary activity sink is required")
	}
	if bufferSize <= 0 {
		bufferSize = 128
	}

	sink := &OperationalActivitySink{
		primary:  primary,
		fallback: fallback,
		policy:   policy,
		queue:    make(chan ServiceActivityEntry, bufferSize),
		now: func() time.Time {
			return time.Now().UTC()
		},
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	if pruner, ok := primary.(ActivityRetentionPruner); ok {
		sink.pruner = pruner
	}

	go sink.run()
	return sink, nil
}

func (s *OperationalActivitySink) Record(ctx context.Context, entry ServiceActivityEntry) error {
	if s == nil || s.primary == nil {
		return fmt.Errorf("core: operational activity sink is not configured")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.now().UTC()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.queue <- entry:
		return nil
	default:
		if s.fallback != nil {
			return s.fallback.Record(ctx, entry)
		}
		return nil
	}
}

func (s *OperationalActivitySink) List(ctx context.Context, filter ServicesActivityFilter) (ServicesActivityPage, error) {
	if s == nil || s.primary == nil {
		return ServicesActivityPage{}, fmt.Errorf("core: operational activity sink is not configured")
	}
	return s.primary.List(ctx, filter)
}

func (s *OperationalActivitySink) EnforceRetention(ctx context.Context) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("core: operational activity sink is not configured")
	}
	pruner := s.pruner
	if pruner == nil {
		if p, ok := s.primary.(ActivityRetentionPruner); ok {
			pruner = p
		}
	}
	if pruner == nil {
		return 0, nil
	}
	return pruner.Prune(ctx, s.policy)
}

func (s *OperationalActivitySink) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

func (s *OperationalActivitySink) run() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			return
		case entry := <-s.queue:
			if err := s.primary.Record(context.Background(), entry); err != nil && s.fallback != nil {
				_ = s.fallback.Record(context.Background(), entry)
			}
		}
	}
}

var _ ServicesActivitySink = (*OperationalActivitySink)(nil)
