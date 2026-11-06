package app

import (
	"encoding/json"
	"time"
)

// statusEvent is one full snapshot pushed over SSE. A client replaces its whole
// state with Data; there are no partial updates that could drift.
type statusEvent struct {
	Type string `json:"type"`
	At   string `json:"at"`
	Data Status `json:"data"`
}

func (a *App) statusEvent() ([]byte, error) {
	return json.Marshal(statusEvent{
		Type: "status",
		At:   time.Now().UTC().Format(time.RFC3339),
		Data: a.status(),
	})
}

// notifyStatus schedules a coalesced status frame. A scan that snapshots twenty
// worktrees produces one frame, not twenty.
func (a *App) notifyStatus() {
	if a.hub == nil || a.hub.Subscribers() == 0 {
		return
	}

	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	if a.statusTimer != nil {
		return
	}
	a.statusTimer = time.AfterFunc(120*time.Millisecond, func() {
		a.statusMu.Lock()
		a.statusTimer = nil
		a.statusMu.Unlock()
		a.broadcastStatus()
	})
}

func (a *App) broadcastStatus() {
	if a.hub == nil || a.hub.Subscribers() == 0 {
		return
	}
	payload, err := a.statusEvent()
	if err != nil {
		a.log.Warn("could not encode status event", "err", err)
		return
	}
	a.hub.Publish(payload)
}
