# Self-healing Infrastructure — internal event bus + startup reconciliation

> Design record for the two-state self-healing model introduced for GitHub issue #48.
> Implemented on branch `feat/self-healing-event-bus`. This is the "what shipped" doc;
> the original work plan is in the git history of `docs/ref/todo/self-healing-event-bus-plan.md`.

## 1. Principle

> **Self-healing = keep the system working, don't delete config that looks wrong.**
> Config is *user intent*. The system must tolerate dangling references (a NIC unplugged,
> a VLAN deleted outside PiGate) and, when the resource returns, come back to working order
> **on its own** — while the user always stays informed via the Event Log.

Concretely: an event handler may only ever mutate **runtime state** (kernel routes, nftables,
dnsmasq config, qdiscs). It must **never** delete or mutate user config in the DB. Removing a
configured interface is an explicit user action (a future UI, issue #49), never an automatic
reaction to a link going away.

## 2. Two states

1. **Startup state** — reconcile-on-boot. Each subsystem's `InitApplyConfig` /
   `InitApplyConfigurationAtStartup` compares the DB against the kernel and applies. This
   covers reboot/power-loss where interfaces are missing from the very first moment and the
   kernel emits *no* event to react to.
2. **Running state** — event-triggered while running, via the internal event bus below. This
   covers a resource that disappears and returns *during* operation (the field-test case: a
   VLAN deleted and recreated outside PiGate that used to stay DOWN with no IP until the user
   pressed UP in the UI).

## 3. Components

### NetEventBus (`backend/internal/service/event_bus.go`)

A stdlib-only in-process pub/sub fan-out (channels + timers, no third-party dependency). Adding
a new self-healing consumer no longer means threading another dependency through the monitor's
constructor — it just calls `Subscribe`.

- **Semantic event kinds**: `InterfaceAdded`, `InterfaceRemoved`, `LinkChanged`,
  `AddrRouteChanged`. `NetEvent{Kind, Name, Up, Running}`.
- **Subscribe modes**:
  - `Debounced` — coalesces a burst over a 500 ms window, then delivers the handler once per
    distinct interface name. For expensive, idempotent full re-syncs.
  - `Immediate` — delivers every event, in order, on the subscriber's own goroutine. For
    consumers that must observe intermediate transitions (dhcpcd, see Caution 3).
- Each subscriber runs on its own goroutine, so a slow handler (dnsmasq restart over D-Bus)
  never blocks the publisher or other subscribers.
- **Pause/Resume** live on the bus and suppress dispatch for *all* subscribers — used to bracket
  a config import so nothing re-applies against a half-written DB.

### NetlinkMonitor as translator (`backend/internal/service/netlink_monitor.go`)

The monitor owns no business logic. It subscribes to raw netlink Link/Addr/Route updates and
translates them into semantic `NetEvent`s on the bus. The key translation: it tracks the set of
link indexes it has already seen (seeded from `netlink.LinkList()` at Start), so it distinguishes
a genuinely new interface (`InterfaceAdded`) from a mere flag flip on a known index
(`LinkChanged`). `RTM_DELLINK` → `InterfaceRemoved`; addr/route events → `AddrRouteChanged`.

## 4. Subscription table (wired in `cmd/pigate/main.go`)

| Subscriber      | Kinds                                  | Mode      | Action |
|-----------------|----------------------------------------|-----------|--------|
| interface       | InterfaceAdded                         | Debounced | `ReapplyInterfaceByName(name)` — re-apply DB config for the returned link; recreate child VLANs of a returned parent |
| dhcpcd          | InterfaceAdded, LinkChanged, Removed   | Immediate | `HandleLinkEvent(name, up, running)` — start/stop the per-iface client |
| routing-dns     | AddrRouteChanged, LinkChanged          | Debounced | `ReconcileKernelRoutingTable()` + `ApplyDNSConfig()` |
| dhcp-server     | InterfaceAdded                         | Debounced | `ApplyAll()` — restore the dhcp-range skipped while the iface was gone |
| qos             | InterfaceAdded                         | Debounced | `SyncToKernel()` — re-attach qdiscs/classes |
| event-log       | InterfaceAdded, InterfaceRemoved       | Immediate | Record come/go so self-healing is observable |

Subsystems that already self-heal and need **no** subscription:
- **DNS Server** — dnsmasq listens via `bind-dynamic` (#50), so it binds new interfaces itself.
- **Firewall** — nftables matches `iifname` by name, not by link index, so a returning interface
  is matched automatically.

## 5. Startup tolerance fix

`kernel/real_qos.go` was the last subsystem that aborted its whole sync when one interface was
missing (`LinkByName` error → `return`). It now skips + logs and continues, matching the
tolerance pattern in `real_routing.go`. So a QoS rule on an offline NIC no longer blocks QoS for
every other interface, and the qdisc re-attaches on its own when the link returns
(`InterfaceAdded` → `SyncToKernel`).

## 6. Ordering

`NetlinkMonitor.Start` runs **last** in `main.go`, after every subsystem's startup apply has
completed. Starting it earlier would let the flurry of boot-time link events (dhcpcd bringing
links up) fire self-heal re-applies that race the startup path. The brief drift window between
the applies and Start is acceptable — the applies just ran.

## 7. Invariants / cautions (carried from the plan)

1. `RTM_NEWLINK` fires on every flag change, not just creation → only an unseen link index is
   `InterfaceAdded`; heavy re-appliers key off `InterfaceAdded` to avoid a re-apply storm on a
   blinking link.
2. Handlers are idempotent, so an event a handler's own re-apply generates converges (no loop).
3. dhcpcd must be `Immediate` — Wi-Fi needs the "UP-not-running" → "RUNNING" transition observed
   in order, or it never requests a lease.
4. Monitor starts after all startup applies (see §6).
5. Pause/Resume is bus-level, covering every subscriber during a config import.
6. Handlers never delete user config in the DB (§1).
7. Slow handlers run on per-subscriber goroutines so they can't stall the netlink loop.

## 8. Acceptance test (field-test-derived)

Create a VLAN via PiGate → `ip link del <vlan>` outside PiGate → `ip link add …` to recreate →
the interface returns **UP with its static IP + DHCP range + QoS rules, without touching the UI**.
A drift-check against DB desired-state ensures an interface the user intentionally set DOWN is
never forced back UP.

> Requires a real kernel (mock mode disables the monitor). Verify on a VM/board.
