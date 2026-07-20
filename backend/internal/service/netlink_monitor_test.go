package service

import (
	"net"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// newLinkUpdate builds a minimal netlink.LinkUpdate for the given header type,
// index, name and flags — enough for handleLinkUpdate to classify it. Note that
// handleLinkUpdate reads the interface index from u.Index (the netlink message's
// embedded IfInfomsg), not from Link.Attrs().Index, so both must be set.
func newLinkUpdate(msgType uint16, index int, name string, flags net.Flags) netlink.LinkUpdate {
	return netlink.LinkUpdate{
		IfInfomsg: nl.IfInfomsg{IfInfomsg: unix.IfInfomsg{Index: int32(index)}},
		Header:    unix.NlMsghdr{Type: msgType},
		Link: &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Index: index,
				Name:  name,
				Flags: flags,
			},
		},
	}
}

// collectEvents subscribes an Immediate handler on the bus that forwards every
// event it sees into a buffered channel, so tests can assert on delivery order and
// count with a timeout instead of relying on a fixed sleep.
func collectEvents(t *testing.T, bus *NetEventBus) chan NetEvent {
	t.Helper()
	ch := make(chan NetEvent, 16)
	bus.Subscribe("test",
		[]NetEventKind{InterfaceAdded, InterfaceRemoved, LinkChanged, AddrRouteChanged},
		Immediate,
		func(e NetEvent) { ch <- e })
	return ch
}

func expectEvent(t *testing.T, ch chan NetEvent, wantKind NetEventKind) NetEvent {
	t.Helper()
	select {
	case e := <-ch:
		if e.Kind != wantKind {
			t.Fatalf("expected event kind %s, got %s (name=%q)", wantKind, e.Kind, e.Name)
		}
		return e
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event kind %s", wantKind)
		return NetEvent{}
	}
}

func expectNoEvent(t *testing.T, ch chan NetEvent) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("expected no event, got kind=%s name=%q", e.Kind, e.Name)
	case <-time.After(100 * time.Millisecond):
		// no event arrived — expected
	}
}

// TestNetlinkMonitor_DuplicateNewlinkSuppressed covers case 1 from the plan: the same
// RTM_NEWLINK (identical name/up/running) delivered twice for a known index must
// publish only once.
func TestNetlinkMonitor_DuplicateNewlinkSuppressed(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state
	flags := net.FlagUp | net.FlagRunning

	// First NEWLINK for a never-seen index -> InterfaceAdded.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceAdded)

	// Second NEWLINK, identical name/up/running -> suppressed, no event.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectNoEvent(t, ch)
}

// TestNetlinkMonitor_FlagChangePublishes covers case 2: a genuine flag change on a
// known index must still publish LinkChanged.
func TestNetlinkMonitor_FlagChangePublishes(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", net.FlagUp|net.FlagRunning), known)
	expectEvent(t, ch, InterfaceAdded)

	// Flip down: up/running both go false -> a real change, must publish.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", 0), known)
	e := expectEvent(t, ch, LinkChanged)
	if e.Up || e.Running {
		t.Errorf("expected Up=false Running=false, got Up=%v Running=%v", e.Up, e.Running)
	}
}

// TestNetlinkMonitor_RenameSameFlagsPublishes covers case 3: a rename (name changes,
// flags stay the same) must still publish — only name+up+running all-equal is
// suppressed.
func TestNetlinkMonitor_RenameSameFlagsPublishes(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state
	flags := net.FlagUp | net.FlagRunning

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceAdded)

	// Same flags, different name -> a rename, must publish LinkChanged.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth1", flags), known)
	e := expectEvent(t, ch, LinkChanged)
	if e.Name != "eth1" {
		t.Errorf("expected renamed interface name eth1, got %q", e.Name)
	}
}

// TestNetlinkMonitor_DellinkThenNewlinkIsInterfaceAdded covers case 4: removing a
// link drops it from known, so a subsequent NEWLINK for the same index is treated as
// a brand-new interface (InterfaceAdded), not a duplicate.
func TestNetlinkMonitor_DellinkThenNewlinkIsInterfaceAdded(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state
	flags := net.FlagUp | net.FlagRunning

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceAdded)

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_DELLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceRemoved)

	if _, seen := known[5]; seen {
		t.Fatalf("expected index 5 to be dropped from known after DELLINK")
	}

	// Same index reappears -> treated as a new interface, not a duplicate.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceAdded)
}
