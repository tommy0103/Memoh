package sessionruntime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	chatview "github.com/felinics/memoh/internal/agent/view"
)

func TestCurrentRunViewInputCodec(t *testing.T) {
	for _, tc := range []struct {
		name, data, text, turnID string
		legacy                   bool
	}{
		{"legacy", `{"turn_id":"root","request_user_turn":{"turn_id":"root","role":"user","text":"old"}}`, "old", "root", true},
		{"canonical wins", `{"turn_id":"root","request_user_turn":{"turn_id":"root","role":"user","text":"stale"},"user_turns":[{"turn_id":"root","role":"user","text":"current"}]}`, "current", "root", true},
		{"replacement", `{"turn_id":"root","operation":{"kind":"edit","replacement_user_turn":{"turn_id":"root","role":"user","text":"edited"}}}`, "edited", "root", true},
		{"steer only", `{"turn_id":"root","user_turns":[{"turn_id":"steer","role":"user","text":"later"}]}`, "later", "steer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var run CurrentRunView
			var snapshot Snapshot
			require.NoError(t, json.Unmarshal([]byte(tc.data), &run))
			require.NoError(t, unmarshalSnapshot([]byte(`{"current_run_view":`+tc.data+`}`), &snapshot))
			require.Equal(t, &run, snapshot.CurrentRunView, "public and backend decoders")
			require.Len(t, run.UserTurns, 1)
			require.Equal(t, tc.text, run.UserTurns[0].Text)
			// Canonical updates must appear in the legacy wire, except that a steer
			// cannot become the run's original input. Both encoders must agree.
			run.UserTurns[0].Text = "persisted"
			data, err := json.Marshal(run)
			require.NoError(t, err)
			stored, err := marshalSnapshot(Snapshot{CurrentRunView: &run})
			require.NoError(t, err)
			var fields struct {
				Run json.RawMessage `json:"current_run_view"`
			}
			require.NoError(t, json.Unmarshal(stored, &fields))
			require.JSONEq(t, string(data), string(fields.Run))
			var wire struct {
				RequestUserTurn json.RawMessage   `json:"request_user_turn"`
				UserTurns       []chatview.UITurn `json:"user_turns"`
			}
			require.NoError(t, json.Unmarshal(data, &wire))
			require.Len(t, wire.UserTurns, 1)
			require.Equal(t, tc.turnID, wire.UserTurns[0].TurnID)
			if tc.legacy {
				var legacy chatview.UITurn
				require.NoError(t, json.Unmarshal(wire.RequestUserTurn, &legacy))
				require.Equal(t, "persisted", legacy.Text)
			} else {
				require.Nil(t, wire.RequestUserTurn)
			}
			var restored CurrentRunView
			require.NoError(t, json.Unmarshal(data, &restored))
			require.Equal(t, run, restored, "roundtrip keeps canonical inputs")
		})
	}
}
