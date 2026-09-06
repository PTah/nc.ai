package dockicon

import "sync/atomic"

// activeAgents counts concurrent agent runs. Dock animation runs while > 0 (macOS only).
var activeAgents atomic.Int32

// BeginAgent starts Dock icon animation on the first concurrent agent run (macOS).
func BeginAgent() {
	if activeAgents.Add(1) == 1 {
		startAnim()
	}
}

// EndAgent stops Dock animation when the last agent run finishes (macOS).
func EndAgent() {
	for {
		cur := activeAgents.Load()
		if cur <= 0 {
			return
		}
		if activeAgents.CompareAndSwap(cur, cur-1) {
			if cur-1 == 0 {
				stopAnim()
			}
			return
		}
	}
}
