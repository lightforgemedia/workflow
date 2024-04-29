package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/luno/jettison/jtest"
	"github.com/stretchr/testify/require"
	clock_testing "k8s.io/utils/clock/testing"
)

func TestProcessTimeout(t *testing.T) {
	ctx := context.Background()
	w := &Workflow[string, testStatus]{
		Name:  "example",
		ctx:   ctx,
		clock: clock_testing.NewFakeClock(time.Date(2024, time.April, 19, 0, 0, 0, 0, time.UTC)),
	}

	value := "data"
	b, err := Marshal(&value)
	jtest.RequireNil(t, err)

	type calls struct {
		updater      func(ctx context.Context, current testStatus, next testStatus, record *Record[string, testStatus]) error
		store        func(ctx context.Context, record *WireRecord, maker OutboxEventDataMaker) error
		timeoutFunc  func(ctx context.Context, r *Record[string, testStatus], now time.Time) (testStatus, error)
		completeFunc func(ctx context.Context, id int64) error
	}

	type caller func(call map[string]int) calls

	testCases := []struct {
		name          string
		caller        caller
		timeout       timeout[string, testStatus]
		record        *WireRecord
		expectedCalls map[string]int
	}{
		{
			name: "Golden path consume",
			caller: func(call map[string]int) calls {
				return calls{
					updater: func(ctx context.Context, current testStatus, next testStatus, record *Record[string, testStatus]) error {
						call["updater"] += 1
						require.Equal(t, "new data", *record.Object)
						return nil
					},
					store: func(ctx context.Context, record *WireRecord, maker OutboxEventDataMaker) error {
						call["store"] += 1
						return nil
					},
					timeoutFunc: func(ctx context.Context, r *Record[string, testStatus], now time.Time) (testStatus, error) {
						call["timeout/TimeoutFunc"] += 1
						*r.Object = "new data"
						return statusEnd, nil
					},
					completeFunc: func(ctx context.Context, id int64) error {
						call["complete"] += 1
						return nil
					},
				}
			},
			record: &WireRecord{
				ID:           1,
				WorkflowName: "example",
				ForeignID:    "32948623984623",
				RunID:        "JHFJDS-LSFKHJSLD-KSJDBLSL",
				RunState:     RunStateRunning,
				Status:       int(statusStart),
				Object:       b,
			},
			expectedCalls: map[string]int{
				"timeout/TimeoutFunc": 1,
				"updater":             1,
				"complete":            1,
			},
		},
		{
			name: "Skip consume",
			caller: func(call map[string]int) calls {
				return calls{
					timeoutFunc: func(ctx context.Context, r *Record[string, testStatus], now time.Time) (testStatus, error) {
						call["timeout/TimeoutFunc"] += 1
						*r.Object = "new data"
						return testStatus(SkipTypeDefault), nil
					},
				}
			},
			record: &WireRecord{
				ID:           1,
				WorkflowName: "example",
				ForeignID:    "32948623984623",
				RunID:        "JHFJDS-LSFKHJSLD-KSJDBLSL",
				RunState:     RunStateRunning,
				Status:       int(statusStart),
				Object:       b,
			},
			expectedCalls: map[string]int{
				"timeout/TimeoutFunc": 1,
			},
		},
		{
			name: "Mark record as Running",
			caller: func(call map[string]int) calls {
				return calls{
					timeoutFunc: func(ctx context.Context, r *Record[string, testStatus], now time.Time) (testStatus, error) {
						call["timeout/TimeoutFunc"] += 1
						return testStatus(SkipTypeDefault), nil
					},
					store: func(ctx context.Context, record *WireRecord, maker OutboxEventDataMaker) error {
						call["store"] += 1
						require.Equal(t, record.RunState, RunStateRunning)
						return nil
					},
				}
			},
			record: &WireRecord{
				ID:           1,
				WorkflowName: "example",
				ForeignID:    "32948623984623",
				RunID:        "JHFJDS-LSFKHJSLD-KSJDBLSL",
				RunState:     RunStateInitiated,
				Status:       int(statusStart),
				Object:       b,
			},
			expectedCalls: map[string]int{
				"timeout/TimeoutFunc": 1,
				"store":               1,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			calls := map[string]int{}

			timeout := timeout[string, testStatus]{
				TimeoutFunc: tc.caller(calls).timeoutFunc,
			}

			tr := TimeoutRecord{
				ID:           1,
				WorkflowName: tc.record.WorkflowName,
				ForeignID:    tc.record.ForeignID,
				RunID:        tc.record.RunID,
				Status:       tc.record.Status,
			}

			err := processTimeout(ctx, w, timeout, tc.record, tr, tc.caller(calls).completeFunc, tc.caller(calls).store, tc.caller(calls).updater, "processName")
			jtest.RequireNil(t, err)

			require.Equal(t, tc.expectedCalls, calls)
		})
	}
}