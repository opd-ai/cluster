// Package discovery provides peer discovery via UDP multicast beacons.
package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/opd-ai/cluster/internal/nodeapi"
)

const (
	// MulticastAddr is the multicast group address for node discovery.
	MulticastAddr = "239.77.0.1"
	// MulticastPort is the UDP port for discovery beacons.
	MulticastPort = 9977
)

// Beacon sends periodic UDP multicast discovery messages.
type Beacon struct {
	conn      *net.UDPConn
	done      chan struct{}
	interval  time.Duration
	msgFunc   func() nodeapi.BeaconMessage
}

// NewBeacon creates a new beacon sender on the multicast group.
func NewBeacon(interval time.Duration, msgFunc func() nodeapi.BeaconMessage) (*Beacon, error) {
	// Connect to the multicast address (for sending, we don't bind to the group)
	addr, err := net.ResolveUDPAddr("udp", MulticastAddr+":"+fmt.Sprintf("%d", MulticastPort))
	if err != nil {
		return nil, fmt.Errorf("resolve UDP addr: %w", err)
	}

	// Use a local address for binding
	localAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("resolve local addr: %w", err)
	}

	conn, err := net.DialUDP("udp", localAddr, addr)
	if err != nil {
		return nil, fmt.Errorf("dial UDP: %w", err)
	}

	return &Beacon{
		conn:     conn,
		done:     make(chan struct{}),
		interval: interval,
		msgFunc:  msgFunc,
	}, nil
}

// Start begins sending periodic beacon messages.
func (b *Beacon) Start() {
	go b.run()
}

// Stop stops the beacon sender.
func (b *Beacon) Stop() {
	close(b.done)
	if b.conn != nil {
		b.conn.Close()
	}
}

func (b *Beacon) run() {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			msg := b.msgFunc()
			data, err := json.Marshal(msg)
			if err != nil {
				// Silently skip on encode error; beacon is best-effort
				continue
			}

			_, err = b.conn.Write(data)
			if err != nil {
				// Silently skip on send error; beacon is best-effort
				continue
			}
		}
	}
}
