package sessionruntime

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// dispatchTestCommand supplies explicit command envelopes to the transport
// contract tests. Those tests use live reservations, not durable admission.
// Public decision identity/ledger validation is exercised by RouteDecisionResponse
// tests and the process-level acceptance suite, not a legacy production router.
func (m *Manager) dispatchTestCommand(ctx context.Context, botID, sessionID, commandType, targetID string, payload []byte) (bool, error) {
	snapshot, err := m.Snapshot(ctx, botID, sessionID)
	if err != nil {
		return false, err
	}
	run := snapshot.CurrentRunView
	targetID, present := runtimeCommandTargetID(run, commandType, targetID)
	if !present {
		return false, nil
	}
	cmd := Command{
		Type: commandType, ID: testCommandID(botID, sessionID, run, commandType, targetID),
		BotID: botID, SessionID: sessionID, RunID: run.RunID, Generation: run.Generation,
		TargetID: targetID, DecisionResolved: true,
		Payload: payload, PayloadHash: activeCommandPayloadHash(commandType, payload),
	}
	loadCtx, cancel := context.WithTimeout(ctx, min(m.commandTimeout(), 100*time.Millisecond))
	result, found, err := m.loadCommandResult(loadCtx, cmd.ID)
	cancel()
	if err != nil {
		return true, err
	}
	if found {
		return true, commandResultErrorFor(cmd, result)
	}
	if !isActiveRunStatus(run.Status) {
		return false, nil
	}
	now, err := m.backend.Now(ctx)
	if err != nil {
		return true, err
	}
	cmd.CreatedAt, cmd.ExpiresAt = now, now.Add(m.commandTimeout())
	if m.distributed == nil || run.OwnerID == m.ownerID {
		return true, commandResultErrorFor(cmd, m.executeRoutedCommand(ctx, cmd))
	}
	return true, m.dispatchRemoteCommand(ctx, run.OwnerID, cmd)
}

func testCommandID(botID, sessionID string, run *CurrentRunView, commandType, targetID string) string {
	return decisionControlCommandID(commandType, botID, targetID,
		strings.Join([]string{sessionID, run.RunID, run.Generation}, ":"))
}

func runtimeCommandTargetID(run *CurrentRunView, commandType, targetID string) (string, bool) {
	targetID = strings.TrimSpace(targetID)
	if run == nil || targetID == "" {
		return "", false
	}
	for _, message := range run.Messages {
		switch commandType {
		case CommandToolApprovalResponse:
			if message.Approval != nil && (strings.TrimSpace(message.Approval.ApprovalID) == targetID || strconv.Itoa(message.Approval.ShortID) == targetID) {
				canonical := strings.TrimSpace(message.Approval.ApprovalID)
				if canonical == "" {
					canonical = strconv.Itoa(message.Approval.ShortID)
				}
				return canonical, true
			}
		case CommandUserInputResponse:
			if message.UserInput != nil && (strings.TrimSpace(message.UserInput.UserInputID) == targetID || strconv.Itoa(message.UserInput.ShortID) == targetID) {
				canonical := strings.TrimSpace(message.UserInput.UserInputID)
				if canonical == "" {
					canonical = strconv.Itoa(message.UserInput.ShortID)
				}
				return canonical, true
			}
		}
	}
	return "", false
}
