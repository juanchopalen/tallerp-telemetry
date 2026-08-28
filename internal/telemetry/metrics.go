package telemetry

import "sync/atomic"

type Metrics struct {
	ConnectionsActive   atomic.Int64
	FramesReceived      atomic.Uint64
	LocationsReceived   atomic.Uint64
	HeartbeatsReceived  atomic.Uint64
	InvalidCRC          atomic.Uint64
	UnsupportedProtocol atomic.Uint64
	DeliverySuccess     atomic.Uint64
	DeliveryFailure     atomic.Uint64
}
