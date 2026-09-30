package kernel

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"

	"pigate/internal/model"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
)

// --- Phase 2 of docs/ref/todo/nftables-sets-refactor-plan.md (issue #168) ---
//
// Set-mode expansion of user policy rules. Instead of the cartesian
// src x dst x svc x iif x oif expansion of addUserChainRules (thousands of nft
// rules for a few policies), every multi-value "dimension" of one PolicyRule
// collapses into an anonymous nftables set matched with a single expr.Lookup,
// so one PolicyRule becomes 1 nft rule per chain (2 when its services mix
// "protocol only" with "protocol + port"). Match semantics are the union
// semantics of the cartesian expansion:
//
//	iif in I  &&  oif in O  &&  src in (union S)  &&  dst in (union D)  &&  svc in (union V)
//
// Payload loads are deliberately identical to the legacy path (IP at network
// header offset 12/16, protocol at network header offset 9 — NOT meta l4proto,
// dport at transport header offset 2): switching to meta l4proto would change
// how IPv6 packets in the inet table behave (plan section 3.3 / Caution 7).
//
// Anonymous sets bind to exactly one lookup in the kernel, so every emitted
// nft rule builds its own fresh sets. Intervals must not overlap, so they are
// merged here in pure Go before AddSet. A dimension with no usable entry makes
// the policy emit nothing in that chain (fail closed) — never an empty set and
// never "no condition".

// interval is an inclusive uint32 range; used for IPv4 addresses and (widened)
// for ports.
type interval struct {
	start, end uint32
}

