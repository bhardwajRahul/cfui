package logger

import "testing"

func TestLogBroadcasterUnsubscribeAfterCloseDoesNotPanic(t *testing.T) {
	b := NewLogBroadcaster(10)
	ch := b.Subscribe("test")

	b.Close()
	b.Unsubscribe(ch)
}
