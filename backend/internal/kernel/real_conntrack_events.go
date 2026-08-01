//go:build linux

package kernel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"

	"pigate/internal/model"
)

// This file implements WatchFlowEnd (kernel.TrafficAccountingManager) by
// subscribing to the conntrack DESTROY multicast group, per
// docs/ref/todo/traffic-accounting-accuracy-phase2-plan.md §2.1. It adds no
// new dependency: every primitive used (nl.Subscribe, NetlinkSocket.Receive,
// nl.ParseRouteAttr, the CTA_* attribute constants) comes from
// github.com/vishvananda/netlink/nl, already a direct dependency for
// DumpFlows/DumpRuleCounters in real_traffic_account.go. The higher-level
// netlink.ConntrackTableList helper used by DumpFlows has no equivalent for
// events, so the CTA attribute walk below is written by hand (parseRawData in
// that library is unexported).
//
// Deliberately does NOT touch real_firewall.go or any nftables chain — this
// is a pure conntrack (netfilter connection tracking) read, unrelated to rule
// evaluation (plan §0, Caution 14).

const (
	// conntrackEventChanSize bounds the in-process handoff buffer between the
	// netlink read loop and the (potentially slow) service-layer callback,
	// mirroring trafficLogChanSize in real_traffic_log.go. A burst of DESTROY
	// events (e.g. a port scan tearing down thousands of short flows) drops
	// events beyond this bound rather than blocking the read loop or growing
	// memory without limit (plan Caution 3).
	conntrackEventChanSize = 256

	// maxConntrackEventsPerSecond caps how many DESTROY events this listener
	// will forward to the callback per second, on top of the bounded channel
	// above. This is a second, independent backstop against a scan/DDoS
	// generating tens of thousands of teardowns/sec (plan Caution 3): even if
	// the consumer were fast enough to keep the channel from filling, the
	// aggregation cost of applying that many deltas is capped here.
	maxConntrackEventsPerSecond = 2000

	// nfnlSubsysCtNetlink is NFNL_SUBSYS_CTNETLINK from
	// linux/netfilter/nfnetlink.h. Not exported by nl, so redeclared here to
	// recognize DESTROY event message types defensively.
	nfnlSubsysCtNetlink = 1
)

// nfnlMsgCtDelete is the ctnetlink netlink message type used for DESTROY
// events: (subsys<<8)|IPCTNL_MSG_CT_DELETE.
const nfnlMsgCtDelete = (nfnlSubsysCtNetlink << 8) | nl.IPCTNL_MSG_CT_DELETE

