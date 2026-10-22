package sessionruntime

import (
	"context"

	"github.com/felinics/memoh/internal/agent/turn"
)

// Package-local backend/ownership tests isolate live reservation algorithms.
// Application and ACP tests use sessiontest.Start and the public Admit path.
func (m *Manager) StartRun(ctx context.Context, botID, sessionID, runID string, abortCh chan<- struct{}, cancel context.CancelFunc, injectCh chan<- turn.InjectMessage) error {
	_, err := m.StartRunHandle(ctx, botID, sessionID, runID, abortCh, cancel, injectCh)
	return err
}

func (m *Manager) StartRunHandle(ctx context.Context, botID, sessionID, runID string, abortCh chan<- struct{}, cancel context.CancelFunc, injectCh chan<- turn.InjectMessage) (RunHandle, error) {
	return m.StartRunWithAdmissionBuilderHandle(ctx, botID, sessionID, runID, func(context.Context, RunHandle) (RunAdmissionView, error) {
		return RunAdmissionView{}, nil
	}, abortCh, cancel, injectCh)
}

func (m *Manager) StartRunWithAdmissionBuilderHandle(ctx context.Context, botID, sessionID, runID string, builder func(context.Context, RunHandle) (RunAdmissionView, error), abortCh chan<- struct{}, cancel context.CancelFunc, injectCh chan<- turn.InjectMessage) (RunHandle, error) {
	return m.StartRunWithAdmissionBuilderAndOwnershipHandle(ctx, botID, sessionID, runID, builder, nil, abortCh, cancel, injectCh)
}

func (m *Manager) StartRunWithAdmissionBuilderAndOwnershipHandle(ctx context.Context, botID, sessionID, runID string, builder func(context.Context, RunHandle) (RunAdmissionView, error), ownershipCancel context.CancelCauseFunc, abortCh chan<- struct{}, cancel context.CancelFunc, injectCh chan<- turn.InjectMessage) (RunHandle, error) {
	handle, _, err := m.startRun(ctx, runStart{
		botID:           botID,
		sessionID:       sessionID,
		runID:           runID,
		builder:         builder,
		ownershipCancel: ownershipCancel,
		abortCh:         abortCh,
		cancel:          cancel,
		injectCh:        injectCh,
	})
	return handle, err
}
