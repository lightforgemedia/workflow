package kafkastreamer_test

import (
	"testing"

	"github.com/luno/workflow/adapters/kafkastreamer"
	adapter "github.com/luno/workflow/adapters/testing"
)

func TestStreamer(t *testing.T) {
	constructor := kafkastreamer.New([]string{"localhost:9092"})
	adapter.TestStreamer(t, constructor)
}