// mergeIntervals sorts and merges overlapping AND adjacent intervals
// (x..y, y+1..z => x..z) so neither the rbtree nor the pipapo backend ever
// sees overlapping ranges or an end-element key that collides with the next
// start-element key. Uses uint64 arithmetic to stay safe at 0xFFFFFFFF.
// Intervals with start > end are dropped.
func mergeIntervals(in []interval) []interval {
	s := make([]interval, 0, len(in))
	for _, iv := range in {
		if iv.start <= iv.end {
			s = append(s, iv)
		}
	}
	if len(s) == 0 {
		return nil
	}
	sort.Slice(s, func(i, j int) bool {
		if s[i].start != s[j].start {
			return s[i].start < s[j].start
		}
		return s[i].end < s[j].end
	})
	out := []interval{s[0]}
	for _, iv := range s[1:] {
		cur := &out[len(out)-1]
		if uint64(iv.start) <= uint64(cur.end)+1 {
			if iv.end > cur.end {
				cur.end = iv.end
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// addrComboInterval converts one addrCombo into the inclusive IPv4 interval it
// matches, mirroring buildIPMatchExpressions/ipMatchExprsForCombo parsing
// exactly (missing "/" => /32, host bits masked, IPv6 rejected, range parts
// TrimSpace'd). nonEmpty=false means the combo can never match a packet (range
// with start > end) and must be dropped; err means the entry is unusable and
// must be skipped (logged by the caller).
func addrComboInterval(c addrCombo) (iv interval, nonEmpty bool, err error) {
	if c.resolvedFQDNIP != nil {
		ip := c.resolvedFQDNIP.To4()
		if ip == nil {
			return interval{}, false, fmt.Errorf("resolved FQDN address %q is not IPv4", c.resolvedFQDNIP)
		}
		v := binary.BigEndian.Uint32(ip)
		return interval{v, v}, true, nil
	}
	entry := c.entry
	switch entry.Type {
	case "subnet":
		val := strings.TrimSpace(entry.Value)
		if !strings.Contains(val, "/") {
			val += "/32"
		}
		_, ipNet, perr := net.ParseCIDR(val)
		if perr != nil {
			return interval{}, false, fmt.Errorf("invalid subnet value %q for address object %q: %w", entry.Value, c.objName, perr)
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil {
			return interval{}, false, fmt.Errorf("only IPv4 subnets are supported: %q (address object %q)", entry.Value, c.objName)
		}
		if len(ipNet.Mask) != 4 {
			return interval{}, false, fmt.Errorf("unsupported subnet mask length in %q (address object %q)", entry.Value, c.objName)
		}
		base := binary.BigEndian.Uint32(ip4)
		mask := binary.BigEndian.Uint32(ipNet.Mask)
		return interval{base & mask, (base & mask) | ^mask}, true, nil

	case "range":
		parts := strings.Split(entry.Value, "-")
		if len(parts) != 2 {
			return interval{}, false, fmt.Errorf("invalid range value %q for address object %q", entry.Value, c.objName)
		}
		startIP := net.ParseIP(strings.TrimSpace(parts[0])).To4()
		endIP := net.ParseIP(strings.TrimSpace(parts[1])).To4()
		if startIP == nil || endIP == nil {
			return interval{}, false, fmt.Errorf("invalid IP range %q for address object %q", entry.Value, c.objName)
		}
		s, e := binary.BigEndian.Uint32(startIP), binary.BigEndian.Uint32(endIP)
		if s > e {
			// Legacy emitted Gte(start)+Lte(end), which no packet satisfies.
			return interval{}, false, nil
		}
		return interval{s, e}, true, nil

	default:
		return interval{}, false, fmt.Errorf("unsupported address entry type %q for address object %q", entry.Type, c.objName)
	}
}

// svcAtom is one (protocol, destination-port) service match. anyPort means
// "this protocol, any port or no port check": ICMP, or a TCP/UDP spec the
// legacy builder leaves without a port match.
type svcAtom struct {
	proto      byte
	anyPort    bool
	start, end uint16
}

// svcComboAtom converts one svcCombo into a svcAtom, mirroring
// buildRuleExpressions' "Service / Protocol" block branch by branch,
// including its quirks (plan Caution 6 — do not normalize): a port spec of
// "", "-" or "1-65535" (after TrimSpace, literal) or one that splits into 3+
// parts on "-" is proto-only; "1 - 65535" is a real port range; 1 part is a
// single port; 2 parts is a range with TrimSpace'd ends. nonEmpty=false means
// start > end (can never match). A port outside 0..65535 is an error (the
// legacy builder byte-truncated it; validation never lets such data into the
// DB).
func svcComboAtom(vc svcCombo) (a svcAtom, nonEmpty bool, err error) {
	if !vc.hasFilter {
		return svcAtom{}, false, fmt.Errorf("ALL service has no atom")
	}
	switch vc.protocol {
	case "TCP":
		a.proto = 6
	case "UDP":
		a.proto = 17
	case "ICMP":
		a.proto = 1
		a.anyPort = true
		return a, true, nil
	default:
		return svcAtom{}, false, fmt.Errorf("unsupported protocol %q for service %q", vc.protocol, vc.objName)
	}

	portStr := strings.TrimSpace(vc.port)
	if portStr == "" || portStr == "-" || portStr == "1-65535" {
		a.anyPort = true
		return a, true, nil
	}
	parts := strings.Split(portStr, "-")
	switch len(parts) {
	case 1:
		n, perr := strconv.Atoi(parts[0])
		if perr != nil {
			return svcAtom{}, false, fmt.Errorf("invalid port %q: %w", parts[0], perr)
		}
		if n < 0 || n > 65535 {
			return svcAtom{}, false, fmt.Errorf("port %d out of range for service %q", n, vc.objName)
		}
		a.start, a.end = uint16(n), uint16(n)
		return a, true, nil
	case 2:
		s, perr := strconv.Atoi(strings.TrimSpace(parts[0]))
		if perr != nil {
			return svcAtom{}, false, fmt.Errorf("invalid start port %q: %w", parts[0], perr)
		}
		e, perr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if perr != nil {
			return svcAtom{}, false, fmt.Errorf("invalid end port %q: %w", parts[1], perr)
		}
		if s < 0 || s > 65535 || e < 0 || e > 65535 {
			return svcAtom{}, false, fmt.Errorf("port range %d-%d out of range for service %q", s, e, vc.objName)
		}
		if s > e {
			return svcAtom{}, false, nil
		}
		a.start, a.end = uint16(s), uint16(e)
		return a, true, nil
	default:
		// 3+ parts: the legacy builder falls through with no port match
		// (proto only). Mirrored, not normalized.
		a.anyPort = true
		return a, true, nil
	}
}

// --- element encoders ---

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// encodeIPv4IntervalElems encodes merged, ascending IPv4 intervals the way the
// nft CLI does for an rbtree interval set: a leading {0.0.0.0, end} element iff
// the first interval does not start at 0, then per interval its start element
// and — unless it ends at 255.255.255.255 — an end element holding end+1.
func encodeIPv4IntervalElems(ivs []interval) []nftables.SetElement {
	if len(ivs) == 0 {
		return nil
	}
	elems := make([]nftables.SetElement, 0, 2*len(ivs)+1)
	if ivs[0].start != 0 {
		elems = append(elems, nftables.SetElement{Key: be32(0), IntervalEnd: true})
	}
	for _, iv := range ivs {
		elems = append(elems, nftables.SetElement{Key: be32(iv.start)})
		if iv.end != 0xFFFFFFFF {
			elems = append(elems, nftables.SetElement{Key: be32(iv.end + 1), IntervalEnd: true})
		}
	}
	return elems
}

// protoPorts is one protocol's merged, ascending destination-port intervals.
type protoPorts struct {
	proto byte
	ports []interval
}

// concatKey builds one 8-byte "inet_proto . inet_service" concat key: the
// proto padded to a 4-byte register, then the big-endian port padded to 4.
func concatKey(proto byte, port uint32) []byte {
	return []byte{proto, 0, 0, 0, byte(port >> 8), byte(port), 0, 0}
}

// encodeProtoPortElems encodes per-protocol port intervals for the pipapo
// concat set: one element per interval with Key = proto . start and KeyEnd =
// proto . end (KeyEnd always sent, even when start == end).
func encodeProtoPortElems(groups []protoPorts) []nftables.SetElement {
	var elems []nftables.SetElement
	for _, g := range groups {
		for _, iv := range g.ports {
			elems = append(elems, nftables.SetElement{
				Key:    concatKey(g.proto, iv.start),
				KeyEnd: concatKey(g.proto, iv.end),
			})
		}
	}
	return elems
}

// encodeIfnameElems encodes interface names (deduplicated, order kept) as
// 16-byte padded ifname keys.
func encodeIfnameElems(names []string) []nftables.SetElement {
	seen := make(map[string]bool, len(names))
	elems := make([]nftables.SetElement, 0, len(names))
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		elems = append(elems, nftables.SetElement{Key: padInterfaceName(n)})
	}
	return elems
}

// encodeProtoElems encodes IP protocol numbers (deduplicated, ascending).
func encodeProtoElems(protos []byte) []nftables.SetElement {
	seen := make(map[byte]bool, len(protos))
	var uniq []byte
	for _, p := range protos {
		if !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i] < uniq[j] })
	elems := make([]nftables.SetElement, len(uniq))
	for i, p := range uniq {
		elems[i] = nftables.SetElement{Key: []byte{p}}
	}
	return elems
}

// --- anonymous set constructors (always Anonymous+Constant) ---

var protoPortSetType = nftables.MustConcatSetType(nftables.TypeInetProto, nftables.TypeInetService)

func newAnonIPv4Set(t *nftables.Table) *nftables.Set {
	return &nftables.Set{
		Table: t, Anonymous: true, Constant: true, Interval: true,
		KeyType: nftables.TypeIPAddr, KeyByteOrder: binaryutil.BigEndian,
	}
}

func newAnonProtoPortSet(t *nftables.Table) *nftables.Set {
	return &nftables.Set{
		Table: t, Anonymous: true, Constant: true, Interval: true, Concatenation: true,
		KeyType: protoPortSetType, KeyByteOrder: binaryutil.BigEndian,
	}
}

func newAnonProtoSet(t *nftables.Table) *nftables.Set {
	return &nftables.Set{
		Table: t, Anonymous: true, Constant: true,
		KeyType: nftables.TypeInetProto, KeyByteOrder: binaryutil.BigEndian,
	}
}

func newAnonIfnameSet(t *nftables.Table) *nftables.Set {
	return &nftables.Set{
		Table: t, Anonymous: true, Constant: true,
		KeyType: nftables.TypeIFName, KeyByteOrder: binaryutil.BigEndian,
	}
}

// addAnonSet adds set with its elements to b and returns the Lookup that
// references it. The Lookup MUST be built after AddSet returns: AddSet is what
// assigns the set's ID and "__set%d" name.
func addAnonSet(b nftBatch, set *nftables.Set, elems []nftables.SetElement) (*expr.Lookup, error) {
	if len(elems) == 0 {
		// Fail closed: never emit an empty set.
		return nil, fmt.Errorf("refusing to emit an empty nft set")
	}
	if err := b.AddSet(set, elems); err != nil {
		return nil, fmt.Errorf("add nft set: %w", err)
	}
	return &expr.Lookup{SourceRegister: 1, SetName: set.Name, SetID: set.ID}, nil
}

// --- dimension collection ---

// addrAtom is one usable address entry of a dimension.
type addrAtom struct {
	combo addrCombo
	iv    interval
}

// addrDim is one address dimension (source or destination) of a policy rule.
// any means an "ALL"/"" name is present: no condition at all.
type addrDim struct {
	any   bool
	atoms []addrAtom
}

func (d addrDim) usable() bool { return d.any || len(d.atoms) > 0 }

func addrAtomKey(c addrCombo) string {
	return c.entry.Type + "|" + c.entry.Value + "|" + c.resolvedFQDNIP.String()
}

// collectAddrDim resolves every name of one address list (source or
// destination) through addressCombos — so FQDN entries are resolved and
// recorded for every name — into deduplicated atoms. Unknown names and
// unusable entries are skipped individually with a log line (legacy
// behavior); it never returns an error.
func collectAddrDim(dimName, chainName string, r model.PolicyRule, names []string,
	addrsMap map[string]model.AddressObject, fqdnRec *fqdnRecorder) addrDim {
	var d addrDim
	seen := map[string]bool{}
	for _, name := range names {
		combos, err := addressCombos(name, addrsMap, fqdnRec)
		if err != nil {
			log.Printf("[RealFirewall] Skip %s rule %q %s %q: %v", chainName, r.Name, dimName, name, err)
			continue
		}
		for _, c := range combos {
			if !c.hasFilter {
				d.any = true
				continue
			}
			iv, nonEmpty, err := addrComboInterval(c)
			if err != nil {
				log.Printf("[RealFirewall] Skip %s rule %q %s entry %s: %v", chainName, r.Name, dimName, comboDesc(c), err)
				continue
			}
			if !nonEmpty {
				log.Printf("[RealFirewall] Skip %s rule %q %s entry %s: empty address range (start > end)", chainName, r.Name, dimName, comboDesc(c))
				continue
			}
			k := addrAtomKey(c)
			if seen[k] {
				continue
			}
			seen[k] = true
			d.atoms = append(d.atoms, addrAtom{combo: c, iv: iv})
		}
	}
	return d
}

// svcEntry is one usable service entry of the service dimension.
type svcEntry struct {
	combo svcCombo
	atom  svcAtom
}

// svcDim is the service dimension of a policy rule; any means "ALL"/"".
type svcDim struct {
	any   bool
	atoms []svcEntry
}

func (d svcDim) usable() bool { return d.any || len(d.atoms) > 0 }

func svcAtomKey(a svcAtom) string {
	if a.anyPort {
		return fmt.Sprintf("%d|any", a.proto)
	}
	return fmt.Sprintf("%d|%d-%d", a.proto, a.start, a.end)
}

// collectSvcDim is the service counterpart of collectAddrDim.
func collectSvcDim(chainName string, r model.PolicyRule, names []string,
	svcsMap map[string]model.ServiceObject) svcDim {
	var d svcDim
	seen := map[string]bool{}
	for _, name := range names {
		combos, err := serviceCombos(name, svcsMap)
		if err != nil {
			log.Printf("[RealFirewall] Skip %s rule %q service %q: %v", chainName, r.Name, name, err)
			continue
		}
		for _, c := range combos {
			if !c.hasFilter {
				d.any = true
				continue
			}
			a, nonEmpty, err := svcComboAtom(c)
			if err != nil {
				log.Printf("[RealFirewall] Skip %s rule %q service entry %s: %v", chainName, r.Name, svcComboDesc(c), err)
				continue
			}
			if !nonEmpty {
				log.Printf("[RealFirewall] Skip %s rule %q service entry %s: empty port range (start > end)", chainName, r.Name, svcComboDesc(c))
				continue
			}
			k := svcAtomKey(a)
			if seen[k] {
				continue
			}
			seen[k] = true
			d.atoms = append(d.atoms, svcEntry{combo: c, atom: a})
		}
	}
	return d
}

// ifaceMatchNames applies the chain scoping (input ignores out interfaces,
// output ignores in interfaces — done by the caller passing nil) and the legacy
// normalizeIfaceMatchList, then deduplicates. An empty result means "no
// interface condition".
func ifaceMatchNames(names []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range normalizeIfaceMatchList(names) {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// --- match expression builders (each may add sets to the batch) ---

func ifaceSetMatchExprs(b nftBatch, table *nftables.Table, key expr.MetaKey, names []string) ([]expr.Any, error) {
	switch len(names) {
	case 0:
		return nil, nil
	case 1:
		return []expr.Any{
			&expr.Meta{Key: key, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(names[0])},
		}, nil
	}
	lk, err := addAnonSet(b, newAnonIfnameSet(table), encodeIfnameElems(names))
	if err != nil {
		return nil, err
	}
	return []expr.Any{&expr.Meta{Key: key, Register: 1}, lk}, nil
}

func addrSetMatchExprs(b nftBatch, table *nftables.Table, d addrDim, offset uint32) ([]expr.Any, error) {
	if d.any {
		return nil, nil
	}
	if len(d.atoms) == 1 {
		return ipMatchExprsForCombo(d.atoms[0].combo, offset)
	}
	ivs := make([]interval, len(d.atoms))
	for i, a := range d.atoms {
		ivs[i] = a.iv
	}
	lk, err := addAnonSet(b, newAnonIPv4Set(table), encodeIPv4IntervalElems(mergeIntervals(ivs)))
	if err != nil {
		return nil, err
	}
	return []expr.Any{
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: 4},
		lk,
	}, nil
}

func protoLoadExpr(dest uint32) *expr.Payload {
	return &expr.Payload{DestRegister: dest, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1}
}

func dportExprs(start, end uint32) []expr.Any {
	load := &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2}
	if start == end {
		return []expr.Any{load, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portToBytes(int(start))}}
	}
	return []expr.Any{
		load,
		&expr.Cmp{Op: expr.CmpOpGte, Register: 1, Data: portToBytes(int(start))},
		&expr.Cmp{Op: expr.CmpOpLte, Register: 1, Data: portToBytes(int(end))},
	}
}

// svcVariantBuilder builds the service match exprs of one emitted nft rule
// (adding that rule's own sets to the batch).
type svcVariantBuilder func(b nftBatch, table *nftables.Table) ([]expr.Any, error)

// svcVariants turns the service dimension into the list of nft-rule variants
// (plan section 3.3 "service split"): ALL => one variant without exprs; a
// single atom => the legacy exprs byte-for-byte; otherwise variant A (protocols
// matched with no port check, from anyPort atoms) and/or variant B (proto+port
// atoms whose protocol is not already covered by A).
func svcVariants(d svcDim) []svcVariantBuilder {
	if d.any {
		return []svcVariantBuilder{func(nftBatch, *nftables.Table) ([]expr.Any, error) { return nil, nil }}
	}
	if len(d.atoms) == 1 {
		vc := d.atoms[0].combo
		return []svcVariantBuilder{func(nftBatch, *nftables.Table) ([]expr.Any, error) { return svcComboMatchExprs(vc) }}
	}

	var protosP []byte
	inP := map[byte]bool{}
	for _, e := range d.atoms {
		if e.atom.anyPort && !inP[e.atom.proto] {
			inP[e.atom.proto] = true
			protosP = append(protosP, e.atom.proto)
		}
	}
	sort.Slice(protosP, func(i, j int) bool { return protosP[i] < protosP[j] })

	byProto := map[byte][]interval{}
	for _, e := range d.atoms {
		if e.atom.anyPort || inP[e.atom.proto] {
			continue
		}
		byProto[e.atom.proto] = append(byProto[e.atom.proto], interval{uint32(e.atom.start), uint32(e.atom.end)})
	}
	var groups []protoPorts
	total := 0
	for proto, ivs := range byProto {
		merged := mergeIntervals(ivs)
		groups = append(groups, protoPorts{proto: proto, ports: merged})
		total += len(merged)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].proto < groups[j].proto })

	var out []svcVariantBuilder
	if len(protosP) > 0 {
		out = append(out, func(b nftBatch, table *nftables.Table) ([]expr.Any, error) {
			if len(protosP) == 1 {
				return []expr.Any{
					protoLoadExpr(1),
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{protosP[0]}},
				}, nil
			}
			lk, err := addAnonSet(b, newAnonProtoSet(table), encodeProtoElems(protosP))
			if err != nil {
				return nil, err
			}
			return []expr.Any{protoLoadExpr(1), lk}, nil
		})
	}
	if total > 0 {
		out = append(out, func(b nftBatch, table *nftables.Table) ([]expr.Any, error) {
			if total == 1 {
				g := groups[0]
				exprs := []expr.Any{
					protoLoadExpr(1),
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{g.proto}},
				}
				return append(exprs, dportExprs(g.ports[0].start, g.ports[0].end)...), nil
			}
			lk, err := addAnonSet(b, newAnonProtoPortSet(table), encodeProtoPortElems(groups))
			if err != nil {
				return nil, err
			}
			// proto -> reg1 (NFT_REG32_00), dport -> reg 9 (NFT_REG32_01, the
			// next 4 bytes); a payload shorter than 4 bytes is zero-padded in
			// the register, matching the concat key layout.
			return []expr.Any{
				protoLoadExpr(1),
				&expr.Payload{DestRegister: 9, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				lk,
			}, nil
		})
	}
	return out
}

