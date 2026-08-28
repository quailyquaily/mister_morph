package bus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type seenPair struct {
	conv string
	id   string
}

func TestInprocPublishSubscribe(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 8, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()

	var (
		mu   sync.Mutex
		got  []string
		done = make(chan struct{})
	)
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		mu.Lock()
		got = append(got, msg.ID)
		if len(got) == 3 {
			close(done)
		}
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	base := validMessage(t)
	for i := 0; i < 3; i++ {
		msg := base
		msg.ID = fmt.Sprintf("bus_%d", i+1)
		msg.IdempotencyKey = fmt.Sprintf("idem_%d", i+1)
		if err := b.Publish(context.Background(), msg); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for messages")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("message count mismatch: got %d want 3", len(got))
	}
}

func TestInprocPublishValidatedAndWaitReturnsHandlerError(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	wantErr := errors.New("task enqueue failed")
	if err := b.Subscribe(TopicChatMessage, func(context.Context, BusMessage) error {
		return wantErr
	}); err != nil {
		t.Fatal(err)
	}

	if err := b.PublishValidatedAndWait(context.Background(), validMessage(t)); !errors.Is(err, wantErr) {
		t.Fatalf("PublishValidatedAndWait() error = %v, want %v", err, wantErr)
	}
}

func TestInprocConversationOrder(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 16, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()

	var (
		mu      sync.Mutex
		seen    = make([]seenPair, 0, 8)
		done    = make(chan struct{})
		seenCnt int
	)
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, seenPair{conv: msg.ConversationKey, id: msg.ID})
		seenCnt++
		if seenCnt == 6 {
			close(done)
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	messages := []BusMessage{
		testMessageForConversation(t, "conv:a", "a1", "i1"),
		testMessageForConversation(t, "conv:b", "b1", "i2"),
		testMessageForConversation(t, "conv:a", "a2", "i3"),
		testMessageForConversation(t, "conv:b", "b2", "i4"),
		testMessageForConversation(t, "conv:a", "a3", "i5"),
		testMessageForConversation(t, "conv:b", "b3", "i6"),
	}
	for _, msg := range messages {
		if err := b.Publish(context.Background(), msg); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for ordered deliveries")
	}

	mu.Lock()
	defer mu.Unlock()
	if extractIDs(seen, "conv:a") != "a1,a2,a3" {
		t.Fatalf("conv:a order mismatch: got %s", extractIDs(seen, "conv:a"))
	}
	if extractIDs(seen, "conv:b") != "b1,b2,b3" {
		t.Fatalf("conv:b order mismatch: got %s", extractIDs(seen, "conv:b"))
	}
}

func TestInprocBackpressure(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 1, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()

	block := make(chan struct{})
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		<-block
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	first := testMessageForConversation(t, "conv:block", "m1", "idem1")
	if err := b.Publish(context.Background(), first); err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	second := testMessageForConversation(t, "conv:block", "m2", "idem2")
	err = b.Publish(ctx, second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Publish(second) error = %v, want context deadline exceeded", err)
	}
	if code := ErrorCodeOf(err); code != CodeQueueFull {
		t.Fatalf("Publish(second) code = %q, want %q", code, CodeQueueFull)
	}
	close(block)
}

func TestInprocPublishWithoutSubscriberFails(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()

	msg := validMessage(t)
	err = b.Publish(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), ErrNoSubscriberForTopic.Error()) {
		t.Fatalf("Publish() error = %v, want ErrNoSubscriberForTopic", err)
	}
	if code := ErrorCodeOf(err); code != CodeNoSubscriber {
		t.Fatalf("Publish() code = %q, want %q", code, CodeNoSubscriber)
	}
}

func TestInprocPublishAfterCloseFails(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error { return nil }); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	err = b.Publish(context.Background(), validMessage(t))
	if !errors.Is(err, ErrBusClosed) {
		t.Fatalf("Publish() error = %v, want ErrBusClosed", err)
	}
	if code := ErrorCodeOf(err); code != CodeBusClosed {
		t.Fatalf("Publish() code = %q, want %q", code, CodeBusClosed)
	}
}

func TestInprocCloseDrainsAcceptedDeliveries(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	contextCanceled := make(chan struct{}, 1)
	var (
		mu        sync.Mutex
		delivered []string
	)
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		if msg.ID == "m1" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				contextCanceled <- struct{}{}
				return ctx.Err()
			}
		}
		mu.Lock()
		delivered = append(delivered, msg.ID)
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	first := testMessageForConversation(t, "conv:close", "m1", "close-1")
	second := testMessageForConversation(t, "conv:close", "m2", "close-2")
	if err := b.Publish(context.Background(), first); err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first delivery")
	}
	if err := b.Publish(context.Background(), second); err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- b.Close()
	}()

	select {
	case <-contextCanceled:
		close(release)
		<-closeResult
		t.Fatal("Close() canceled an accepted delivery instead of draining it")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	mu.Lock()
	got := strings.Join(delivered, ",")
	mu.Unlock()
	if got != "m1,m2" {
		t.Fatalf("delivered = %q, want %q", got, "m1,m2")
	}
}

