package sessionruntime

import (
	"encoding/json"

	chatview "github.com/felinics/memoh/internal/agent/view"
)

// These wire views share one input adapter without copying the domain's field
// list. Backend snapshot codecs use them directly so nested MarshalJSON calls
// do not serialize and scan the complete message stream a second time.
type runViewFields CurrentRunView

type runViewWire struct {
	runViewFields
	RequestUserTurn *chatview.UITurn `json:"request_user_turn,omitempty"`
}

type snapshotFields Snapshot

type snapshotWire struct {
	snapshotFields
	CurrentRunView *runViewWire `json:"current_run_view,omitempty"`
}

// A steer-only projection must not masquerade as the run's original input.
func (run CurrentRunView) requestUserTurn() *chatview.UITurn {
	if len(run.UserTurns) == 0 {
		return nil
	}
	first := &run.UserTurns[0]
	if run.TurnID != "" && first.TurnID != "" && run.TurnID != first.TurnID {
		return nil
	}
	return first
}

func (run CurrentRunView) wireView() runViewWire {
	return runViewWire{runViewFields: runViewFields(run), RequestUserTurn: run.requestUserTurn()}
}

// Decode old snapshots once, then retain only canonical inputs in live state.
// If both forms exist, user_turns wins over a stale legacy copy.
func (wire runViewWire) value() CurrentRunView {
	run := CurrentRunView(wire.runViewFields)
	if len(run.UserTurns) == 0 {
		switch {
		case wire.RequestUserTurn != nil:
			run.UserTurns = []chatview.UITurn{*wire.RequestUserTurn}
		case run.Operation != nil && run.Operation.ReplacementUserTurn != nil:
			run.UserTurns = []chatview.UITurn{*run.Operation.ReplacementUserTurn}
		}
	}
	return run
}

func (run CurrentRunView) MarshalJSON() ([]byte, error) {
	return json.Marshal(run.wireView())
}

func (run *CurrentRunView) UnmarshalJSON(data []byte) error {
	wire := runViewWire{runViewFields: runViewFields(*run)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*run = wire.value()
	return nil
}

func marshalSnapshot(snapshot Snapshot) ([]byte, error) {
	wire := snapshotWire{snapshotFields: snapshotFields(snapshot)}
	if snapshot.CurrentRunView != nil {
		view := snapshot.CurrentRunView.wireView()
		wire.CurrentRunView = &view
	}
	return json.Marshal(wire)
}

func unmarshalSnapshot(data []byte, snapshot *Snapshot) error {
	var wire snapshotWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*snapshot = Snapshot(wire.snapshotFields)
	if wire.CurrentRunView != nil {
		view := wire.CurrentRunView.value()
		snapshot.CurrentRunView = &view
	}
	return nil
}
