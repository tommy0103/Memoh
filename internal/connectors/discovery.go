package connectors

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	connectsdk "github.com/felinics/connect-it/sdk/go"
)

// maxDiscoveryErrorLen bounds the upstream error text kept for users and the
// model; the full error still goes to the log.
const maxDiscoveryErrorLen = 300

// ToolDiscoveryFailure is one connection Connect-It left out of the agent's
// tool session because its upstream could not list tools.
type ToolDiscoveryFailure struct {
	Alias        string `json:"alias"`
	ConnectionID string `json:"connection_id"`
	Error        string `json:"error"`
}

// ToolDiscoveryStatus is the last known outcome of loading a bot's connector
// tools for the agent. Error is set when no connector tool could be loaded at
// all; FailedConnections lists the connections excluded from an otherwise
// working session. Both empty means the last load succeeded completely.
type ToolDiscoveryStatus struct {
	ObservedAt        time.Time              `json:"observed_at"`
	Error             string                 `json:"error,omitempty"`
	FailedConnections []ToolDiscoveryFailure `json:"failed_connections,omitempty"`
}

// Degraded reports whether any connector tool is currently missing.
func (s ToolDiscoveryStatus) Degraded() bool {
	return s.Error != "" || len(s.FailedConnections) > 0
}

// toolDiscoveryStore remembers the per-bot status in memory. It is advisory:
// losing it on restart only means the next agent turn re-evaluates.
type toolDiscoveryStore struct {
	mu    sync.Mutex
	byBot map[string]ToolDiscoveryStatus
}

func (st *toolDiscoveryStore) update(botID string, mutate func(*ToolDiscoveryStatus)) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.byBot == nil {
		st.byBot = map[string]ToolDiscoveryStatus{}
	}
	status := st.byBot[botID]
	mutate(&status)
	status.ObservedAt = time.Now()
	st.byBot[botID] = status
}

func (st *toolDiscoveryStore) get(botID string) (ToolDiscoveryStatus, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	status, ok := st.byBot[strings.TrimSpace(botID)]
	return status, ok
}

func (st *toolDiscoveryStore) clear(botID string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.byBot, strings.TrimSpace(botID))
}

// RecordToolDiscoveryFailure notes that the bot's connector tool list could
// not be loaded at all.
func (s *Service) RecordToolDiscoveryFailure(botID string, err error) {
	if err == nil {
		return
	}
	if s == nil {
		return
	}
	s.discovery.update(botID, func(status *ToolDiscoveryStatus) {
		status.Error = discoveryErrorText(err)
	})
}

// RecordToolDiscoverySuccess notes that the connector tool list loaded. Any
// connections Connect-It excluded from the session stay recorded until the
// next session is issued or the bot's connectors change.
func (s *Service) RecordToolDiscoverySuccess(botID string) {
	if s == nil {
		return
	}
	s.discovery.update(botID, func(status *ToolDiscoveryStatus) {
		status.Error = ""
	})
}

// RecordToolDiscoverySession replaces the connections Connect-It reported as
// excluded from the bot's current tool session.
func (s *Service) RecordToolDiscoverySession(botID string, failed []ToolDiscoveryFailure) {
	if s == nil {
		return
	}
	s.discovery.update(botID, func(status *ToolDiscoveryStatus) {
		status.FailedConnections = append([]ToolDiscoveryFailure(nil), failed...)
	})
}

// ClearToolDiscovery forgets the bot's status, for example when its
// connectors changed or it has no enabled connection left.
func (s *Service) ClearToolDiscovery(botID string) {
	if s == nil {
		return
	}
	s.discovery.clear(botID)
}

// ToolDiscoveryStatus returns the bot's last recorded status, if any.
func (s *Service) ToolDiscoveryStatus(botID string) (ToolDiscoveryStatus, bool) {
	if s == nil {
		return ToolDiscoveryStatus{}, false
	}
	return s.discovery.get(botID)
}

// annotateToolDiscovery copies the recorded status onto the connections it
// concerns: a whole-session failure applies to every connection the session
// would have carried, a per-connection failure only to that alias.
func (s *Service) annotateToolDiscovery(botID string, items []Connector) {
	if s == nil {
		return
	}
	status, ok := s.discovery.get(botID)
	if !ok || !status.Degraded() {
		return
	}
	failedByAlias := make(map[string]string, len(status.FailedConnections))
	for _, failure := range status.FailedConnections {
		failedByAlias[failure.Alias] = failure.Error
	}
	for i := range items {
		switch {
		case status.Error != "":
			if items[i].Enabled && items[i].Status == "active" {
				items[i].ToolDiscoveryError = status.Error
			}
		default:
			items[i].ToolDiscoveryError = failedByAlias[items[i].Alias]
		}
	}
}

// discoveryErrorText keeps the part of a tool listing error that helps a
// user or the model: the Connect-It code and message when the upstream
// answered, otherwise the transport error, bounded in length.
func discoveryErrorText(err error) string {
	var apiErr *connectsdk.APIError
	text := ""
	if errors.As(err, &apiErr) {
		text = strings.TrimSpace(apiErr.Message)
		if apiErr.Code != "" {
			text = apiErr.Code + ": " + text
		}
	}
	if text == "" {
		text = strings.TrimSpace(err.Error())
	}
	if len(text) > maxDiscoveryErrorLen {
		text = text[:maxDiscoveryErrorLen] + "…"
	}
	return text
}

// ToolDiagnostics renders the bot's degraded connector status as guidance
// for the model: the tools are missing because loading them failed, not
// because the user has no connectors.
func (s *Service) ToolDiagnostics(botID string) []string {
	if s == nil {
		return nil
	}
	status, ok := s.discovery.get(botID)
	if !ok || !status.Degraded() {
		return nil
	}
	if status.Error != "" {
		return []string{fmt.Sprintf(
			"Connector tools (the user's connected services such as Google, Notion or GitHub) could not be loaded for this turn: %s. "+
				"The connections may still be authorized. If the user asks for something that needs them, say that the connector tools are temporarily unavailable and suggest checking the connectors under the bot's Apps settings; do not claim that no connector exists.",
			status.Error)}
	}
	lines := make([]string, 0, len(status.FailedConnections))
	for _, failure := range status.FailedConnections {
		lines = append(lines, fmt.Sprintf(
			"The %q connector's tools could not be loaded for this turn: %s. Its connection may still be authorized; other connector tools are available as usual. "+
				"If the user needs it, say that this connector is temporarily unavailable and suggest checking it under the bot's Apps settings.",
			failure.Alias, failure.Error))
	}
	return lines
}
