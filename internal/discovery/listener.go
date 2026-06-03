// Package discovery provides peer discovery via UDP multicast beacons.
package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/opd-ai/cluster/internal/nodeapi"
	"golang.org/x/net/ipv4"
)

// Listener receives UDP multicast discovery beacons from peer nodes.
type Listener struct {
	conn    *ipv4.PacketConn
	done    chan struct{}
	msgCh   chan nodeapi.BeaconMessage
	seen    map[string]int // address -> seq for dedup
}

// NewListener creates a new beacon listener on the multicast group.
func NewListener(bufferSize int) (*Listener, error) {
	addr := net.UDPAddr{
		Port: MulticastPort,
		IP:   net.ParseIP("0.0.0.0"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}

	packetConn := ipv4.NewPacketConn(conn)

	group := net.IPAddr{IP: net.ParseIP(MulticastAddr)}

	// Find a suitable network interface
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		// Fallback to first available interface if eth0 doesn't exist
		ifaces, err := net.Interfaces()
		if err != nil || len(ifaces) == 0 {
			conn.Close()
			return nil, fmt.Errorf("no network interfaces found")
		}
		for _, i := range ifaces {
			if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 {
				iface = &i
				break
			}
		}
		if iface == nil {
			conn.Close()
			return nil, fmt.Errorf("no suitable network interface found")
		}
	}

	err = packetConn.JoinGroup(iface, &group)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("join multicast group: %w", err)
	}

	if bufferSize == 0 {
		bufferSize = 100
	}

	return &Listener{
		conn:  packetConn,
		done:  make(chan struct{}),
		msgCh: make(chan nodeapi.BeaconMessage, bufferSize),
		seen:  make(map[string]int),
	}, nil
}

// MessagesCh returns the channel on which beacon messages are received.
// Deduplication is automatic; only new beacons are sent on the channel.
func (l *Listener) MessagesCh() <-chan nodeapi.BeaconMessage {
	return l.msgCh
}

// Start begins listening for beacon messages.
func (l *Listener) Start() {
	go l.run()
}

// Stop stops the beacon listener.
func (l *Listener) Stop() {
	close(l.done)
	if l.conn != nil {
		l.conn.Close()
	}
}

func (l *Listener) run() {
	defer close(l.msgCh)

	buf := make([]byte, 1024)
	for {
		select {
		case <-l.done:
			return
		default:
		}

		// Set read deadline to allow checking done channel
		_ = l.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _, _, err := l.conn.ReadFrom(buf)
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			// Other errors; silently continue
			continue
		}

		var msg nodeapi.BeaconMessage
		err = json.Unmarshal(buf[:n], &msg)
		if err != nil {
			// Invalid JSON; skip
			continue
		}

		// Deduplicate by address and seq number
		lastSeq, exists := l.seen[msg.Address]
		if exists && lastSeq >= msg.SeqNum {
			// Already seen this or a newer beacon from this address
			continue
		}

		l.seen[msg.Address] = msg.SeqNum

		// Send on channel (non-blocking; drop if buffer full)
		select {
		case l.msgCh <- msg:
		default:
			// Buffer full; drop oldest message and retry
		}
	}
}
