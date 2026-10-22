package compaction

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

// starvationCorpus is a leading reasoning-only row that renders empty, an
// answered ask_user exchange, then ordinary old history, then the current
// turn.
func starvationCorpus(t *testing.T) []sqlc.ListUncompactedMessagesBySessionRow {
	t.Helper()
	return []sqlc.ListUncompactedMessagesBySessionRow{
		mkRow(t, "assistant", `[{"type":"reasoning","text":"internal chain of thought"}]`, 400),                                       // 0: renders empty
		mkRow(t, "assistant", `[{"type":"tool-call","toolCallId":"U","toolName":"ask_user","input":{}}]`, 40),                         // 1: must-keep call
		mkRow(t, "tool", `[{"type":"tool-result","toolCallId":"U","toolName":"ask_user","output":"user said yes"}]`, 40),              // 2: must-keep result
		mkRow(t, "user", `"old question about the migration plan with enough surrounding detail to be worth compressing"`, 400),       // 3
		mkRow(t, "assistant", `"old answer walking through every step of the plan in enough words to outweigh a short summary"`, 400), // 4
		mkRow(t, "user", `"current question"`, 40), // 5: protected current turn
	}
}

func TestDoCompactionCoversLeadingRowsThatRenderNothing(t *testing.T) {
	t.Parallel()

	rows := starvationCorpus(t)
	q := &fakeQueries{uncompacted: rows}
	stub := &stubModel{summary: "compacted the old exchange"}
	svc := newMachineryService(q)

	res, err := svc.RunCompactionSync(context.Background(), machineryConfig(stub, 100))
	if err != nil {
		t.Fatalf("RunCompactionSync: %v", err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q: a leading row that renders nothing must not starve the history behind it", res.Status, StatusOK)
	}

	if len(q.markedIDs) != 5 {
		t.Fatalf("marked = %v, want the whole prefix before the current question", q.markedIDs)
	}
	for _, frag := range []string{"old question", "old answer", "user said yes"} {
		if !strings.Contains(stub.prompt, frag) {
			t.Fatalf("summarizer prompt missing %q:\n%s", frag, stub.prompt)
		}
	}
	for _, frag := range []string{"current question", "chain of thought"} {
		if strings.Contains(stub.prompt, frag) {
			t.Fatalf("summarizer prompt leaked raw-retained content %q:\n%s", frag, stub.prompt)
		}
	}
}

func TestDoCompactionNoopsWithoutModelCallWhenOnlyUnmarkableRowsRemain(t *testing.T) {
	t.Parallel()

	rows := starvationCorpus(t)
	// The reasoning row renders nothing and the unanswered ask_user call stays.
	remaining := append([]sqlc.ListUncompactedMessagesBySessionRow{}, rows[0], rows[1], rows[5])
	q := &fakeQueries{uncompacted: remaining}
	stub := &stubModel{summary: "should never be produced"}
	svc := newMachineryService(q)

	res, err := svc.RunCompactionSync(context.Background(), machineryConfig(stub, 100))
	if err != nil {
		t.Fatalf("RunCompactionSync: %v", err)
	}
	if res.Status != StatusNoop {
		t.Fatalf("status = %q, want %q", res.Status, StatusNoop)
	}
	if stub.calls != 0 {
		t.Fatalf("model called %d times for an unmarkable window, want 0", stub.calls)
	}
	if len(q.markedIDs) != 0 {
		t.Fatalf("marked = %v, want none", q.markedIDs)
	}
	if q.created {
		t.Fatal("a compaction log row was created for a no-op window")
	}
}

func TestDoCompactionCoversOversizedRenderEmptyHead(t *testing.T) {
	t.Parallel()

	rows := []sqlc.ListUncompactedMessagesBySessionRow{
		mkRow(t, "assistant", `[{"type":"reasoning","text":"a"}]`, 20000), // renders empty, huge raw estimate
		mkRow(t, "assistant", `[{"type":"reasoning","text":"b"}]`, 20000),
		mkRow(t, "user", `"old question"`, 100),
		mkRow(t, "assistant", `"old answer"`, 100),
		mkRow(t, "user", `"current question"`, 40),
	}
	q := &fakeQueries{uncompacted: rows}
	stub := &stubModel{summary: "old exchange condensed"}
	svc := newMachineryService(q)

	cfg := machineryConfig(stub, 100)
	cfg.MaxCompactTokens = 10000 // smaller than one reasoning row's raw estimate
	res, err := svc.RunCompactionSync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunCompactionSync: %v", err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q: a render-empty head must not eat the trim budget", res.Status, StatusOK)
	}
	if len(q.markedIDs) != 4 {
		t.Fatalf("marked = %v, want the render-empty head covered with the old question/answer pair", q.markedIDs)
	}
}

