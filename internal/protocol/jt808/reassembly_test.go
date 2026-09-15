package jt808

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// buildSubpackagedFrame constructs one physical subpackaged frame's decoded
// Frame directly (bypassing the wire encoding, since EncodeFrame only
// builds simple non-subpackaged frames and the reassembler only cares about
// already-decoded Frame values).
func buildSubpackagedFrame(messageID uint16, terminalSN string, serial, total, packetSerial uint16, body []byte) Frame {
	return Frame{
		MessageID: messageID, HasSubpackage: true, TerminalSN: terminalSN,
		MessageSerial: serial, TotalPackets: total, PacketSerial: packetSerial, Body: body,
	}
}

func TestReassemblerNonSubpackagedFrameIsPassthrough(t *testing.T) {
	t.Parallel()
	r := NewReassembler(time.Minute)
	frame := Frame{MessageID: MsgLocationReport, HasSubpackage: false, Body: []byte{0x01}}
	got, complete := r.Push(frame, time.Now())
	if !complete {
		t.Fatal("expected a non-subpackaged frame to complete immediately")
	}
	if !bytes.Equal(got.Body, frame.Body) {
		t.Fatalf("Body = %x, want %x", got.Body, frame.Body)
	}
	if r.Pending() != 0 {
		t.Fatalf("expected 0 pending entries, got %d", r.Pending())
	}
}

func TestReassemblerInOrder(t *testing.T) {
	t.Parallel()
	r := NewReassembler(time.Minute)
	now := time.Now()

	part1 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 10, 2, 1, []byte{0x01, 0x02})
	part2 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 10, 2, 2, []byte{0x03, 0x04})

	if _, complete := r.Push(part1, now); complete {
		t.Fatal("expected incomplete after the first of two packets")
	}
	got, complete := r.Push(part2, now)
	if !complete {
		t.Fatal("expected complete after the second of two packets")
	}
	want := []byte{0x01, 0x02, 0x03, 0x04}
	if !bytes.Equal(got.Body, want) {
		t.Fatalf("reassembled Body = %x, want %x", got.Body, want)
	}
	if got.HasSubpackage {
		t.Fatal("reassembled frame should no longer report HasSubpackage")
	}
	if r.Pending() != 0 {
		t.Fatalf("expected the entry to be cleared after completion, got %d pending", r.Pending())
	}
}

func TestReassemblerOutOfOrder(t *testing.T) {
	t.Parallel()
	r := NewReassembler(time.Minute)
	now := time.Now()

	part3 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 11, 3, 3, []byte{0x05})
	part1 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 11, 3, 1, []byte{0x01})
	part2 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 11, 3, 2, []byte{0x03})

	r.Push(part3, now)
	r.Push(part1, now)
	got, complete := r.Push(part2, now)
	if !complete {
		t.Fatal("expected complete once all 3 out-of-order packets arrive")
	}
	want := []byte{0x01, 0x03, 0x05}
	if !bytes.Equal(got.Body, want) {
		t.Fatalf("reassembled Body = %x, want %x (must be in packet-serial order, not arrival order)", got.Body, want)
	}
}

func TestReassemblerDuplicatePacketOverwrites(t *testing.T) {
	t.Parallel()
	r := NewReassembler(time.Minute)
	now := time.Now()

	part1a := buildSubpackagedFrame(MsgLocationReport, "013800100034", 12, 2, 1, []byte{0xaa})
	part1b := buildSubpackagedFrame(MsgLocationReport, "013800100034", 12, 2, 1, []byte{0xbb}) // retransmitted packet 1
	part2 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 12, 2, 2, []byte{0xcc})

	r.Push(part1a, now)
	r.Push(part1b, now)
	got, complete := r.Push(part2, now)
	if !complete {
		t.Fatal("expected complete")
	}
	want := []byte{0xbb, 0xcc}
	if !bytes.Equal(got.Body, want) {
		t.Fatalf("expected the later duplicate to win: Body = %x, want %x", got.Body, want)
	}
}

func TestReassemblerExpiresIncompleteEntries(t *testing.T) {
	t.Parallel()
	r := NewReassembler(10 * time.Millisecond)
	start := time.Now()

	part1 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 13, 2, 1, []byte{0x01})
	r.Push(part1, start)
	if r.Pending() != 1 {
		t.Fatalf("expected 1 pending entry, got %d", r.Pending())
	}

	// Advance time well past the TTL and push an unrelated frame to trigger
	// the expiry sweep.
	later := start.Add(time.Hour)
	other := buildSubpackagedFrame(MsgLocationReport, "013800100034", 99, 2, 1, []byte{0xff})
	r.Push(other, later)

	if r.Pending() != 1 {
		t.Fatalf("expected the stale entry to be expired and only the fresh one pending, got %d", r.Pending())
	}
}

func TestReassemblerDifferentSessionsDoNotCollide(t *testing.T) {
	t.Parallel()
	r := NewReassembler(time.Minute)
	now := time.Now()

	terminalA1 := buildSubpackagedFrame(MsgLocationReport, "AAA", 1, 2, 1, []byte{0x01})
	terminalB1 := buildSubpackagedFrame(MsgLocationReport, "BBB", 1, 2, 1, []byte{0x02})
	r.Push(terminalA1, now)
	r.Push(terminalB1, now)
	if r.Pending() != 2 {
		t.Fatalf("expected 2 independent pending entries for 2 terminals, got %d", r.Pending())
	}
}

// TestReassembledFrameDecodesViaLengthCheck sanity-checks that a
// reassembled body's total length is at least consistent with what
// parseHeader would expect if it were an on-wire frame (defensive
// documentation of the contract: Reassembler only joins Body bytes, it
// does not re-derive BodyProps/length).
func TestReassembledFrameDecodesViaLengthCheck(t *testing.T) {
	t.Parallel()
	r := NewReassembler(time.Minute)
	now := time.Now()

	body := make([]byte, 0, 28)
	body = binary.BigEndian.AppendUint32(body, 0) // alarm flags
	part1 := buildSubpackagedFrame(MsgLocationReport, "013800100034", 20, 1, 1, body)
	got, complete := r.Push(part1, now)
	if !complete {
		t.Fatal("a 1-of-1 subpackaged message should complete on the first packet")
	}
	if len(got.Body) != len(body) {
		t.Fatalf("got %d body bytes, want %d", len(got.Body), len(body))
	}
}
