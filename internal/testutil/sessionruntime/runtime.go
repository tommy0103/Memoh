// Package sessiontest creates admitted runtimes for tests outside the runtime
// package. It uses the public admission path, never a pre-ledger reservation.
package sessiontest

import (
	"context"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

type deterministicLedger struct{ *sessionledger.Store }

// Fixture callers name their run through the invocation argument. Production
// IDs still come from Manager.Admit; deterministic test identities keep events
// and assertions readable without bypassing admission or fencing.
func (s deterministicLedger) Admit(ctx context.Context, params ledger.AdmitParams) (ledger.Run, bool, error) {
	params.RunID = params.InvocationID
	return s.Store.Admit(ctx, params)
}

type fence struct{}

func (fence) Activate(context.Context, string, string, int64) error { return nil }

func New(backend sessionruntime.Backend, opts sessionruntime.Options) *sessionruntime.Manager {
	if opts.Ledger == nil {
		opts.Ledger = deterministicLedger{sessionledger.New()}
	}
	if opts.Fence == nil {
		opts.Fence = fence{}
	}
	return sessionruntime.NewManager(backend, opts)
}

func Start(ctx context.Context, manager *sessionruntime.Manager, botID, sessionID, invocationID string, abortCh chan<- struct{}, cancel context.CancelFunc, injectCh chan<- turn.InjectMessage) (sessionruntime.RunHandle, error) {
	admission, err := manager.Admit(ctx, sessionruntime.AdmitInput{
		BotID: botID, SessionID: sessionID, InvocationID: invocationID,
		Payload: []byte(`{"text":"runtime fixture"}`),
		Execution: sessionruntime.Execution{
			Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
				return sessionruntime.RunAdmissionView{}, nil
			},
			AbortCh: abortCh, Cancel: cancel, InjectCh: injectCh,
		},
	})
	return admission.Handle, err
}