func TestDoCompactionCostsAnExchangeByWhatItRenders(t *testing.T) {
	t.Parallel()

	rows := []sqlc.ListUncompactedMessagesBySessionRow{
		mkRow(t, "assistant", `[{"type":"text","text":"running the tool"},{"type":"tool-call","toolCallId":"x","toolName":"exec","input":{}}]`, 40000),
		mkRow(t, "tool", `[{"type":"binary","data":"opaque"}]`, 100), // unrecognized part renders empty -> exchange incomplete
		mkRow(t, "user", `"old question"`, 100),
		mkRow(t, "assistant", `"old answer"`, 100),
		mkRow(t, "user", `"current question"`, 40),
	}
	q := &fakeQueries{uncompacted: rows}
	stub := &stubModel{summary: "old exchange condensed"}
	svc := newMachineryService(q)

	cfg := machineryConfig(stub, 100)
	cfg.MaxCompactTokens = 10000 // smaller than the incomplete exchange's raw estimate
	res, err := svc.RunCompactionSync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunCompactionSync: %v", err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q: an exchange's raw usage estimate must not eat the trim budget", res.Status, StatusOK)
	}
	if len(q.markedIDs) != 4 {
		t.Fatalf("marked = %v, want the exchange covered with the old question/answer pair", q.markedIDs)
	}
}

// productionStarvationCorpus is the MEMOH-100 session shape: a head of
// answered ask_user exchanges and reasoning-only rows, a 14-character user
// message, another reasoning-only row, then the history worth compacting.
func productionStarvationCorpus(t *testing.T) []sqlc.ListUncompactedMessagesBySessionRow {
	t.Helper()
	var rows []sqlc.ListUncompactedMessagesBySessionRow
	for i := range 14 {
		id := fmt.Sprintf("ask-%02d", i)
		rows = append(rows,
			mkRow(t, "assistant", `[{"type":"tool-call","toolCallId":"`+id+`","toolName":"ask_user","input":{"question":"pick one"}}]`, 20),
			mkRow(t, "tool", `[{"type":"tool-result","toolCallId":"`+id+`","toolName":"ask_user","output":"answer `+id+`"}]`, 20),
		)
	}
	reasoning := mkRow(t, "assistant", `[{"type":"reasoning","text":"internal chain of thought"}]`, 400)
	rows = append(rows, reasoning, reasoning, mkRow(t, "user", `"dbdfc538 note?"`, 0), reasoning)
	for i := range 6 {
		id := fmt.Sprintf("exec-%02d", i)
		rows = append(rows,
			mkRow(t, "user", `"run the next migration step and report"`, 0),
			mkRow(t, "assistant", `[{"type":"tool-call","toolCallId":"`+id+`","toolName":"exec","input":{"command":"step"}}]`, 20),
			mkRow(t, "tool", `[{"type":"tool-result","toolCallId":"`+id+`","toolName":"exec","output":"EXEC_OUTPUT `+strings.Repeat("migration detail ", 60)+`"}]`, 20),
		)
	}
	for i := range rows {
		rows[i].ID = testUUID(t)
	}
	return append(rows, mkRow(t, "user", `"current question"`, 0))
}

func TestCompactionCoversThePrefixPastAnsweredAskUserAndReasoningRows(t *testing.T) {
	t.Parallel()

	rows := productionStarvationCorpus(t)
	q := &fakeQueries{uncompacted: rows}
	stub := &stubModel{summary: "SUMMARY " + strings.Repeat("kept decision ", 72)}
	res, err := newMachineryService(q).RunCompactionSync(context.Background(), machineryConfig(stub, 1))
	if err != nil || res.Status != StatusOK {
		t.Fatalf("compaction stalled on the short head fragment: result=%+v err=%v", res, err)
	}
	if stub.calls != 1 {
		t.Fatalf("summarizer calls = %d, want 1", stub.calls)
	}
	want := rows[:len(rows)-1]
	if len(q.markedIDs) != len(want) {
		t.Fatalf("marked %d rows, want the %d-row prefix before the current input", len(q.markedIDs), len(want))
	}
	for i, row := range want {
		if q.markedIDs[i] != row.ID {
			t.Fatalf("marked[%d] is not prefix row %d: coverage must be one contiguous span", i, i)
		}
	}
	for _, frag := range []string{"answer ask-00", "dbdfc538", "EXEC_OUTPUT"} {
		if !strings.Contains(stub.prompt, frag) {
			t.Fatalf("summarizer prompt missing %q", frag)
		}
	}
}

func TestCompactionCountsTheReplayOfRowsThatRenderNothing(t *testing.T) {
	t.Parallel()

	reasoning := `[{"type":"reasoning","text":"` + strings.Repeat("weigh the options ", 1500) + `"}]`
	rows := []sqlc.ListUncompactedMessagesBySessionRow{
		mkRow(t, "user", `"ok"`, 2),
		mkRow(t, "assistant", reasoning, 6000),
		mkRow(t, "assistant", `"done"`, 2),
		mkRow(t, "user", `"current question"`, 4),
	}
	q := &fakeQueries{uncompacted: rows}
	stub := &stubModel{summary: "SUMMARY " + strings.Repeat("kept decision ", 20)}
	res, err := newMachineryService(q).RunCompactionSync(context.Background(), machineryConfig(stub, 1))
	if err != nil || res.Status != StatusOK || len(q.markedIDs) != 3 {
		t.Fatalf("a summary far smaller than the replayed reasoning was rejected: result=%+v err=%v marked=%d", res, err, len(q.markedIDs))
	}
}
