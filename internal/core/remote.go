package core

import "time"

// RemoteEnvelope is the wire format for snapshot batches pushed from a worker machine to the hub.
type RemoteEnvelope struct {
	Machine string `json:"machine"`
	// SentAt is the worker-clock send time. Hub eviction keys on server-side
	// receivedAt; SentAt stays on the wire for transit-lag debugging.
	SentAt    time.Time       `json:"sent_at"`
	Snapshots []UsageSnapshot `json:"snapshots"`
}
