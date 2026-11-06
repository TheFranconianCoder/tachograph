package stream

import (
	"testing"
	"time"
)

func TestHubCoalescesToTheNewestMessage(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	hub.Publish([]byte("old"))
	hub.Publish([]byte("new"))

	select {
	case message := <-sub.Out():
		if string(message) != "new" {
			t.Fatalf("message = %q, want the newest snapshot", message)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received")
	}
}

func TestUnsubscribeClosesTheSubscriber(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe()
	hub.Unsubscribe(sub)

	select {
	case <-sub.Done():
	default:
		t.Fatal("Done was not closed")
	}
	if hub.Subscribers() != 0 {
		t.Errorf("subscribers = %d, want 0", hub.Subscribers())
	}

	// Publishing after unsubscribing must not panic.
	hub.Publish([]byte("ignored"))
}

func TestPublishNeverBlocksOnASlowClient(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			hub.Publish([]byte("status"))
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a subscriber that was not reading")
	}
}
