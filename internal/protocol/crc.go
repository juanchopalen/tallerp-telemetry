package protocol

// CalculateCRC calculates the reflected CRC-ITU variant documented by the
// tracker protocol: init 0xffff, polynomial 0x8408, final complement.
func CalculateCRC(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, value := range data {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}
