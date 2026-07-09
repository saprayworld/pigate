package kernel

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
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
	// ForwardNflogSnaplen is the number of packet bytes the kernel copies per
	// event — enough for the IPv4/IPv6 header + TCP/UDP ports, not the payload.
	ForwardNflogSnaplen uint32 = 64
	// trafficLogChanSize bounds the in-process handoff buffer. On a traffic
	// burst, events beyond this are dropped (counted) rather than blocking the
	// netlink read loop — the Forward Traffic page is a recent-sample view, not
	// a complete record (see forward-traffic-log-plan.md §5.3).
	trafficLogChanSize = 256
)

// RealTrafficLog implements TrafficLogManager by subscribing to NFLOG group
// ForwardNflogGroup and parsing each event's packet header into a FirewallLog.
type RealTrafficLog struct{}

func NewRealTrafficLog() *RealTrafficLog {
	return &RealTrafficLog{}
}

// WatchForwardTraffic opens the NFLOG socket, then decouples the netlink read
// loop from the (potentially slow) consumer via a buffered channel drained by a
// separate goroutine. It blocks until ctx is cancelled.
func (r *RealTrafficLog) WatchForwardTraffic(ctx context.Context, cb func(model.FirewallLog)) error {
	cfg := &nflog.Config{
		Group:    ForwardNflogGroup,
		Copymode: nflog.CopyPacket,
		Bufsize:  ForwardNflogSnaplen,
	}
	nf, err := nflog.Open(cfg)
	if err != nil {
		return fmt.Errorf("failed to open NFLOG group %d: %w (requires CAP_NET_ADMIN)", ForwardNflogGroup, err)
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
					log.Printf("[RealTrafficLog] Dropped %d forward-traffic log events in the last 30s (burst overflow)", n)
				}
			}
		}
	}()

	hook := func(attr nflog.Attribute) int {
		entry, ok := parseNflogAttr(attr)
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
		log.Printf("[RealTrafficLog] NFLOG read error: %v", e)
		return 0
	}

	if err := nf.RegisterWithErrorFunc(ctx, hook, errFunc); err != nil {
		return fmt.Errorf("failed to register NFLOG hook: %w", err)
	}

	log.Printf("[RealTrafficLog] Listening for forward-chain packet logs on NFLOG group %d", ForwardNflogGroup)
	<-ctx.Done()
	return nil
}

// parseNflogAttr turns one NFLOG event into a FirewallLog. Time and ID are left
// blank — main.go stamps the timestamp when it pushes into the ring buffer.
// Returns ok=false when there is no packet payload to parse.
func parseNflogAttr(attr nflog.Attribute) (model.FirewallLog, bool) {
	if attr.Payload == nil {
		return model.FirewallLog{}, false
	}
	entry := parsePacketHeader(*attr.Payload)

	action, reason := "PASS", "Forwarded"
	if attr.Prefix != nil {
		prefix := strings.TrimSpace(*attr.Prefix)
		if strings.Contains(prefix, "DROP") {
			action, reason = "DROP", "Blocked (forward)"
		} else {
			action, reason = "PASS", "Allowed (forward)"
		}
	}
	entry.Action = action
	entry.Reason = reason
	return entry, true
}

// parsePacketHeader reads src/dst/proto/port out of a raw IPv4 or IPv6 packet
// header. It is defensive against short buffers (Snaplen may truncate the
// packet): any field it can't read is left as "-". The version nibble of the
// first byte selects IPv4 vs IPv6 (inet family — payload can be either).
func parsePacketHeader(pkt []byte) model.FirewallLog {
	entry := model.FirewallLog{Src: "-", Dest: "-", Port: "-", Proto: "-"}
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

	// Destination port for TCP/UDP only (offset 2 in the transport header).
	if (proto == 6 || proto == 17) && len(transport) >= 4 {
		entry.Port = fmt.Sprintf("%d", binary.BigEndian.Uint16(transport[2:4]))
	}

	return entry
}