func TestInprocHandlerCanPublishWithMaxInFlightOne(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 1, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}

	nestedResult := make(chan error, 1)
	nestedDelivered := make(chan struct{}, 1)
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		if msg.ID != "outer" {
			nestedDelivered <- struct{}{}
			return nil
		}
		nested := testMessageForConversation(t, msg.ConversationKey, "inner", "nested-inner")
		publishCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		err := b.Publish(publishCtx, nested)
		nestedResult <- err
		return err
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	outer := testMessageForConversation(t, "conv:nested", "outer", "nested-outer")
	if err := b.Publish(context.Background(), outer); err != nil {
		t.Fatalf("Publish(outer) error = %v", err)
	}
	if err := <-nestedResult; err != nil {
		_ = b.Close()
		t.Fatalf("nested Publish() error = %v", err)
	}
	select {
	case <-nestedDelivered:
	case <-time.After(2 * time.Second):
		_ = b.Close()
		t.Fatal("timed out waiting for nested delivery")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestInprocCloseDrainsMessagesPublishedByActiveHandler(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	nestedResult := make(chan error, 1)
	nestedDelivered := make(chan struct{}, 1)
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		switch msg.ID {
		case "outer-close":
			close(started)
			<-release
			nested := testMessageForConversation(t, msg.ConversationKey, "inner-close", "close-inner")
			err := b.Publish(ctx, nested)
			nestedResult <- err
			return err
		case "inner-close":
			nestedDelivered <- struct{}{}
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	outer := testMessageForConversation(t, "conv:close-nested", "outer-close", "close-outer")
	if err := b.Publish(context.Background(), outer); err != nil {
		t.Fatalf("Publish(outer) error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for outer handler")
	}

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- b.Close()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		probe := testMessageForConversation(t, "conv:probe", "probe", "close-probe")
		probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		probeErr := b.Publish(probeCtx, probe)
		cancel()
		if errors.Is(probeErr, ErrBusClosed) {
			break
		}
		if probeErr != nil && !errors.Is(probeErr, context.DeadlineExceeded) {
			t.Fatalf("Publish(probe) error = %v", probeErr)
		}
		if time.Now().After(deadline) {
			t.Fatal("bus did not stop external publishes")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-nestedResult; err != nil {
		<-closeResult
		t.Fatalf("nested Publish() during Close error = %v", err)
	}
	select {
	case <-nestedDelivered:
	case <-time.After(2 * time.Second):
		<-closeResult
		t.Fatal("timed out waiting for nested delivery during Close")
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestInprocHandlerPanicDoesNotStopShardWorker(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 1, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}

	delivered := make(chan struct{}, 1)
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error {
		if msg.ID == "panic" {
			panic("handler failed")
		}
		delivered <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if err := b.Publish(context.Background(), testMessageForConversation(t, "conv:panic", "panic", "panic-1")); err != nil {
		t.Fatalf("Publish(panic) error = %v", err)
	}
	select {
	case deliveryErr := <-b.Errors():
		if !strings.Contains(deliveryErr.ErrorText, "handler failed") {
			t.Fatalf("delivery error = %q, want panic value", deliveryErr.ErrorText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for panic delivery error")
	}

	if err := b.Publish(context.Background(), testMessageForConversation(t, "conv:panic", "after", "panic-2")); err != nil {
		t.Fatalf("Publish(after panic) error = %v", err)
	}
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("shard worker stopped after handler panic")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestInprocSubscribeDuplicateTopicFails(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()

	first := func(ctx context.Context, msg BusMessage) error { return nil }
	second := func(ctx context.Context, msg BusMessage) error { return nil }
	if err := b.Subscribe(TopicChatMessage, first); err != nil {
		t.Fatalf("Subscribe(first) error = %v", err)
	}
	err = b.Subscribe(TopicChatMessage, second)
	if err == nil || !strings.Contains(err.Error(), ErrTopicAlreadyHandled.Error()) {
		t.Fatalf("Subscribe(second) error = %v, want ErrTopicAlreadyHandled", err)
	}
	if code := ErrorCodeOf(err); code != CodeTopicAlreadyHandled {
		t.Fatalf("Subscribe(second) code = %q, want %q", code, CodeTopicAlreadyHandled)
	}
}

func TestInprocSubscribeAfterPublishFails(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error { return nil }); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := b.Publish(context.Background(), validMessage(t)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	err = b.Subscribe(TopicDMReplyV1, func(ctx context.Context, msg BusMessage) error { return nil })
	if err == nil || !errors.Is(err, ErrTopicFrozen) {
		t.Fatalf("Subscribe() error = %v, want ErrTopicFrozen", err)
	}
	if code := ErrorCodeOf(err); code != CodeTopicFrozen {
		t.Fatalf("Subscribe() code = %q, want %q", code, CodeTopicFrozen)
	}
}

func TestInprocPublishValidatedRejectsInvalidMessage(t *testing.T) {
	b, err := NewInproc(InprocOptions{MaxInFlight: 2, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()
	if err := b.Subscribe(TopicChatMessage, func(ctx context.Context, msg BusMessage) error { return nil }); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	msg := validMessage(t)
	msg.IdempotencyKey = ""
	err = b.PublishValidated(context.Background(), msg)
	if err == nil {
		t.Fatalf("PublishValidated() expected error")
	}
	if code := ErrorCodeOf(err); code != CodeInvalidMessage {
		t.Fatalf("PublishValidated() code = %q, want %q", code, CodeInvalidMessage)
	}
}

func testMessageForConversation(t *testing.T, conversationKey string, id string, idem string) BusMessage {
	t.Helper()
	msg := validMessage(t)
	msg.ConversationKey = conversationKey
	msg.ID = id
	msg.IdempotencyKey = idem
	return msg
}

func extractIDs(pairs []seenPair, conv string) string {
	out := make([]string, 0, len(pairs))
	for _, item := range pairs {
		if item.conv == conv {
			out = append(out, item.id)
		}
	}
	return strings.Join(out, ",")
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
