//go:build integration

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This fault lives in the isolated test database, not in production code. It
// blocks only this invocation's terminal UPDATE, after its immutable proposal
// committed. Killing the real owner in that window must preserve the proposal.
func TestSRDUR002PreparedFinishSurvivesProcessCrash(t *testing.T) {
	if !envBool(crashEnv) {
		t.Skipf("set %s=1 for the isolated process-crash scenario", crashEnv)
	}
	fixture := requireFixture(t, true)
	prepareFakeModel(t)
	env := loadEnvironment()
	sessionID := mustCreateSession(t, fixture, "finish-proposal-crash")
	marker := uniqueMarker("finish-proposal")
	invocation := "invocation-" + marker
	conn := mustDial(t, env.primaryURL, fixture)
	defer closeWebSocket(conn)
	_, admitted := mustSendAndAccept(t, fixture, conn, sessionID, invocation, directiveMode(marker, 2, 0, "block"))
	if !globalFakeModel.WaitRequestCount(marker, 1, 5*time.Second) {
		t.Fatal("run never reached the model")
	}
	defer globalFakeModel.Release(marker)
	probe := requireLedger(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := envOr(postgresURLEnv, "")
	if dsn == "" {
		t.Fatal("the fault test requires an explicit isolated PostgreSQL URL")
	}
	gate, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	key := time.Now().UnixNano()
	name := fmt.Sprintf("acceptance_finish_%d", key)
	fn := pgx.Identifier{"public", name}.Sanitize()
	trigger := pgx.Identifier{name}.Sanitize()
	if _, err := gate.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		_ = gate.Close(context.Background())
		t.Fatal(err)
	}
	unlocked := false
	unlock := func() {
		if unlocked {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := gate.Exec(cleanup, "SELECT pg_advisory_unlock($1)", key); err != nil {
			t.Errorf("release terminal gate: %v", err)
		}
		unlocked = true
	}
	t.Cleanup(func() {
		unlock()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := gate.Exec(cleanup, "DROP TRIGGER IF EXISTS "+trigger+" ON session_runs; DROP FUNCTION IF EXISTS "+fn+"()"); err != nil {
			t.Errorf("remove terminal fault: %v", err)
		}
		_ = gate.Close(cleanup)
	})
	// Identifiers are quoted and the invocation is test-generated, never user SQL.
	body := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $gate$
 BEGIN
  IF NEW.invocation_id = '%s' THEN PERFORM pg_advisory_xact_lock(%d); END IF;
  RETURN NEW;
 END $gate$;
 CREATE TRIGGER %s BEFORE UPDATE ON session_runs FOR EACH ROW
 WHEN (OLD.state = 'finishing' AND NEW.state IN ('completed','aborted','failed','lost'))
 EXECUTE FUNCTION %s()`, fn, strings.ReplaceAll(invocation, "'", "''"), key, trigger, fn)
	if _, err := gate.Exec(ctx, body); err != nil {
		t.Fatal(err)
	}
	globalFakeModel.Release(marker)
	proposed := mustWaitRunState(t, sessionID, invocation, func(run sessionRunRecord) bool { return run.State == "finishing" })
	var proposal string
	if err := probe.pool.QueryRow(ctx, "SELECT proposed_terminal_state FROM session_runs WHERE run_id=$1::uuid", proposed.RunID).Scan(&proposal); err != nil || proposal != "completed" {
		t.Fatalf("proposal=%q err=%v", proposal, err)
	}
	before, err := probe.historySummary(ctx, proposed)
	if err != nil || before.UserMessages != 1 || before.AssistantMessages == 0 || before.WrongTurnIDs != 0 {
		t.Fatalf("proposal precedes durable output: %+v err=%v", before, err)
	}
	kill := exec.CommandContext(ctx, "docker", "kill", "--signal=KILL", env.primaryContainer) //nolint:gosec // explicit isolated acceptance container
	if output, err := kill.CombinedOutput(); err != nil {
		t.Fatalf("kill owner: %v: %s", err, output)
	}
	t.Cleanup(func() {
		restart, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		start := exec.CommandContext(restart, "docker", "start", env.primaryContainer) //nolint:gosec // explicit isolated acceptance container
		if output, err := start.CombinedOutput(); err != nil {
			t.Errorf("restart owner: %v: %s", err, output)
			return
		}
		client := &http.Client{Timeout: time.Second}
		for restart.Err() == nil {
			request, _ := http.NewRequestWithContext(restart, http.MethodHead, env.primaryURL+"/health", nil)
			response, err := client.Do(request) //nolint:gosec // explicit isolated acceptance URL
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Error("owner did not restart after fault test")
	})
	unlock()
	outcome, err := probe.waitRun(ctx, sessionID, invocation, func(run sessionRunRecord) bool { return run.terminal() })
	if err != nil || outcome.State != "completed" {
		t.Fatalf("prepared outcome lost after crash: %+v err=%v", outcome, err)
	}
	if outcome.RunID != admitted.RunID {
		t.Fatal("recovery changed run identity")
	}
	if after := assertTerminalHistory(t, outcome); after != before {
		t.Fatalf("recovery duplicated output: before=%+v after=%+v", before, after)
	}
	peer := mustDial(t, env.secondaryURL, fixture)
	defer closeWebSocket(peer)
	if err := subscribeRuntime(peer, sessionID); err != nil {
		t.Fatal(err)
	}
	if events, err := readUntil(peer, 10*time.Second, func(event wsEvent) bool {
		return eventRunID(event) == outcome.RunID && eventState(event) == "completed"
	}); err != nil {
		t.Fatalf("durable terminal did not repair live projection: %v events=%+v", err, events)
	}
	_, next := mustSendAndAccept(t, fixture, peer, sessionID, "next-"+marker, directive(uniqueMarker("after-finish-crash"), 1, 0))
	mustReadRunCompleted(t, peer, next.RunID)
}
