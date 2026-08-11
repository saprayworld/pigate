package kernel

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pigate/internal/model"

	nflog "github.com/florianl/go-nflog/v2"
)

// NFLOG parameters for the forward-chain packet log. real_firewall.go directs
// the forward ACCEPT/DROP log statements to this group instead of printk so the
// listener below can turn them into model.FirewallLog entries.
const (
	// ForwardNflogGroup is the netlink log group id shared with real_firewall.go
	// (equivalent to nft `log ... group 100`).
	ForwardNflogGroup uint16 = 100
	// LocalNflogGroup is the netlink log group used for input/output chain
	// packet logs (equivalent to nft `log ... group 101`). Deliberately a
	// separate group from ForwardNflogGroup rather than reusing group 100:
	// each group gets its own netlink socket, read loop and bounded channel
	// (trafficLogChanSize below), so a burst of high-volume forward traffic
	// can never starve/drop the lower-volume but diagnostically valuable
	// input/output events (who is hitting the box itself). See
	// docs/ref/todo/traffic-log-pagination-and-local-traffic-plan.md §2.3.
	LocalNflogGroup uint16 = 101
	// ForwardNflogSnaplen is the number of packet bytes the kernel copies per
	// event — enough for the IPv4/IPv6 header + TCP/UDP ports, not the payload.
	// Shared by both NFLOG groups above.
	ForwardNflogSnaplen uint32 = 64
	// trafficLogChanSize bounds the in-process handoff buffer. On a traffic
	// burst, events beyond this are dropped (counted) rather than blocking the
	// netlink read loop — the Forward Traffic page is a recent-sample view, not
	// a complete record (see forward-traffic-log-plan.md §5.3).
	trafficLogChanSize = 256
)

// RealTrafficLog implements TrafficLogManager by subscribing to NFLOG group
// ForwardNflogGroup and parsing each event's packet header into a FirewallLog.
type RealTrafficLog struct {
	ifaces *ifaceNameResolver
}

func NewRealTrafficLog() *RealTrafficLog {
	return &RealTrafficLog{ifaces: newIfaceNameResolver()}
}

// ifaceNameResolver maps a kernel interface index (as carried by NFLOG's
// indev/outdev attributes) to its name, caching successful lookups so a traffic
// burst does not issue a net.InterfaceByIndex syscall per packet. Indexes are
// effectively stable for the life of an interface, so caching is safe.
type ifaceNameResolver struct {
	mu    sync.RWMutex
	cache map[uint32]string
}

func newIfaceNameResolver() *ifaceNameResolver {
	return &ifaceNameResolver{cache: make(map[uint32]string)}
}

// name returns the interface name for the given index pointer. A nil/zero index
// (attribute absent — e.g. locally generated traffic) yields "-". Failed lookups
// fall back to "if<idx>" and are not cached, so a later successful resolve wins.
func (r *ifaceNameResolver) name(index *uint32) string {
	if index == nil || *index == 0 {
		return "-"
	}
	idx := *index

	r.mu.RLock()
	cached, ok := r.cache[idx]
	r.mu.RUnlock()
	if ok {
		return cached
	}

	iface, err := net.InterfaceByIndex(int(idx))
	if err != nil {
		return fmt.Sprintf("if%d", idx)
	}
	r.mu.Lock()
	r.cache[idx] = iface.Name
	r.mu.Unlock()
	return iface.Name
}

// WatchForwardTraffic opens the forward-chain NFLOG socket and streams
// events tagged with chain "forward". It blocks until ctx is cancelled.
func (r *RealTrafficLog) WatchForwardTraffic(ctx context.Context, cb func(model.FirewallLog)) error {
	return r.watchGroup(ctx, ForwardNflogGroup, model.PolicyChainForward, cb)
}

// WatchLocalTraffic opens the input/output-chain NFLOG socket (a separate
// group and socket from WatchForwardTraffic — see LocalNflogGroup's doc
// comment) and streams events tagged with chain "input" or "output"
// (determined per-event by parseNflogAttr from the log prefix; defaultChain
// here is only the fallback for an event with no/unrecognized prefix). It
// blocks until ctx is cancelled.
func (r *RealTrafficLog) WatchLocalTraffic(ctx context.Context, cb func(model.FirewallLog)) error {
	return r.watchGroup(ctx, LocalNflogGroup, model.PolicyChainInput, cb)
}