// WatchFlowEnd subscribes to NFNLGRP_CONNTRACK_DESTROY and streams the final
// byte count of every conntrack flow at teardown. It blocks until ctx is
// cancelled or the subscription itself fails (e.g. missing CAP_NET_ADMIN or a
// kernel built without CONFIG_NF_CONNTRACK_EVENTS) — a returned error here is
// not fatal to the caller, which must degrade to poll-only (plan Caution 6).
func (r *RealTrafficAccounting) WatchFlowEnd(ctx context.Context, cb func(model.FlowSample)) error {
	sock, err := nl.Subscribe(unix.NETLINK_NETFILTER, unix.NFNLGRP_CONNTRACK_DESTROY)
	if err != nil {
		return fmt.Errorf("failed to subscribe to conntrack DESTROY events: %w (requires CAP_NET_ADMIN and CONFIG_NF_CONNTRACK_EVENTS)", err)
	}

	// Close the socket as soon as ctx is done, which unblocks the blocking
	// Receive() call below (plan Caution 8: fd must not outlive ctx).
	var closeOnce sync.Once
	closeSock := func() { closeOnce.Do(sock.Close) }
	defer closeSock()
	go func() {
		<-ctx.Done()
		closeSock()
	}()

	ch := make(chan model.FlowSample, conntrackEventChanSize)
	var dropped atomic.Uint64
	var enobufs atomic.Uint64
	var rateLimited atomic.Uint64

	// Drain goroutine: the only place cb is invoked, so a slow cb never stalls
	// the netlink read loop below (mirrors real_traffic_log.go watchGroup).
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-ch:
				cb(f)
			case <-ticker.C:
				d, e, rl := dropped.Swap(0), enobufs.Swap(0), rateLimited.Swap(0)
				if d > 0 || e > 0 || rl > 0 {
					log.Printf("[RealTrafficAccounting] conntrack DESTROY events in the last 30s: %d dropped (channel full), %d rate-limited, %d ENOBUFS (kernel outran reader)", d, rl, e)
				}
			}
		}
	}()

	limiter := &perSecondLimiter{}
	log.Printf("[RealTrafficAccounting] Listening for conntrack DESTROY events (NFNLGRP_CONNTRACK_DESTROY)")

	for {
		msgs, _, err := sock.Receive()
		if err != nil {
			if ctx.Err() != nil {
				// Socket was closed intentionally because ctx was cancelled.
				return nil
			}
			if errors.Is(err, unix.ENOBUFS) {
				// Kernel outran our reader — some events were lost, but this
				// must never be treated as fatal (plan Caution 4): losing a
				// handful of teardown byte-counts only slightly under-counts
				// this poll cycle, whereas exiting the goroutine would lose
				// event-based accounting entirely and silently, forever.
				enobufs.Add(1)
				continue
			}
			return fmt.Errorf("conntrack DESTROY event read failed: %w", err)
		}

		for _, msg := range msgs {
			sample, ok := safeParseConntrackDestroy(msg)
			if !ok {
				continue
			}
			if !limiter.allow(maxConntrackEventsPerSecond) {
				rateLimited.Add(1)
				continue
			}
			select {
			case ch <- sample:
			default:
				dropped.Add(1)
			}
		}
	}
}

// perSecondLimiter is a minimal fixed-window rate limiter: allow returns
// false once more than max calls have been made within the current wall-clock
// second. Safe for concurrent use, though WatchFlowEnd only calls it from its
// single read-loop goroutine.
type perSecondLimiter struct {
	mu     sync.Mutex
	second int64
	count  int
}

func (l *perSecondLimiter) allow(max int) bool {
	now := time.Now().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now != l.second {
		l.second = now
		l.count = 0
	}
	l.count++
	return l.count <= max
}

// safeParseConntrackDestroy decodes one raw netlink message into a
// model.FlowSample, wrapped in a panic recovery guard: a hand-written byte
// parser reading kernel-controlled, attacker-influenceable payloads must
// degrade (skip this event) on any malformed/truncated input, never crash
// the whole pigate process (plan Caution 5, mirrors safeConntrackList in
// real_traffic_account.go).
func safeParseConntrackDestroy(msg syscall.NetlinkMessage) (sample model.FlowSample, ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RealTrafficAccounting] recovered from panic while parsing conntrack DESTROY event: %v", rec)
			ok = false
		}
	}()

	if msg.Header.Type != nfnlMsgCtDelete {
		return model.FlowSample{}, false
	}
	if len(msg.Data) < nl.SizeofNfgenmsg {
		return model.FlowSample{}, false
	}

	family := msg.Data[0]
	attrs, err := nl.ParseRouteAttr(msg.Data[nl.SizeofNfgenmsg:])
	if err != nil {
		return model.FlowSample{}, false
	}

	var (
		srcIP, dstIP          net.IP
		proto                 uint8
		srcPort, dstPort      uint16
		haveTuple             bool
		origBytes, replyBytes uint64
	)

	for _, a := range attrs {
		switch attrType(a) {
		case nl.CTA_TUPLE_ORIG:
			ip, pr, sp, dp, ok := parseCtaTupleIP(a.Value)
			if ok {
				srcIP, dstIP, proto, srcPort, dstPort = ip.src, ip.dst, pr, sp, dp
				haveTuple = true
			}
		case nl.CTA_COUNTERS_ORIG:
			origBytes = parseCtaCountersBytes(a.Value)
		case nl.CTA_COUNTERS_REPLY:
			replyBytes = parseCtaCountersBytes(a.Value)
		}
	}

	if !haveTuple || srcIP == nil || dstIP == nil {
		return model.FlowSample{}, false
	}

	key := flowKeyFromParts(family, proto, srcIP.String(), srcPort, dstIP.String(), dstPort)
	return model.FlowSample{
		Key:        key,
		SrcIP:      srcIP.String(),
		DstIP:      dstIP.String(),
		Proto:      proto,
		DstPort:    dstPort,
		BytesOrig:  origBytes,
		BytesReply: replyBytes,
	}, true
}

