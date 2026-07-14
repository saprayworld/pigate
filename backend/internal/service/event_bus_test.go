package service

import (
	"sync"
	"testing"
	"time"
)

// waitFor polls cond until it is true or the deadline elapses. Keeps the timing
// tests from being flaky on a slow CI box without hard-coding long sleeps.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func TestBusDebouncedCoalesces(t *testing.T) {
	bus := newNetEventBus(30 * time.Millisecond)

	var mu sync.Mutex
	calls := 0
	bus.Subscribe("test", []NetEventKind{InterfaceAdded}, Debounced, func(NetEvent) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	// A burst of events for the SAME interface within the window collapses to one call.
	for i := 0; i < 5; i++ {
		bus.Publish(NetEvent{Kind: InterfaceAdded, Name: "eth0"})
		time.Sleep(3 * time.Millisecond)
	}

	if !waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 1
	}) {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("expected exactly 1 coalesced call, got %d", calls)
	}
}

func TestBusDebouncedPerDistinctName(t *testing.T) {
	bus := newNetEventBus(30 * time.Millisecond)

	var mu sync.Mutex
	names := map[string]int{}
	bus.Subscribe("test", []NetEventKind{InterfaceAdded}, Debounced, func(e NetEvent) {
		mu.Lock()
		names[e.Name]++
		mu.Unlock()
	})

	// Two distinct interfaces added in the same window must each survive coalescing.
	bus.Publish(NetEvent{Kind: InterfaceAdded, Name: "eth0"})
	bus.Publish(NetEvent{Kind: InterfaceAdded, Name: "eth1"})

	if !waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return names["eth0"] == 1 && names["eth1"] == 1
	}) {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("expected one call per distinct name, got %v", names)
	}
}

func TestBusImmediatePreservesOrder(t *testing.T) {
	bus := newNetEventBus(30 * time.Millisecond)

	var mu sync.Mutex
	var got []bool // Running flag sequence
	bus.Subscribe("dhcpcd", []NetEventKind{LinkChanged}, Immediate, func(e NetEvent) {
		mu.Lock()
		got = append(got, e.Running)
		mu.Unlock()
	})

	// Simulate Wi-Fi: UP-not-running, then UP-running. dhcpcd must see both, in order.
	bus.Publish(NetEvent{Kind: LinkChanged, Name: "wlan0", Up: true, Running: false})
	bus.Publish(NetEvent{Kind: LinkChanged, Name: "wlan0", Up: true, Running: true})

	if !waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	}) {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("expected 2 immediate deliveries, got %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if got[0] != false || got[1] != true {
		t.Fatalf("immediate delivery lost order: %v", got)
	}
}

func TestBusPauseSuppressesResumeRestores(t *testing.T) {
	bus := newNetEventBus(20 * time.Millisecond)

	var mu sync.Mutex
	calls := 0
	bus.Subscribe("test", []NetEventKind{InterfaceAdded}, Debounced, func(NetEvent) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	bus.Pause()
	bus.Publish(NetEvent{Kind: InterfaceAdded, Name: "eth0"})
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	if calls != 0 {
		mu.Unlock()
		t.Fatalf("expected no dispatch while paused, got %d", calls)
	}
	mu.Unlock()

	bus.Resume()
	bus.Publish(NetEvent{Kind: InterfaceAdded, Name: "eth0"})
	if !waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 1
	}) {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("expected dispatch after resume, got %d", calls)
	}
}

func TestBusSlowHandlerDoesNotBlockOthers(t *testing.T) {
	bus := newNetEventBus(20 * time.Millisecond)

	release := make(chan struct{})
	bus.Subscribe("slow", []NetEventKind{InterfaceAdded}, Immediate, func(NetEvent) {
		<-release // block until the test lets go
	})

	var mu sync.Mutex
	fast := 0
	bus.Subscribe("fast", []NetEventKind{InterfaceAdded}, Immediate, func(NetEvent) {
		mu.Lock()
		fast++
		mu.Unlock()
	})

	bus.Publish(NetEvent{Kind: InterfaceAdded, Name: "eth0"})

	// The fast subscriber must run even though the slow one is stuck.
	if !waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return fast == 1
	}) {
		close(release)
		t.Fatal("fast subscriber was blocked by slow subscriber")
	}
	close(release)
}

func TestBusOnlyDeliversSubscribedKinds(t *testing.T) {
	bus := newNetEventBus(20 * time.Millisecond)

	var mu sync.Mutex
	calls := 0
	bus.Subscribe("test", []NetEventKind{InterfaceAdded}, Immediate, func(NetEvent) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	bus.Publish(NetEvent{Kind: LinkChanged, Name: "eth0"})      // not subscribed
	bus.Publish(NetEvent{Kind: AddrRouteChanged, Name: "eth0"}) // not subscribed
	time.Sleep(40 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("subscriber received events it did not subscribe to: %d", calls)
	}
}
