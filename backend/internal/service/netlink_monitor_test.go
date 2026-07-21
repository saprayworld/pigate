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

// TestNetlinkMonitor_PublishMissedStartupLinks_MatchInKnownPublishes covers case 1 from the plan
// (T-04): a name that was skipped at startup and is now present in the seeded known
// map must get a synthetic InterfaceAdded whose Up/Running come straight from the
// seeded linkState (issue #76) — never hardcoded.
func TestNetlinkMonitor_PublishMissedStartupLinks_MatchInKnownPublishes(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := map[int]linkState{
		7: {name: "wlx0cef1548ff2b", up: true, running: false},
	}

	m.publishMissedStartupLinks(known, []string{"wlx0cef1548ff2b"})

	e := expectEvent(t, ch, InterfaceAdded)
	if e.Name != "wlx0cef1548ff2b" {
		t.Errorf("expected name wlx0cef1548ff2b, got %q", e.Name)
	}
	if !e.Up || e.Running {
		t.Errorf("expected Up/Running to match seeded known state (Up=true, Running=false), got Up=%v Running=%v", e.Up, e.Running)
	}
}

// TestNetlinkMonitor_PublishMissedStartupLinks_NotInKnownNoEvent covers case 2: a missed name that
// has not (yet) appeared in the kernel snapshot must not publish anything — it will
// get a genuine InterfaceAdded later via the normal handleLinkUpdate path.
func TestNetlinkMonitor_PublishMissedStartupLinks_NotInKnownNoEvent(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := map[int]linkState{
		7: {name: "eth0", up: true, running: true},
	}

	m.publishMissedStartupLinks(known, []string{"wlx0cef1548ff2b"})

	expectNoEvent(t, ch)
}

// TestNetlinkMonitor_PublishMissedStartupLinks_EmptyMissedNoEvent covers case 3: an empty missed
// slice (the common case — startup apply skipped nothing) must not publish anything.
func TestNetlinkMonitor_PublishMissedStartupLinks_EmptyMissedNoEvent(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := map[int]linkState{
		7: {name: "eth0", up: true, running: true},
	}

	m.publishMissedStartupLinks(known, nil)

	expectNoEvent(t, ch)
}

// TestNetlinkMonitor_PublishMissedStartupLinks_ThenRealNewlinkIsDeduped covers case 4: once a
// synthetic InterfaceAdded has been published for an index already present in known,
// a subsequent real RTM_NEWLINK for that same index must be treated as a normal
// flag-change/duplicate (LinkChanged or suppressed), never a second InterfaceAdded —
// otherwise self-heal subscribers would re-apply twice for one appearance.
func TestNetlinkMonitor_PublishMissedStartupLinks_ThenRealNewlinkIsDeduped(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := map[int]linkState{
		7: {name: "wlx0cef1548ff2b", up: true, running: false},
	}

	m.publishMissedStartupLinks(known, []string{"wlx0cef1548ff2b"})
	expectEvent(t, ch, InterfaceAdded)

	// Identical flags -> duplicate RTM_NEWLINK, suppressed entirely.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 7, "wlx0cef1548ff2b", net.FlagUp), known)
	expectNoEvent(t, ch)

	// A genuine flag change (now RUNNING, e.g. Wi-Fi associated) -> LinkChanged, not
	// another InterfaceAdded.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 7, "wlx0cef1548ff2b", net.FlagUp|net.FlagRunning), known)
	e := expectEvent(t, ch, LinkChanged)
	if e.Name != "wlx0cef1548ff2b" || !e.Up || !e.Running {
		t.Errorf("expected LinkChanged for wlx0cef1548ff2b Up=true Running=true, got name=%q Up=%v Running=%v", e.Name, e.Up, e.Running)
	}
}
