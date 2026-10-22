package message

import (
	"context"
	"testing"

	"github.com/google/uuid"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/runtimefence"
)

// Internal image feedback must not split one admitted run into new user turns.
// Exercise the real fenced step writer and read the resulting database identity.
func TestPostgresRuntimeFenceScreenshotFeedbackKeepsOriginalTurn(t *testing.T) {
	ctx := context.Background()
	pool := openRuntimeFencePostgresPool(t, ctx)
	botID, sessionID := createRuntimeFenceFixtures(t, ctx, pool)
	queries := dbsqlc.New(pool)
	token := acquireRuntimeFenceToken(t, ctx, queries, botID, sessionID)
	runID, turnID := uuid.NewString(), uuid.NewString()
	_, err := pool.Exec(ctx, `INSERT INTO session_runs
		(run_id, bot_id, session_id, invocation_id, turn_id, turn_position, state,
		input_json, input_fingerprint, owner_id, fencing_token, owner_since, live_generation)
		VALUES ($1,$2,$3,$4,$5,0,'running','{}','repro','repro-owner',$6,now(),'repro')`,
		runID, botID, sessionID, uuid.NewString(), turnID, token)
	if err != nil {
		t.Fatal(err)
	}
	// Admission reserves position 0 before the step writer sees any messages.
	if _, err := pool.Exec(ctx, `UPDATE bot_sessions SET next_turn_position=1 WHERE id=$1`, sessionID); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, postgresstore.NewQueriesWithPool(pool, queries))
	owner := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: botID.String(), SessionID: sessionID.String(), Token: token})
	position := int64(0)
	requestID := ""
	steps := [][]PersistInput{
		{
			{Role: "user", TurnID: turnID, TurnPosition: &position, Content: []byte(`{"role":"user","content":"Inspect the desktop and return a screenshot"}`)},
			{Role: "assistant", Content: []byte(`{"role":"assistant","content":[{"type":"tool-call","toolCallId":"read-1","toolName":"read","input":{"path":"/data/screenshot.png"}}]}`)},
			{Role: "tool", Content: []byte(`{"role":"tool","content":[{"type":"tool-result","toolCallId":"read-1","toolName":"read","output":"image loaded"}]}`)},
		},
		{
			{Role: "user", Metadata: map[string]any{MessageSourceMetadataKey: MessageSourceInternalFeedback}, Content: []byte(`{"role":"user","content":[{"type":"image","image":"data:image/png;base64,AAAA"}]}`)},
			{Role: "assistant", Content: []byte(`{"role":"assistant","content":"Screenshot inspected; task complete."}`)},
		},
	}
	// A second read must also leave the admitted turn unchanged.
	steps = append(steps, steps[1])
	for _, inputs := range steps {
		for i := range inputs {
			inputs[i].BotID, inputs[i].SessionID, inputs[i].RunID = botID.String(), sessionID.String(), runID
			inputs[i].TurnRequestMessageID = requestID
		}
		persisted, err := service.PersistAgentStep(owner, AgentStep{RunID: runID, Messages: inputs})
		if err != nil {
			t.Fatal(err)
		}
		// Mirror agentStepCommitter's handover between SDK steps.
		for _, message := range persisted {
			if message.Role == "user" && !IsInternalFeedback(message.Metadata) {
				requestID = message.ID
			}
		}
	}
	rows, err := pool.Query(ctx, `SELECT role, turn_id::text, turn_position, run_id::text
		FROM bot_history_messages WHERE session_id=$1 ORDER BY created_at, turn_position, turn_message_seq`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	turns := map[string]bool{}
	for rows.Next() {
		var role, rowTurn, rowRun string
		var rowPosition int64
		if err := rows.Scan(&role, &rowTurn, &rowPosition, &rowRun); err != nil {
			t.Fatal(err)
		}
		t.Logf("role=%s turn=%s position=%d run=%s", role, rowTurn, rowPosition, rowRun)
		if rowRun != runID {
			t.Errorf("run changed: %s", rowRun)
		}
		turns[rowTurn] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || !turns[turnID] {
		t.Fatalf("one request plus internal screenshot feedback produced %d turns; want the original admitted turn only", len(turns))
	}
	rows.Close()
	// A real additional user input still opens a distinct turn, even inside
	// the same run. This is the boundary internal feedback must not erase.
	steer, err := service.PersistAgentStep(owner, AgentStep{RunID: runID, Messages: []PersistInput{
		{BotID: botID.String(), SessionID: sessionID.String(), RunID: runID, Role: "user", TurnRequestMessageID: requestID, DisplayText: "also inspect another window", Content: []byte(`{"role":"user","content":"also inspect another window"}`)},
		{BotID: botID.String(), SessionID: sessionID.String(), RunID: runID, Role: "assistant", Content: []byte(`{"role":"assistant","content":"checking another window"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(steer) != 2 || steer[0].TurnID == turnID || steer[1].TurnID != steer[0].TurnID {
		t.Fatalf("real user turn not preserved: %+v", steer)
	}
}
