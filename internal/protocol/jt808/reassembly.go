package jt808

import (
	"sync"
	"time"
)

// DefaultReassemblyTTL bounds how long an incomplete subpackaged message is
// held in memory before being dropped, so a terminal that never finishes
// sending every packet can't leak memory indefinitely.
const DefaultReassemblyTTL = 2 * time.Minute

type reassemblyKey struct {
	TerminalSN    string
	MessageID     uint16
	MessageSerial uint16
}

type reassemblyEntry struct {
	total    uint16
	packets  map[uint16]Frame
	lastSeen time.Time
}

// Reassembler joins subpackaged JT808 messages (the header's subpackage bit
// set, with total-packet-count and packet-serial fields) back into one
// logical Frame. It is intended to live for the lifetime of one connection
// — TerminalSN plus MessageID/MessageSerial is only unique within a
// session, not globally.
type Reassembler struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[reassemblyKey]*reassemblyEntry
}

func NewReassembler(ttl time.Duration) *Reassembler {
	if ttl <= 0 {
		ttl = DefaultReassemblyTTL
	}
	return &Reassembler{ttl: ttl, entries: make(map[reassemblyKey]*reassemblyEntry)}
}

// Push adds one packet of a (possibly subpackaged) message. If frame isn't
// subpackaged, it is returned unchanged and complete=true. Otherwise it is
// buffered; once every packet 1..TotalPackets has arrived, Push returns a
// single reassembled Frame (body = concatenation of each packet's body in
// packet-serial order) with complete=true. Duplicate packets overwrite the
// earlier copy rather than erroring. now is used to expire stale
// incomplete entries on every call, bounding memory use.
func (r *Reassembler) Push(frame Frame, now time.Time) (Frame, bool) {
	if !frame.HasSubpackage {
		return frame, true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.expireLocked(now)

	key := reassemblyKey{TerminalSN: frame.TerminalSN, MessageID: frame.MessageID, MessageSerial: frame.MessageSerial}
	entry, ok := r.entries[key]
	if !ok {
		entry = &reassemblyEntry{total: frame.TotalPackets, packets: make(map[uint16]Frame)}
		r.entries[key] = entry
	}
	entry.lastSeen = now
	entry.packets[frame.PacketSerial] = frame
	if frame.TotalPackets > entry.total {
		entry.total = frame.TotalPackets
	}

	if uint16(len(entry.packets)) < entry.total || entry.total == 0 {
		return Frame{}, false
	}

	var body []byte
	for serial := uint16(1); serial <= entry.total; serial++ {
		part, ok := entry.packets[serial]
		if !ok {
			return Frame{}, false // still missing a packet despite the count matching (shouldn't happen, but never assume)
		}
		body = append(body, part.Body...)
	}
	delete(r.entries, key)

	reassembled := frame
	reassembled.Body = body
	reassembled.HasSubpackage = false
	reassembled.PacketSerial = 0
	reassembled.TotalPackets = 0
	return reassembled, true
}

func (r *Reassembler) expireLocked(now time.Time) {
	for key, entry := range r.entries {
		if now.Sub(entry.lastSeen) > r.ttl {
			delete(r.entries, key)
		}
	}
}

// Pending reports how many incomplete logical messages are currently
// buffered, for tests and diagnostics.
func (r *Reassembler) Pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