// attrType masks off the NLA_F_NESTED/NLA_F_NET_BYTEORDER flag bits, mirroring
// how the kernel-attribute walk in the vishvananda/netlink library's own
// (unexported) parseRawData does it.
func attrType(a syscall.NetlinkRouteAttr) uint16 {
	return a.Attr.Type & nl.NLA_TYPE_MASK
}

type ctaTupleAddrs struct {
	src, dst net.IP
}

// parseCtaTupleIP walks one CTA_TUPLE_ORIG payload (a nested CTA_TUPLE_IP +
// CTA_TUPLE_PROTO pair) and extracts the 5-tuple. Returns ok=false if the
// nested attributes are missing or malformed, so the caller can skip this
// event rather than emit a half-populated FlowSample.
func parseCtaTupleIP(data []byte) (addrs ctaTupleAddrs, proto uint8, srcPort uint16, dstPort uint16, ok bool) {
	attrs, err := nl.ParseRouteAttr(data)
	if err != nil {
		return ctaTupleAddrs{}, 0, 0, 0, false
	}

	var haveIP bool
	for _, a := range attrs {
		switch attrType(a) {
		case nl.CTA_TUPLE_IP:
			ipAttrs, err := nl.ParseRouteAttr(a.Value)
			if err != nil {
				continue
			}
			for _, ipa := range ipAttrs {
				switch attrType(ipa) {
				case nl.CTA_IP_V4_SRC:
					if len(ipa.Value) >= 4 {
						addrs.src = net.IP(ipa.Value[:4])
						haveIP = true
					}
				case nl.CTA_IP_V4_DST:
					if len(ipa.Value) >= 4 {
						addrs.dst = net.IP(ipa.Value[:4])
					}
				case nl.CTA_IP_V6_SRC:
					if len(ipa.Value) >= 16 {
						addrs.src = net.IP(ipa.Value[:16])
						haveIP = true
					}
				case nl.CTA_IP_V6_DST:
					if len(ipa.Value) >= 16 {
						addrs.dst = net.IP(ipa.Value[:16])
					}
				}
			}
		case nl.CTA_TUPLE_PROTO:
			protoAttrs, err := nl.ParseRouteAttr(a.Value)
			if err != nil {
				continue
			}
			for _, pa := range protoAttrs {
				switch attrType(pa) {
				case nl.CTA_PROTO_NUM:
					if len(pa.Value) >= 1 {
						proto = pa.Value[0]
					}
				case nl.CTA_PROTO_SRC_PORT:
					if len(pa.Value) >= 2 {
						srcPort = binary.BigEndian.Uint16(pa.Value[:2])
					}
				case nl.CTA_PROTO_DST_PORT:
					if len(pa.Value) >= 2 {
						dstPort = binary.BigEndian.Uint16(pa.Value[:2])
					}
				}
			}
		}
	}

	if !haveIP || addrs.dst == nil {
		return ctaTupleAddrs{}, 0, 0, 0, false
	}
	return addrs, proto, srcPort, dstPort, true
}

// parseCtaCountersBytes reads CTA_COUNTERS_BYTES out of a nested
// CTA_COUNTERS_ORIG/CTA_COUNTERS_REPLY payload. Returns 0 (not an error) when
// the sub-attribute is absent — e.g. `net.netfilter.nf_conntrack_acct=0`
// yields a DESTROY event with no byte counters at all, same degrade-to-zero
// behavior as DumpFlows (see TrafficAccountingManager doc comment).
func parseCtaCountersBytes(data []byte) uint64 {
	attrs, err := nl.ParseRouteAttr(data)
	if err != nil {
		return 0
	}
	for _, a := range attrs {
		if attrType(a) == nl.CTA_COUNTERS_BYTES && len(a.Value) >= 8 {
			return binary.BigEndian.Uint64(a.Value[:8])
		}
	}
	return 0
}
