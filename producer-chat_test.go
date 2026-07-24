package main

import (
	"context"
	"sync"
	"testing"
	"time"

	ifunny "github.com/open-ifunny/ifunny-go"
)

// TestMultiplexChatListenTeardownNoDeadlock proves the chat-listen-all teardown
// deadlock. A live subscription's callback runs on the connection's single
// reader goroutine and blocks handing its event to the producer loop. When the
// run is cancelled (the stop-after cutoff), teardown unsubscribes each channel.
// A real WAMP Unsubscribe shares that reader goroutine, so unsub() cannot return
// until the in-flight callback does. If teardown unsubscribes *before* releasing
// blocked callbacks (via done), the two wait on each other forever.
//
// The fake below models exactly that constraint: unsub() joins any in-flight
// handler for its channel. The test drives one callback into a blocked send,
// cancels, and asserts teardown completes. With unsub-before-close(done) it
// deadlocks; closing done first makes it terminate.
func TestMultiplexChatListenTeardownNoDeadlock(t *testing.T) {
	var inflight sync.WaitGroup // in-flight handler goroutines (the fake "reader")

	handlers := make(map[string]func(*ifunny.ChatEvent) error)
	var mu sync.Mutex
	subscribed := make(chan string, 1)

	subscribe := func(_ context.Context, channel string, handle func(*ifunny.ChatEvent) error) (func(), error) {
		mu.Lock()
		handlers[channel] = handle
		mu.Unlock()
		subscribed <- channel
		// A real Unsubscribe is serviced on the reader goroutine, so it can only
		// return once any callback currently dispatching has finished.
		unsub := func() { inflight.Wait() }
		return unsub, nil
	}

	// deliver invokes a channel's handler on a fresh goroutine, mirroring how the
	// connection dispatches an event into the registered callback.
	deliver := func(channel string, ev *ifunny.ChatEvent) {
		mu.Lock()
		h := handlers[channel]
		mu.Unlock()
		inflight.Add(1)
		go func() {
			defer inflight.Done()
			_ = h(ev)
		}()
	}

	// encode signals once, when the producer loop consumes the first event and is
	// about to park on the (unread) send channel.
	encoded := make(chan struct{}, 1)
	encode := func(any) ([]byte, error) {
		select {
		case encoded <- struct{}{}:
		default:
		}
		return []byte("x"), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	send := make(chan []byte) // unbuffered and never read → loop parks on send
	errs := make(chan error, 8)

	returned := make(chan struct{})
	go func() {
		multiplexChatListen(ctx, []string{"ch"}, subscribe, encode, send, errs)
		close(returned)
	}()

	<-subscribed // handler for "ch" is registered

	ev := &ifunny.ChatEvent{}

	// 1) First event: the producer loop reads it, encodes (signalling us), then
	//    parks on send<-b because nothing reads send.
	deliver("ch", ev)
	<-encoded

	// 2) Second event: its callback blocks on events<-event, because the loop is
	//    now parked in the send select and will never read events again.
	deliver("ch", ev)

	// 3) Stop-after cutoff: cancel. The loop returns and runs teardown, which
	//    unsubscribes while callback #2 is still blocked.
	cancel()

	select {
	case <-returned:
		// Clean teardown.
	case <-time.After(2 * time.Second):
		t.Fatal("multiplexChatListen deadlocked: unsub() ran before done was closed, " +
			"so a blocked callback and the unsubscribe waited on each other")
	}
}
