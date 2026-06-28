package service

import (
	"context"
	"log"
	"sync"
	"time"

	"pigate/internal/db"

	"github.com/vishvananda/netlink"
)

// debouncer aggregates network events and schedules reconciliation after a cooldown period.
type debouncer struct {
	mu       sync.Mutex
	timer    *time.Timer
	interval time.Duration
}

func newDebouncer(interval time.Duration) *debouncer {
	return &debouncer{
		interval: interval,
	}
}

func (d *debouncer) debounce(action func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.interval, action)
}

// NetlinkMonitor listens to Linux kernel networking events (link, address, route updates)
// and triggers static route reconciliation to synchronize kernel routes with the database configuration.
type NetlinkMonitor struct {
	repo           *db.Repository
	routingService *RoutingService
	dnsService     *DNSService
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

func NewNetlinkMonitor(repo *db.Repository, routingService *RoutingService, dnsService *DNSService) *NetlinkMonitor {
	return &NetlinkMonitor{
		repo:           repo,
		routingService: routingService,
		dnsService:     dnsService,
	}
}

// Start initiates netlink event listeners. Subscriptions are skipped if mock mode is active.
func (m *NetlinkMonitor) Start(ctx context.Context) {
	if m.repo.IsMockMode() {
		log.Printf("[NetlinkMonitor] Running in mock mode. Netlink subscriptions disabled.")
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	linkChan := make(chan netlink.LinkUpdate)
	addrChan := make(chan netlink.AddrUpdate)
	routeChan := make(chan netlink.RouteUpdate)

	done := ctx.Done()

	// 1. Subscribe to Link state events (e.g. interface up/down, plugged/unplugged)
	if err := netlink.LinkSubscribe(linkChan, done); err != nil {
		log.Printf("[NetlinkMonitor] Failed to subscribe to Netlink Link updates: %v", err)
		cancel()
		return
	}

	// 2. Subscribe to Address events (e.g. IP address configured/removed)
	if err := netlink.AddrSubscribe(addrChan, done); err != nil {
		log.Printf("[NetlinkMonitor] Failed to subscribe to Netlink Address updates: %v", err)
		cancel()
		return
	}

	// 3. Subscribe to Route changes (e.g. route created or deleted externally)
	if err := netlink.RouteSubscribe(routeChan, done); err != nil {
		log.Printf("[NetlinkMonitor] Failed to subscribe to Netlink Route updates: %v", err)
		cancel()
		return
	}

	log.Printf("[NetlinkMonitor] Successfully subscribed to Netlink updates (Link, Addr, Route)")

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		d := newDebouncer(500 * time.Millisecond)

		for {
			select {
			case linkUpdate, ok := <-linkChan:
				if !ok {
					return
				}
				log.Printf("[NetlinkMonitor] Received Link event: Index=%d, Name=%s, Flags=%v",
					linkUpdate.Index, linkUpdate.Attrs().Name, linkUpdate.Attrs().Flags)
				d.debounce(m.reconcile)

			case addrUpdate, ok := <-addrChan:
				if !ok {
					return
				}
				log.Printf("[NetlinkMonitor] Received Address event: LinkIndex=%d, Address=%s, NewAddr=%t",
					addrUpdate.LinkIndex, addrUpdate.LinkAddress.String(), addrUpdate.NewAddr)
				d.debounce(m.reconcile)

			case routeUpdate, ok := <-routeChan:
				if !ok {
					return
				}
				dstStr := "default"
				if routeUpdate.Dst != nil {
					dstStr = routeUpdate.Dst.String()
				}
				gwStr := "none"
				if routeUpdate.Gw != nil {
					gwStr = routeUpdate.Gw.String()
				}
				log.Printf("[NetlinkMonitor] Received Route event: Type=%d, Dst=%s, Gw=%s, Protocol=%d",
					routeUpdate.Type, dstStr, gwStr, routeUpdate.Protocol)
				d.debounce(m.reconcile)

			case <-done:
				log.Printf("[NetlinkMonitor] Netlink event loops terminated")
				return
			}
		}
	}()
}

func (m *NetlinkMonitor) reconcile() {
	log.Printf("[NetlinkMonitor] Network change/drift detected. Reconciling network routing...")
	if err := m.routingService.reconcileKernelRoutingTable(); err != nil {
		log.Printf("[NetlinkMonitor] Error reconciling routing table: %v", err)
	} else {
		log.Printf("[NetlinkMonitor] Routing table reconciliation completed successfully")
	}

	log.Printf("[NetlinkMonitor] Network change/drift detected. Reapplying DNS configurations...")
	if err := m.dnsService.ApplyDNSConfig(); err != nil {
		log.Printf("[NetlinkMonitor] Error applying DNS configurations: %v", err)
	} else {
		log.Printf("[NetlinkMonitor] DNS reconciliation completed successfully")
	}
}

// Stop halts the monitor loop and waits for resources to clean up.
func (m *NetlinkMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	log.Printf("[NetlinkMonitor] Netlink monitor stopped")
}
