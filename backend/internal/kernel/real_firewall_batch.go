package kernel

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/google/nftables"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// --- Phase 1 of docs/ref/todo/nftables-sets-refactor-plan.md (issue #168) ---
//
// Two independent safety nets for one big ApplyRules batch: (1) a large
// netlink socket buffer so the kernel's per-message acks of a big batch do
// not overflow the receive queue (ENOBUFS) and the batch itself is not
// larger than the send buffer (EMSGSIZE), and (2) a whole-ruleset rule
// budget that rejects an oversized apply BEFORE anything is sent to the
// kernel, so the previously applied ruleset stays in place.

// nftSockBufBytes is the requested netlink send/receive buffer size. The
// kernel doubles the value for its own accounting (~64 MiB), but memory is
// only actually consumed while acks are queued.
const nftSockBufBytes = 32 << 20

var nftSockBufWarnOnce sync.Once

// nftSocketBufferOption is a nftables.SockOption that enlarges the netlink
// socket buffers. It is best-effort and ALWAYS returns nil: google/nftables
// runs SockOptions on every dial and turns an error into a failed Flush
// (conn.go), so a failure here would mean the firewall is not applied at all
// (plan Caution 3). Order: SO_RCVBUFFORCE/SO_SNDBUFFORCE (needs CAP_NET_ADMIN
// in the init user namespace — the pigate binary has it), then the plain
// SetReadBuffer/SetWriteBuffer (capped by net.core.rmem_max/wmem_max), then
// give up with a single log line.
func nftSocketBufferOption(c *netlink.Conn) error {
	rcvForced, sndForced := false, false
	if rc, err := c.SyscallConn(); err == nil {
		_ = rc.Control(func(fd uintptr) {
			rcvForced = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, nftSockBufBytes) == nil
			sndForced = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUFFORCE, nftSockBufBytes) == nil
		})
	}

	var rcvErr, sndErr error
	if !rcvForced {
		rcvErr = c.SetReadBuffer(nftSockBufBytes)
	}
	if !sndForced {
		sndErr = c.SetWriteBuffer(nftSockBufBytes)
	}
	if rcvErr != nil || sndErr != nil {
		nftSockBufWarnOnce.Do(func() {
			log.Printf("[RealFirewall] Warning: could not enlarge nftables netlink socket buffers (rcv: %v, snd: %v); continuing with kernel defaults", rcvErr, sndErr)
		})
	}
	return nil
}

// newFirewallConn opens the nftables connection ApplyRules uses. A package
// var so unit tests can substitute a nftables.WithTestDial connection.
var newFirewallConn = func() (*nftables.Conn, error) {
	return nftables.New(nftables.WithSockOptions(nftSocketBufferOption))
}

// ErrNftRuleBudgetExceeded is returned (wrapped) by ApplyRules when the
// ruleset it built holds more nft rules than max-total-nft-rules. Nothing has
// been flushed to the kernel when this is returned.
var ErrNftRuleBudgetExceeded = errors.New("nft rule budget exceeded")

// checkRuleBudget reports whether count nft rules fit in max.
func checkRuleBudget(count, max int) error {
	if count > max {
		return fmt.Errorf("%w: ruleset needs %d nft rules, limit is %d (config key %q)",
			ErrNftRuleBudgetExceeded, count, max, "max-total-nft-rules")
	}
	return nil
}

// nftBatch is the slice of *nftables.Conn the rule-building helpers need.
// *nftables.Conn satisfies it as-is; ApplyRules hands the helpers a
// countingBatch wrapping the real connection so every rule/set is counted.
type nftBatch interface {
	AddRule(*nftables.Rule) *nftables.Rule
	AddSet(*nftables.Set, []nftables.SetElement) error
}

// countingBatch forwards to inner and counts what passes through, so
// ApplyRules can enforce the whole-ruleset budget before the single Flush.
type countingBatch struct {
	inner nftBatch
	rules int
	sets  int
	elems int
}

func (c *countingBatch) AddRule(r *nftables.Rule) *nftables.Rule {
	c.rules++
	return c.inner.AddRule(r)
}

func (c *countingBatch) AddSet(s *nftables.Set, vals []nftables.SetElement) error {
	if err := c.inner.AddSet(s, vals); err != nil {
		return err
	}
	c.sets++
	c.elems += len(vals)
	return nil
}
