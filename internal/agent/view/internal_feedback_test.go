package view

import (
	"encoding/json"
	"testing"

	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

func TestInternalFeedbackDoesNotRenderOrSplitAnswer(t *testing.T) {
	rows := []messagepkg.Message{
		{ID: "u", Role: "user", TurnID: "turn", Content: json.RawMessage(`{"role":"user","content":"inspect"}`)},
		{ID: "a1", Role: "assistant", TurnID: "turn", Content: json.RawMessage(`{"role":"assistant","content":"before"}`)},
		{ID: "feedback", Role: "user", TurnID: "turn", RawMetadata: json.RawMessage(`{"message_source":"internal_feedback"}`), Content: json.RawMessage(`{"role":"user","content":"[file test.pdf]"}`)},
		{ID: "a2", Role: "assistant", TurnID: "turn", Content: json.RawMessage(`{"role":"assistant","content":"after"}`)},
	}
	turns := ConvertMessagesToUITurns(rows)
	if len(turns) != 2 || len(turns[1].Messages) != 2 {
		t.Fatalf("turns=%+v", turns)
	}
	if turns[1].Messages[0].Content != "before" || turns[1].Messages[1].Content != "after" {
		t.Fatalf("answer changed: %+v", turns[1])
	}
}
