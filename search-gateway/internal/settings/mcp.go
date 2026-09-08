package settings

import (
	"fmt"
	"time"
)

// MinHeartbeatInterval is the lowest accepted MCP heartbeat interval.
// It guards the gateway, and the host running it, against an accidentally
// aggressive configuration, such as setting the interval to 1 assuming
// the value is expressed in milliseconds.
const MinHeartbeatInterval = 15 * time.Second

// Mcp groups Model Context Protocol transport settings.
type Mcp struct {
	heartbeatEnabled  bool
	heartbeatInterval time.Duration
}

func (m *Mcp) HeartbeatEnabled() bool           { return m.heartbeatEnabled }
func (m *Mcp) HeartbeatInterval() time.Duration { return m.heartbeatInterval }

// validate enforces the minimum heartbeat interval, but only when the
// heartbeat is enabled. A misconfigured interval is irrelevant while the
// heartbeat is off.
func (m *Mcp) validate() error {
	if m.heartbeatEnabled && m.heartbeatInterval < MinHeartbeatInterval {
		return fmt.Errorf(
			"mcp heartbeat interval too low: SEARCHBASE_MCP_HEARTBEAT_INTERVAL must be >= %ds, got %ds",
			int(MinHeartbeatInterval.Seconds()),
			int(m.heartbeatInterval.Seconds()),
		)
	}

	return nil
}
