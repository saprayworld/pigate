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
// flags stay the same) arriving as the very first event after an index's creation
// (the udev-rename-race shape from PR #79 follow-up / issue #76 §1.1, e.g. a USB
// Wi-Fi adapter created as "eth0" then immediately udev-renamed to a MAC-based name
// on the same index) must publish InterfaceAdded with the new (settled) name, not
// LinkChanged — otherwise name-filtering self-heal subscribers that match the DB's
// final configured name would never see it.
func TestNetlinkMonitor_RenameSameFlagsPublishes(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state
	flags := net.FlagUp | net.FlagRunning

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceAdded)

	// Same flags, different name, first event after creation -> udev rename race,
	// must publish InterfaceAdded with the new name.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth1", flags), known)
	e := expectEvent(t, ch, InterfaceAdded)
	if e.Name != "eth1" {
		t.Errorf("expected renamed interface name eth1, got %q", e.Name)
	}
}

// TestNetlinkMonitor_RenameAfterSettledIsLinkChanged covers a genuine rename of an
// interface that has already settled (had at least one other event since creation):
// InterfaceAdded(eth0) -> LinkChanged (flag change, same name, consumes the settling
// window) -> rename to eth1 must be LinkChanged, not InterfaceAdded, because the
// settling window was already consumed by the intervening flag-change event.
func TestNetlinkMonitor_RenameAfterSettledIsLinkChanged(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", net.FlagUp|net.FlagRunning), known)
	expectEvent(t, ch, InterfaceAdded)

	// Flag-only change, same name -> LinkChanged, consumes the settling window.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", net.FlagUp), known)
	expectEvent(t, ch, LinkChanged)

	// Rename now arrives, but settling was already consumed -> must be LinkChanged.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth1", net.FlagUp), known)
	e := expectEvent(t, ch, LinkChanged)
	if e.Name != "eth1" {
		t.Errorf("expected renamed interface name eth1, got %q", e.Name)
	}
}

// TestNetlinkMonitor_DuplicateThenRenameIsLinkChanged covers the pitfall Caution 7/9
// warn about: the duplicate-NEWLINK-suppression branch must also consume the
// settling window (even though it doesn't publish anything), otherwise a stale
// settling=true from creation would survive an intervening duplicate and wrongly
// turn a later genuine rename into InterfaceAdded.
func TestNetlinkMonitor_DuplicateThenRenameIsLinkChanged(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := make(map[int]linkState) // deterministic baseline; do not seed from the real host's kernel state
	flags := net.FlagUp | net.FlagRunning

	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectEvent(t, ch, InterfaceAdded)

	// Exact duplicate NEWLINK -> suppressed, no event, but consumes settling.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 5, "eth0", flags), known)
	expectNoEvent(t, ch)

	// Rename arrives third -> settling was consumed by the duplicate -> LinkChanged.
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

// TestNetlinkMonitor_PublishMissedStartupLinks_ThenRenameIsInterfaceAdded covers the
// compound race between the #76 missed-startup-window fix (T-03) and the udev rename
// race (T-06): a synthetic InterfaceAdded from publishMissedStartupLinks must also
// mark the index as settling, so if the very next real RTM_NEWLINK for that index is
// a rename (not just a flag change), it is still classified as InterfaceAdded with
// the new (final) name — not LinkChanged, which name-filtering self-heal subscribers
// would miss.
func TestNetlinkMonitor_PublishMissedStartupLinks_ThenRenameIsInterfaceAdded(t *testing.T) {
	bus := newNetEventBus(10 * time.Millisecond)
	m := NewNetlinkMonitor(nil, bus)
	ch := collectEvents(t, bus)

	known := map[int]linkState{
		7: {name: "wlan0", up: true, running: false},
	}

	m.publishMissedStartupLinks(known, []string{"wlan0"})
	expectEvent(t, ch, InterfaceAdded)

	// Rename arrives as the first real event after the synthetic InterfaceAdded ->
	// must be InterfaceAdded with the new name, not LinkChanged.
	m.handleLinkUpdate(newLinkUpdate(unix.RTM_NEWLINK, 7, "wlx4086cbb56030", net.FlagUp), known)
	e := expectEvent(t, ch, InterfaceAdded)
	if e.Name != "wlx4086cbb56030" {
		t.Errorf("expected renamed interface name wlx4086cbb56030, got %q", e.Name)
	}
}
