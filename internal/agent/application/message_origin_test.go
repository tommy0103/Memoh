package application

import (
	"context"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

func TestInternalFeedbackPersistenceSource(t *testing.T) {
	messages := sdkMessagesWithOrigins([]sdk.Message{
		{Role: sdk.MessageRoleUser, Content: []sdk.MessagePart{sdk.FilePart{Data: "AAAA", MediaType: "application/pdf", Filename: "test.pdf"}}},
		sdk.UserMessage("another real user input"),
		sdk.AssistantMessage("done"),
	}, []int{0})
	service := &Service{}
	inputs, err := service.buildPersistInputs(context.Background(), ChatRequest{Query: "original", BotID: "bot", ThreadID: "session"}, messages, "model", storeRoundOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 {
		t.Fatalf("inputs=%d", len(inputs))
	}
	if !messagepkg.IsInternalFeedback(inputs[0].Metadata) || inputs[0].DisplayText != "" {
		t.Fatalf("file feedback missing source or visible: %+v", inputs[0])
	}
	if messagepkg.IsInternalFeedback(inputs[1].Metadata) || inputs[1].DisplayText != "another real user input" {
		t.Fatalf("real user misclassified: %+v", inputs[1])
	}
}

func TestInternalFeedbackDoesNotClaimMatchingUserQuery(t *testing.T) {
	messages := sdkMessagesWithOrigins([]sdk.Message{
		sdk.UserMessage("same text"),
		sdk.UserMessage("same text"),
	}, []int{0})
	service := &Service{}
	inputs, err := service.buildPersistInputs(context.Background(), ChatRequest{
		Query: "same text", BotID: "bot", ThreadID: "session",
		TurnID: "original-turn", ExternalMessageID: "inbound-event",
	}, messages, "model", storeRoundOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inputs[0].TurnID != "" || inputs[0].ExternalMessageID != "" || inputs[0].DisplayText != "" {
		t.Fatalf("internal feedback claimed inbound identity: %+v", inputs[0])
	}
	if inputs[1].TurnID != "original-turn" || inputs[1].ExternalMessageID != "inbound-event" || inputs[1].DisplayText != "same text" {
		t.Fatalf("real user lost inbound identity: %+v", inputs[1])
	}
}