// addUserChainRulesSets is the set-mode twin of addUserChainRules (same
// parameters and return value): it appends, for every enabled PolicyRule of
// chainName, 1 (or 2) nft rules built from anonymous sets instead of the
// cartesian expansion, and returns how many nft rules it emitted. maxAtoms
// (the max-expanded-rules-per-policy config value) bounds the number of
// distinct atoms in any single dimension; a policy above it is skipped in this
// chain with a warning rather than truncated (a silently half-filled set would
// be an invisible change of meaning). It never returns an error.
func addUserChainRulesSets(
	b nftBatch,
	table *nftables.Table,
	nfChain *nftables.Chain,
	chainName string,
	rules []model.PolicyRule,
	addrsMap map[string]model.AddressObject,
	svcsMap map[string]model.ServiceObject,
	acceptLogPrefix, dropLogPrefix string,
	maxAtoms int,
	fqdnRec *fqdnRecorder,
) int {
	emitted := 0
	for _, r := range rules {
		if !r.Status || r.Chain != chainName {
			continue
		}

		// Same guard as addUserChainRules: InInterfaces/OutInterfaces must be
		// populated before use.
		model.NormalizePolicyRuleInterfaces(&r)

		// Tag every nft rule with the DB rule id (traffic-detail collector sums
		// the per-rule counters back by it). Identical to the legacy path.
		ruleUserData := userdata.AppendString(nil, userdata.TypeComment, r.ID)

		sources := r.Source
		if len(sources) == 0 {
			sources = []string{"ALL"}
		}
		destinations := r.Destination
		if len(destinations) == 0 {
			destinations = []string{"ALL"}
		}
		services := r.Service
		if len(services) == 0 {
			services = []string{"ALL"}
		}

		// Resolve all three dimensions unconditionally so every referenced
		// FQDN is recorded for the refresher, even if another dimension turns
		// out empty.
		src := collectAddrDim("source", chainName, r, sources, addrsMap, fqdnRec)
		dst := collectAddrDim("destination", chainName, r, destinations, addrsMap, fqdnRec)
		svc := collectSvcDim(chainName, r, services, svcsMap)

		effIn, effOut := r.InInterfaces, r.OutInterfaces
		if chainName == model.PolicyChainInput {
			effOut = nil
		}
		if chainName == model.PolicyChainOutput {
			effIn = nil
		}
		inNames := ifaceMatchNames(effIn)
		outNames := ifaceMatchNames(effOut)

		// Fail closed: a dimension with no usable entry (and no ALL) matches
		// nothing today (0 combos => 0 rules), so emit nothing — never an
		// empty set and never "no condition".
		var empty string
		switch {
		case !src.usable():
			empty = "source"
		case !dst.usable():
			empty = "destination"
		case !svc.usable():
			empty = "service"
		}
		if empty != "" {
			log.Printf("[RealFirewall] Skip %s rule %q (id=%s): its %s list has no usable entry, emitting no nft rule for it",
				chainName, r.Name, r.ID, empty)
			continue
		}

		// Defense in depth (plan D-6): atom count per dimension vs the
		// max-expanded-rules-per-policy cap. Skip the whole policy, never
		// truncate a set.
		// A dimension that is "ALL" has no atoms worth counting.
		srcN, dstN, svcN := len(src.atoms), len(dst.atoms), len(svc.atoms)
		if src.any {
			srcN = 0
		}
		if dst.any {
			dstN = 0
		}
		if svc.any {
			svcN = 0
		}
		if n := maxInt(srcN, dstN, svcN, len(inNames), len(outNames)); n > maxAtoms {
			log.Printf("[RealFirewall] Warning: skipping %s policy rule %q (id=%s): a match list has %d distinct entries, above the limit %d — raise the %q config key to allow it",
				chainName, r.Name, r.ID, n, maxAtoms, "max-expanded-rules-per-policy")
			continue
		}

		logPrefix := acceptLogPrefix
		if r.Action == "DROP" {
			logPrefix = dropLogPrefix
		}
		logPrefix = withRuleToken(logPrefix, r.ID)

		for _, buildSvc := range svcVariants(svc) {
			// Each emitted nft rule gets fresh sets: an anonymous set binds to
			// exactly one lookup (plan Caution 4).
			var exprs []expr.Any
			parts := []func() ([]expr.Any, error){
				func() ([]expr.Any, error) { return ifaceSetMatchExprs(b, table, expr.MetaKeyIIFNAME, inNames) },
				func() ([]expr.Any, error) { return ifaceSetMatchExprs(b, table, expr.MetaKeyOIFNAME, outNames) },
				func() ([]expr.Any, error) { return addrSetMatchExprs(b, table, src, 12) },
				func() ([]expr.Any, error) { return addrSetMatchExprs(b, table, dst, 16) },
				func() ([]expr.Any, error) { return buildSvc(b, table) },
			}
			var err error
			for _, part := range parts {
				var e []expr.Any
				if e, err = part(); err != nil {
					break
				}
				exprs = append(exprs, e...)
			}
			if err != nil {
				log.Printf("[RealFirewall] Skip %s rule %q (id=%s): %v", chainName, r.Name, r.ID, err)
				continue
			}
			exprs = append(exprs, userRuleSuffixExprs(chainName, r.Action, r.Log, r.Nat, logPrefix)...)
			b.AddRule(&nftables.Rule{
				Table:    table,
				Chain:    nfChain,
				Exprs:    exprs,
				UserData: ruleUserData,
			})
			emitted++
		}
	}
	return emitted
}

func maxInt(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
