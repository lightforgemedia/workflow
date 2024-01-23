package workflow

import (
	"context"
	"time"

	"github.com/luno/jettison/errors"
)

// EventStreamer implementations should all be tested with adaptertest.TestEventStreamer
type EventStreamer interface {
	NewProducer(topic string) Producer
	NewConsumer(topic string, name string, opts ...ConsumerOption) Consumer
}

type Producer interface {
	Send(ctx context.Context, recordID int64, statusType int, headers map[Header]string) error
	Close() error
}

type Consumer interface {
	Recv(ctx context.Context) (*Event, Ack, error)
	Close() error
}

// Ack is used for the event streamer to update its cursor of what messages have
// been consumed. If Ack is not called then the event streamer, depending on implementation,
// will likely not keep track of which records / events have been consumed.
type Ack func() error

type ConsumerOptions struct {
	PollFrequency time.Duration
	EventFilter   EventFilter
}

// EventFilter can be passed to the event streaming implementation to allow specific consumers to have an
// earlier on filtering process. True is returned when the event should be skipped.
type EventFilter func(e *Event) bool

type ConsumerOption func(*ConsumerOptions)

func WithEventFilter(ef EventFilter) ConsumerOption {
	return func(opt *ConsumerOptions) {
		opt.EventFilter = ef
	}
}

func WithConsumerPollFrequency(d time.Duration) ConsumerOption {
	return func(opt *ConsumerOptions) {
		opt.PollFrequency = d
	}
}

func awaitWorkflowStatusByForeignID[Type any, Status StatusType](ctx context.Context, w *Workflow[Type, Status], status Status, foreignID, runID string, role string, pollFrequency time.Duration) (*Record[Type, Status], error) {
	topic := Topic(w.Name, int(status))
	stream := w.eventStreamerFn.NewConsumer(
		topic,
		role,
		WithConsumerPollFrequency(pollFrequency),
		WithEventFilter(func(e *Event) bool {
			fid, ok := e.Headers[HeaderWorkflowForeignID]
			if !ok {
				return false
			}

			rID, ok := e.Headers[HeaderRunID]
			if !ok {
				return false
			}

			return fid != foreignID || rID != runID
		}),
	)
	defer stream.Close()

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		e, ack, err := stream.Recv(ctx)
		if err != nil {
			return nil, err
		}

		if e.Headers[HeaderWorkflowName] != w.Name {
			err = ack()
			if err != nil {
				return nil, err
			}

			continue
		}

		if e.Type != int(status) {
			err = ack()
			if err != nil {
				return nil, err
			}

			continue
		}

		r, err := w.recordStore.Lookup(ctx, e.ForeignID)
		if errors.Is(err, ErrRecordNotFound) {
			err = ack()
			if err != nil {
				return nil, err
			}

			continue
		} else if err != nil {
			return nil, err
		}

		var t Type
		err = Unmarshal(r.Object, &t)
		if err != nil {
			return nil, err
		}

		return &Record[Type, Status]{
			WireRecord: *r,
			Status:     Status(r.Status),
			Object:     &t,
		}, ack()
	}
}