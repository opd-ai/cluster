// Package discovery provides peer discovery via UDP multicast beacons.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/opd-ai/cluster/internal/nodeapi"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// Listener receives UDP multicast discovery beacons from peer nodes.
type Listener struct {
	conn  *ipv4.PacketConn
	done  chan struct{}
	msgCh chan nodeapi.BeaconMessage
	seen  map[string]seenEntry // address -> {seq, timestamp} for dedup
}

// seenEntry tracks a beacon by sequence and last-seen time.
type seenEntry struct {
	seq       int
	timestamp time.Time
}

// NewListener creates a new beacon listener on the multicast group.
func NewListener(bufferSize int) (*Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var setSockOptErr error
			err := c.Control(func(fd uintptr) {
				setSockOptErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
			if err != nil {
				return err
			}
			return setSockOptErr
		},
	}

	addrStr := fmt.Sprintf("0.0.0.0:%d", MulticastPort)
	pc, err := lc.ListenPacket(context.Background(), "udp", addrStr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}

	conn, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return nil, fmt.Errorf("unexpected PacketConn type")
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

	if bufferSize <= 0 {
		bufferSize = 100
	}

	return &Listener{
		conn:  packetConn,
		done:  make(chan struct{}),
		msgCh: make(chan nodeapi.BeaconMessage, bufferSize),
		seen:  make(map[string]seenEntry),
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
	// Run cleanup periodically (every 5 minutes) to remove old entries.
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-l.done:
			return
		case <-cleanupTicker.C:
			// Clean up entries older than 30 minutes
			l.cleanupOldEntries(30 * time.Minute)
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
		entry, exists := l.seen[msg.Address]
		if exists && entry.seq >= msg.SeqNum {
			// Already seen this or a newer beacon from this address
			continue
		}

		l.seen[msg.Address] = seenEntry{seq: msg.SeqNum, timestamp: time.Now()}

		// Send on channel; if buffer is full, drop the oldest message to make room.
		select {
		case l.msgCh <- msg:
		default:
			// Drop the oldest message and retry with the new one.
			select {
			case <-l.msgCh:
			default:
			}
			select {
			case l.msgCh <- msg:
			default:
			}
		}
	}
}

// cleanupOldEntries removes seen entries older than the specified duration.
func (l *Listener) cleanupOldEntries(maxAge time.Duration) {
	now := time.Now()
	for addr, entry := range l.seen {
		if now.Sub(entry.timestamp) > maxAge {
			delete(l.seen, addr)
		}
	}
}