// watchGroup opens an NFLOG socket for the given group, then decouples the
// netlink read loop from the (potentially slow) consumer via a buffered
// channel drained by a separate goroutine. It blocks until ctx is cancelled.
// defaultChain is used by parseNflogAttr when an event's log prefix is
// missing/unrecognized (see its doc comment). Shared by WatchForwardTraffic
// and WatchLocalTraffic so the netlink plumbing is written exactly once.
func (r *RealTrafficLog) watchGroup(ctx context.Context, group uint16, defaultChain string, cb func(model.FirewallLog)) error {
	cfg := &nflog.Config{
		Group:    group,
		Copymode: nflog.CopyPacket,
		Bufsize:  ForwardNflogSnaplen,
	}
	nf, err := nflog.Open(cfg)
	if err != nil {
		return fmt.Errorf("failed to open NFLOG group %d: %w (requires CAP_NET_ADMIN)", group, err)
	}
	defer nf.Close()

	ch := make(chan model.FirewallLog, trafficLogChanSize)
	var dropped atomic.Uint64

	// Drain goroutine: the only place cb is invoked, so a slow cb never stalls
	// the netlink hook below.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case entry := <-ch:
				cb(entry)
			case <-ticker.C:
				if n := dropped.Swap(0); n > 0 {
					log.Printf("[RealTrafficLog] Dropped %d log events for NFLOG group %d (chain %s) in the last 30s (burst overflow)", n, group, defaultChain)
				}
			}
		}
	}()

	hook := func(attr nflog.Attribute) int {
		entry, ok := parseNflogAttr(attr, r.ifaces.name, defaultChain)
		if !ok {
			return 0
		}
		// Non-blocking send: overflow is dropped and counted, never blocks.
		select {
		case ch <- entry:
		default:
			dropped.Add(1)
		}
		return 0
	}

	errFunc := func(e error) int {
		log.Printf("[RealTrafficLog] NFLOG group %d read error: %v", group, e)
		return 0
	}

	if err := nf.RegisterWithErrorFunc(ctx, hook, errFunc); err != nil {
		return fmt.Errorf("failed to register NFLOG hook (group %d): %w", group, err)
	}

	log.Printf("[RealTrafficLog] Listening for chain=%s packet logs on NFLOG group %d", defaultChain, group)
	<-ctx.Done()
	return nil
}

// nflogPrefixInfo maps a normalized (whitespace-collapsed) log prefix token
// sequence to the chain/action/reason it represents, per
// docs/ref/todo/traffic-log-pagination-and-local-traffic-plan.md §2.4. Prefix
// text in real_firewall.go is not always consistently spaced (one rule emits
// "[PiGate]  INP DROP  : " with two spaces after "[PiGate]"), so parseNflogAttr
// normalizes with strings.Fields before matching here — never compare the raw
// prefix string.
type nflogPrefixInfo struct {
	chain  string
	action string
	reason string
}

var nflogPrefixTable = map[string]nflogPrefixInfo{
	"FWD:ACCEPT": {model.PolicyChainForward, "PASS", "Allowed (forward)"},
	"FWD:DROP":   {model.PolicyChainForward, "DROP", "Blocked (forward)"},
	"INP:ACCEPT": {model.PolicyChainInput, "PASS", "Allowed (local-in)"},
	"INP:DROP":   {model.PolicyChainInput, "DROP", "Blocked (local-in)"},
	"OUT:ACCEPT": {model.PolicyChainOutput, "PASS", "Allowed (local-out)"},
	"OUT:DROP":   {model.PolicyChainOutput, "DROP", "Blocked (local-out)"},
}

