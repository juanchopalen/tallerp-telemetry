package jt808

// CalculateChecksum XORs every byte together, per the JT808 check code
// definition: starting from the message header and XORed byte by byte up to
// (but not including) the checksum byte itself. It must be computed on the
// unescaped bytes.
func CalculateChecksum(data []byte) byte {
	var checksum byte
	for _, value := range data {
		checksum ^= value
	}
	return checksum
}
