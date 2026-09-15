package jt808

// Frame is one decoded JT808 message: the two 0x7e delimiters are stripped,
// the payload is unescaped and checksum-verified, and the header fields are
// parsed out. Body carries whatever remains after the (12- or 16-byte)
// header, per the message's own body-length field.
type Frame struct {
	MessageID     uint16
	BodyProps     uint16
	Encryption    uint8
	HasSubpackage bool
	TerminalSN    string
	MessageSerial uint16
	TotalPackets  uint16
	PacketSerial  uint16
	Body          []byte
	Raw           []byte // on-wire bytes as received: escaped, including both 0x7e delimiters
	Checksum      byte
}

// BodyLength returns the body length declared in the message body
// properties word (bits 0-9), independent of len(Body) which is the
// already-validated actual length.
func (f Frame) BodyLength() int { return int(f.BodyProps & bodyLengthMask) }