// parseNflogAttr turns one NFLOG event into a FirewallLog. Time and ID are left
// blank — main.go stamps the timestamp when it pushes into the ring buffer.
// resolveIface maps NFLOG indev/outdev indices to interface names (see
// ifaceNameResolver). defaultChain is used when the log prefix is absent or
// not recognized (e.g. a future rule forgets to set one) — the event is still
// surfaced (as a PASS/"Logged" entry) rather than silently dropped, tagged
// with whichever chain the NFLOG group it arrived on defaults to. Returns
// ok=false only when there is no packet payload to parse.
func parseNflogAttr(attr nflog.Attribute, resolveIface func(*uint32) string, defaultChain string) (model.FirewallLog, bool) {
	if attr.Payload == nil {
		return model.FirewallLog{}, false
	}
	entry := parsePacketHeader(*attr.Payload)
	entry.InIface = resolveIface(attr.InDev)
	entry.OutIface = resolveIface(attr.OutDev)

	chain, action, reason := defaultChain, "PASS", "Logged"
	if attr.Prefix != nil {
		fields := strings.Fields(*attr.Prefix)
		var token, verb string
		for _, f := range fields {
			switch strings.TrimSuffix(f, ":") {
			case "FWD", "INP", "OUT":
				token = strings.TrimSuffix(f, ":")
			case "ACCEPT", "DROP", "AUDIT":
				verb = strings.TrimSuffix(f, ":")
			}
			// "r=<token>" carries the matched PolicyRule id (or a "sys-*"
			// system token for structural log points) — see
			// withRuleToken/logTokenPattern in real_firewall.go. Re-validate
			// against the same whitelist here: even though the writer side
			// already sanitizes, never trust raw kernel/NFLOG bytes without
			// re-checking on the read side too.
			if rid, ok := strings.CutPrefix(f, "r="); ok && logTokenPattern.MatchString(rid) {
				entry.RuleID = rid
			}
		}
		if info, ok := nflogPrefixTable[token+":"+verb]; ok {
			chain, action, reason = info.chain, info.action, info.reason
		}
	}
	entry.Chain = chain
	entry.Action = action
	entry.Reason = reason
	return entry, true
}

// parsePacketHeader reads src/dst/proto/port out of a raw IPv4 or IPv6 packet
// header. It is defensive against short buffers (Snaplen may truncate the
// packet): any field it can't read is left as "-". The version nibble of the
// first byte selects IPv4 vs IPv6 (inet family — payload can be either).
func parsePacketHeader(pkt []byte) model.FirewallLog {
	entry := model.FirewallLog{Src: "-", Dest: "-", SrcPort: "-", Port: "-", Proto: "-"}
	if len(pkt) < 1 {
		return entry
	}

	version := pkt[0] >> 4
	var proto byte
	var transport []byte

	switch version {
	case 4:
		if len(pkt) < 20 {
			return entry
		}
		ihl := int(pkt[0]&0x0F) * 4
		if ihl < 20 {
			ihl = 20
		}
		proto = pkt[9]
		entry.Src = net.IP(pkt[12:16]).String()
		entry.Dest = net.IP(pkt[16:20]).String()
		if len(pkt) >= ihl {
			transport = pkt[ihl:]
		}
	case 6:
		if len(pkt) < 40 {
			return entry
		}
		proto = pkt[6] // Next Header (extension headers not followed — best effort)
		entry.Src = net.IP(pkt[8:24]).String()
		entry.Dest = net.IP(pkt[24:40]).String()
		transport = pkt[40:]
	default:
		return entry
	}

	switch proto {
	case 6:
		entry.Proto = "TCP"
	case 17:
		entry.Proto = "UDP"
	case 1:
		entry.Proto = "ICMP"
	case 58:
		entry.Proto = "ICMPv6"
	default:
		entry.Proto = fmt.Sprintf("proto-%d", proto)
	}

	// Source/destination ports for TCP/UDP only (offsets 0 and 2 in the
	// transport header, respectively).
	if (proto == 6 || proto == 17) && len(transport) >= 4 {
		entry.SrcPort = fmt.Sprintf("%d", binary.BigEndian.Uint16(transport[0:2]))
		entry.Port = fmt.Sprintf("%d", binary.BigEndian.Uint16(transport[2:4]))
	}

	return entry
}
