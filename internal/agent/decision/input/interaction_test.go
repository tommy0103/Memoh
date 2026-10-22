package input

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func interactionTestPayload() UIPayload {
	return UIPayload{
		Version: PayloadVersion,
		Questions: []UIQuestion{
			{
				ID: "q1", Text: "Pick", Kind: QuestionKindSingleSelect,
				Options: []UIOption{{ID: "q1.o1", Label: "A"}, {ID: "q1.o2", Label: "B"}},
			},
			{
				ID: "q2", Text: "Multi", Kind: QuestionKindMultiSelect, AllowCustom: true,
				Options: []UIOption{{ID: "q2.o1", Label: "X"}, {ID: "q2.o2", Label: "Y"}},
			},
			{ID: "q3", Text: "Free", Kind: QuestionKindText},
		},
	}
}

func TestApplyInteractionOpCompletedIsUnchanged(t *testing.T) {
	t.Parallel()

	// Crash window: completed-but-unsubmitted state must not mutate; the
	// caller re-drives submission off the unchanged completed state.
	state := TextInteractionState{Completed: true, QuestionIndex: 2}
	next, outcome := ApplyInteractionOp(interactionTestPayload(), state, InteractionOp{Kind: OpToggleOption, QuestionIndex: 1, OptionIndex: 0})
	if outcome.Changed || outcome.Reject != RejectNone || !next.Completed {
		t.Fatalf("completed state must be inert: %#v %#v", next, outcome)
	}
}

func TestApplyInteractionOpRejects(t *testing.T) {
	t.Parallel()

	payload := interactionTestPayload()
	cases := []struct {
		name string
		op   InteractionOp
		want InteractionReject
	}{
		{"nav out of range", InteractionOp{Kind: OpNavigate, Page: 9}, RejectInvalidOp},
		{"select bad option", InteractionOp{Kind: OpSelectOption, QuestionIndex: 0, OptionIndex: 9}, RejectInvalidOp},
		{"set_text unknown question", InteractionOp{Kind: OpSetText, QuestionID: "nope", Text: "x"}, RejectInvalidOp},
		{"set_text empty", InteractionOp{Kind: OpSetText, QuestionID: "q3", Text: "  "}, RejectEmptyText},
		{"custom not allowed", InteractionOp{Kind: OpSetText, QuestionID: "q1", Text: "x"}, RejectCustomNotAllowed},
		{"unknown kind", InteractionOp{Kind: "bogus"}, RejectInvalidOp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, outcome := ApplyInteractionOp(payload, TextInteractionState{}, tc.op)
			if outcome.Reject != tc.want || outcome.Changed {
				t.Fatalf("outcome = %#v, want reject %q", outcome, tc.want)
			}
		})
	}
}

func TestApplyInteractionOpToggleClearRemovesAnswer(t *testing.T) {
	t.Parallel()

	payload := interactionTestPayload()
	state := TextInteractionState{QuestionIndex: 1}
	state, _ = ApplyInteractionOp(payload, state, InteractionOp{Kind: OpToggleOption, QuestionIndex: 1, OptionIndex: 0})
	if answer, ok := state.Answer("q2"); !ok || len(answer.OptionIDs) != 1 {
		t.Fatalf("toggle on = %#v", state)
	}
	state, _ = ApplyInteractionOp(payload, state, InteractionOp{Kind: OpToggleOption, QuestionIndex: 1, OptionIndex: 0})
	if _, ok := state.Answer("q2"); ok {
		t.Fatalf("toggle off must remove the answer entry: %#v", state)
	}
}

func TestApplyInteractionOpSubmitSatisfiesSubmittedResult(t *testing.T) {
	t.Parallel()

	// The completed answer set must pass Submit's every-question contract.
	payload := interactionTestPayload()
	state, outcome := ApplyInteractionOp(payload, TextInteractionState{}, InteractionOp{Kind: OpSelectOption, QuestionIndex: 0, OptionIndex: 1})
	if !outcome.Changed || state.QuestionIndex != 1 {
		t.Fatalf("select = %#v %#v", state, outcome)
	}
	state, _ = ApplyInteractionOp(payload, state, InteractionOp{Kind: OpSubmit})
	if !state.Completed {
		t.Fatalf("submit = %#v", state)
	}
	if _, err := submittedResult(payload, state.Answers); err != nil {
		t.Fatalf("submittedResult: %v", err)
	}
}

func TestApplyInteractionOpSameSelectionTogglesOffWithoutAdvance(t *testing.T) {
	t.Parallel()

	payload := interactionTestPayload()
	state, _ := ApplyInteractionOp(payload, TextInteractionState{}, InteractionOp{Kind: OpSelectOption, QuestionIndex: 0, OptionIndex: 0})
	state, outcome := ApplyInteractionOp(payload, state, InteractionOp{Kind: OpSelectOption, QuestionIndex: 0, OptionIndex: 0})
	if !outcome.Changed || state.QuestionIndex != 0 {
		t.Fatalf("clear = %#v %#v", state, outcome)
	}
	if _, ok := state.Answer("q1"); ok {
		t.Fatalf("re-select must clear: %#v", state)
	}
}

func TestServiceAdvanceInteractionDoesNotRequireRuntimeFence(t *testing.T) {
	t.Parallel()

	queries := newFakeUserInputQueries()
	svc := NewService(nil, queries)
	future := time.Now().Add(time.Hour)
	req := createStorePending(t, svc, &future, "telegram-button")
	queries.mu.Lock()
	row := queries.rows[req.ID]
	row.RuntimeFencingToken = pgtype.Int8{Int64: 42, Valid: true}
	queries.mu.Unlock()

	result, err := svc.AdvanceInteraction(context.Background(), AdvanceInteractionInput{
		BotID:     storeTestBotID,
		RequestID: req.ID,
		Op:        InteractionOp{Kind: OpSelectOption, QuestionIndex: 0, OptionIndex: 0},
	})
	if err != nil {
		t.Fatalf("advance interaction: %v", err)
	}
	if !result.Handled || !result.Changed {
		t.Fatalf("result = %#v, want handled changed", result)
	}
	if answer, ok := result.Request.Interaction.Answer("q1"); !ok || len(answer.OptionIDs) != 1 {
		t.Fatalf("interaction = %#v, want q1 selection", result.Request.Interaction)
	}
}

func TestServiceAdvanceInteractionTreatsExpiredRequestAsUnhandled(t *testing.T) {
	t.Parallel()

	queries := newFakeUserInputQueries()
	svc := NewService(nil, queries)
	expired := time.Now().Add(-time.Minute)
	req := createStorePending(t, svc, &expired, "telegram-button-expired")

	result, err := svc.AdvanceInteraction(context.Background(), AdvanceInteractionInput{
		BotID:     storeTestBotID,
		RequestID: req.ID,
		Op:        InteractionOp{Kind: OpSelectOption, QuestionIndex: 0, OptionIndex: 0},
	})
	if err != nil {
		t.Fatalf("advance expired interaction: %v", err)
	}
	if result.Handled {
		t.Fatalf("expired result = %#v, want unhandled", result)
	}
}
