package foundation

import (
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

// ObservationPolicy returns the immutable regtest release profile's timing values.
func ObservationPolicy() api.ObservationPolicy {
	return api.ObservationPolicy{Profile: "regtest-pox4-pox5-v1", PollIntervalSeconds: 2, RPCAllowanceSeconds: 10, HeartbeatIntervalSeconds: 5, ProgressGraceSeconds: 120}
}

// ObservationFreshness is independent of production cadence and status-write frequency.
func ObservationFreshness() time.Duration {
	p := ObservationPolicy()
	return time.Duration(3*p.PollIntervalSeconds+p.RPCAllowanceSeconds) * time.Second
}

// ProgressWindow uses the applied cadence without refreshing a past success time.
func ProgressWindow(cadenceUpperBound time.Duration) time.Duration {
	p := ObservationPolicy()
	return max(time.Duration(p.ProgressGraceSeconds)*time.Second, 3*cadenceUpperBound) + time.Duration(p.RPCAllowanceSeconds)*time.Second
}
