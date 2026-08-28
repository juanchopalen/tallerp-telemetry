package protocol

// Frame is a validated GT06 protocol frame. Content excludes the protocol
// number, message serial number, CRC, and framing bytes.
type Frame struct {
	Header         []byte
	Length         int
	ProtocolNumber byte
	Content        []byte
	Serial         uint16
	CRC            uint16
	Raw            []byte
}
