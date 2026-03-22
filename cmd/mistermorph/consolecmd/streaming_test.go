package consolecmd

import "testing"

func TestConsoleStreamHubEvictsDoneFrameWithoutSubscribers(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-no-subscriber"

	hub.PublishSnapshot(taskID, "partial output")
	if _, ok := hub.latest[taskID]; !ok {
		t.Fatal("latest snapshot missing before completion")
	}

	hub.PublishFinal(taskID, "final output")
	if _, ok := hub.latest[taskID]; ok {
		t.Fatal("latest entry retained after done frame without subscribers")
	}
}

func TestConsoleStreamHubEvictsDoneFrameOnLastUnsubscribe(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-with-subscriber"

	_, unsubscribe := hub.Subscribe(taskID)
	hub.PublishFinal(taskID, "final output")

	latest, ok := hub.latest[taskID]
	if !ok {
		t.Fatal("latest entry missing while subscriber is connected")
	}
	if !latest.Done {
		t.Fatalf("latest.Done = %v, want true", latest.Done)
	}

	unsubscribe()
	if _, ok := hub.latest[taskID]; ok {
		t.Fatal("latest entry retained after last subscriber unsubscribed")
	}
}
