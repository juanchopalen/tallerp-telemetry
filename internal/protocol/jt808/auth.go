package jt808

import "fmt"

// ParseAuth decodes a 0x0102 terminal authentication body: a single STRING
// field carrying the auth code the terminal was handed at registration.
func ParseAuth(body []byte) (AuthPacket, error) {
	if len(body) == 0 {
		return AuthPacket{}, fmt.Errorf("%w: empty authentication code", ErrMalformedBody)
	}
	return AuthPacket{AuthCode: string(body)}, nil
}
