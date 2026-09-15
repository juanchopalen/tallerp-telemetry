package telemetry

import "sync/atomic"

type Metrics struct {
	ConnectionsActive        atomic.Int64
	FramesReceived           atomic.Uint64
	FramesReceivedGT06       atomic.Uint64
	FramesReceivedGT02       atomic.Uint64
	FramesReceivedJT808      atomic.Uint64
	LocationsReceived        atomic.Uint64
	LocationsGT06            atomic.Uint64
	LocationsGT02            atomic.Uint64
	LocationsJT808           atomic.Uint64
	HeartbeatsReceived       atomic.Uint64
	HeartbeatsJT808          atomic.Uint64
	InvalidCRC               atomic.Uint64
	UnsupportedProtocol      atomic.Uint64
	ProtocolDetectionFailure atomic.Uint64
	DeliverySuccess          atomic.Uint64
	DeliveryFailure          atomic.Uint64

	// JT808-only.
	RegistrationJT808      atomic.Uint64
	AuthSuccessJT808       atomic.Uint64
	AuthFailureJT808       atomic.Uint64
	ChecksumErrorsJT808    atomic.Uint64
	ParseErrorsJT808       atomic.Uint64
	UnknownMessageIDsJT808 atomic.Uint64
}
