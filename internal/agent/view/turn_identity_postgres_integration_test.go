package view_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	chatview "github.com/felinics/memoh/internal/agent/view"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	dbpkg "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

// The web transcript pairs a live or optimistic turn with its settled twin by
// (turn_id, role), one to one. A converter test over hand-built rows cannot
// vouch for that: it asserts the shape it was handed. This drives the real
// chain instead — persist through the message service, read back through the
// paged UI history query, convert — so a persistence path that ever files a
// second visible user row under an existing turn fails here.
func TestPostgresHistoryPageKeepsTurnRoleIdentityUnique(t *testing.T) {
	ctx := context.Background()
	tx := beginPostgresViewTestTx(t, ctx)
	setupPostgresViewTestFixtures(t, ctx, tx)
	svc := messagepkg.NewService(nil, postgresstore.NewQueries(dbsqlc.New(tx)))

	// A run's request turn is named by admission, which draws its slot from the
	// same session counter. Supplying a slot deliberately does not bump the
	// counter, so the test has to draw one rather than invent a number.
	requestTurnID, requestPosition := allocateTurnSlot(t, ctx, tx)
	request := persistUser(t, ctx, svc, messagepkg.PersistInput{
		Role: "user", DisplayText: "ask", Content: modelJSON(t, "user", "ask"),
		TurnID: requestTurnID, TurnPosition: &requestPosition,
	})
	persistBound(t, ctx, svc, request.ID, "assistant", "first half")

	// An applied steer is a mid-run user input, and every visible user row opens
	// its own canonical turn (SR-TURN-001) — that is exactly what keeps
	// (turn_id, role) unique, so the page has to contain one.
	steer := persistUser(t, ctx, svc, messagepkg.PersistInput{
		Role: "user", DisplayText: "steer me", Content: modelJSON(t, "user", "steer me"),
	})
	persistBound(t, ctx, svc, steer.ID, "assistant", "second half")

	// A following round, so the page spans several turns.
	next := persistUser(t, ctx, svc, messagepkg.PersistInput{
		Role: "user", DisplayText: "and then", Content: modelJSON(t, "user", "and then"),
	})
	persistBound(t, ctx, svc, next.ID, "assistant", "third reply")

	for _, limit := range []int32{2, 4, 8, 30} {
		t.Run(fmt.Sprintf("page of %d", limit), func(t *testing.T) {
			assertTurnRoleIdentityUnique(t, chatview.ConvertMessagesToUITurns(mustPage(t, ctx, svc, limit)))
		})
	}

	// The steer must have opened its own turn, numbered after the request it
	// interrupted — that is what keeps (turn_id, role) unique across the page.
	turns := chatview.ConvertMessagesToUITurns(mustPage(t, ctx, svc, 30))
	positions := map[string]int64{}
	for _, turn := range turns {
		if turn.TurnPosition != nil {
			positions[turn.TurnID] = *turn.TurnPosition
		}
	}
	if steer.TurnID == request.TurnID {
		t.Fatalf("the steer reused the request's turn %q; a visible user row must open its own",
			steer.TurnID)
	}
	if positions[steer.TurnID] <= positions[request.TurnID] {
		t.Fatalf("steer turn %d must be numbered after the request turn %d",
			positions[steer.TurnID], positions[request.TurnID])
	}
	ordered := make([]int64, 0, len(turns))
	for _, turn := range turns {
		if turn.TurnPosition != nil {
			ordered = append(ordered, *turn.TurnPosition)
		}
	}
	if !sort.SliceIsSorted(ordered, func(i, j int) bool { return ordered[i] < ordered[j] }) {
		t.Fatalf("history page is not ordered by turn position: %v", ordered)
	}
}

// allocateTurnSlot draws a slot the way admission does: bump the session
// counter and take the number it vacated, without writing a row.
func allocateTurnSlot(t *testing.T, ctx context.Context, tx pgx.Tx) (string, int64) {
	t.Helper()
	var position int64
	if err := tx.QueryRow(ctx, `
		UPDATE bot_sessions SET next_turn_position = next_turn_position + 1
		WHERE id = $1 RETURNING (next_turn_position - 1)::bigint
	`, postgresViewTestSessionID).Scan(&position); err != nil {
		t.Fatalf("allocate turn slot: %v", err)
	}
	return uuid.NewString(), position
}

func assertTurnRoleIdentityUnique(t *testing.T, turns []chatview.UITurn) {
	t.Helper()
	if len(turns) == 0 {
		t.Fatal("history page produced no turns")
	}
	seen := make(map[string]string, len(turns))
	for index, turn := range turns {
		key := fmt.Sprintf("%s\x00%s", turn.TurnID, turn.Role)
		if first, duplicate := seen[key]; duplicate {
			t.Fatalf("turn %d (%s/%s) repeats the identity already used by %s;"+
				" the web transcript pairs turns one to one on (turn_id, role)"+
				" and would drop one of them",
				index, turn.TurnID, turn.Role, first)
		}
		seen[key] = turn.ID
	}
}

// mustPage reproduces what the client actually receives: the query returns the
// newest turns first, and the handler reverses them before rendering.
func mustPage(t *testing.T, ctx context.Context, svc *messagepkg.DBService, limit int32) []messagepkg.Message {
	t.Helper()
	rows, err := svc.ListLatestUIBySession(ctx, postgresViewTestSessionID, limit)
	if err != nil {
		t.Fatalf("read ui history page: %v", err)
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

func persistUser(t *testing.T, ctx context.Context, svc *messagepkg.DBService, in messagepkg.PersistInput) messagepkg.Message {
	t.Helper()
	in.BotID = postgresViewTestBotID
	in.SessionID = postgresViewTestSessionID
	message, err := svc.Persist(ctx, in)
	if err != nil {
		t.Fatalf("persist %s message: %v", in.Role, err)
	}
	return message
}

func persistBound(t *testing.T, ctx context.Context, svc *messagepkg.DBService, requestID, role, text string) messagepkg.Message {
	t.Helper()
	message, err := svc.Persist(ctx, messagepkg.PersistInput{
		BotID: postgresViewTestBotID, SessionID: postgresViewTestSessionID,
		Role: role, DisplayText: text, Content: modelJSON(t, role, text),
		TurnRequestMessageID: requestID,
	})
	if err != nil {
		t.Fatalf("persist %s message: %v", role, err)
	}
	return message
}

func modelJSON(t *testing.T, role, text string) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"role": role, "content": text})
	if err != nil {
		t.Fatalf("encode model message: %v", err)
	}
	return encoded
}

var (
	postgresViewTestUserID    = uuid.NewString()
	postgresViewTestBotID     = uuid.NewString()
	postgresViewTestSessionID = uuid.NewString()
)

func beginPostgresViewTestTx(t *testing.T, ctx context.Context) pgx.Tx {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("skip postgres integration test: TEST_POSTGRES_DSN is not set")
	}
	pool, err := dbpkg.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to configured postgres integration database: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func setupPostgresViewTestFixtures(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	name := fmt.Sprintf("postgres-view-test-%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, `
		WITH created_user AS (
			INSERT INTO users (id, username, is_active)
			VALUES ($1, $2, true) RETURNING id
		)
		INSERT INTO team_members (user_id, role)
		SELECT id, 'admin' FROM created_user
	`, postgresViewTestUserID, name); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO bots (id, owner_user_id, name) VALUES ($1, $2, $3)
	`, postgresViewTestBotID, postgresViewTestUserID, name); err != nil {
		t.Fatalf("insert bot: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO bot_sessions (id, bot_id, channel_type) VALUES ($1, $2, 'local')
	`, postgresViewTestSessionID, postgresViewTestBotID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

// A steer's turn is drawn when the input is claimed, so the name the client was
// shown must be the name history ends up using. This drives the allocator and
// the supplied-identity write path against a live database.
func TestPostgresSteerTurnSlotSurvivesToTheHistoryPage(t *testing.T) {
	ctx := context.Background()
	tx := beginPostgresViewTestTx(t, ctx)
	setupPostgresViewTestFixtures(t, ctx, tx)
	svc := messagepkg.NewService(nil, postgresstore.NewQueries(dbsqlc.New(tx)))

	requestTurnID, requestPosition := allocateTurnSlot(t, ctx, tx)
	request := persistUser(t, ctx, svc, messagepkg.PersistInput{
		Role: "user", DisplayText: "ask", Content: modelJSON(t, "user", "ask"),
		TurnID: requestTurnID, TurnPosition: &requestPosition,
	})
	persistBound(t, ctx, svc, request.ID, "assistant", "first half")

	slot, err := svc.AllocateTurnSlot(ctx, postgresViewTestSessionID)
	if err != nil {
		t.Fatalf("allocate steer turn slot: %v", err)
	}
	if slot.Position <= requestPosition {
		t.Fatalf("steer slot %d must come after the request slot %d", slot.Position, requestPosition)
	}
	steer := persistUser(t, ctx, svc, messagepkg.PersistInput{
		Role: "user", DisplayText: "steer me", Content: modelJSON(t, "user", "steer me"),
		TurnID: slot.TurnID, TurnPosition: &slot.Position,
	})
	persistBound(t, ctx, svc, steer.ID, "assistant", "second half")

	turns := chatview.ConvertMessagesToUITurns(mustPage(t, ctx, svc, 30))
	assertTurnRoleIdentityUnique(t, turns)

	var found bool
	for _, turn := range turns {
		if turn.TurnID != slot.TurnID {
			continue
		}
		found = true
		if turn.TurnPosition == nil || *turn.TurnPosition != slot.Position {
			t.Fatalf("steer turn landed at position %v, want the allocated slot %d",
				turn.TurnPosition, slot.Position)
		}
	}
	if !found {
		t.Fatalf("the allocated steer turn %q is not in the history page", slot.TurnID)
	}
	// The reply after the steer belongs to the steer's turn, not the request's.
	for _, turn := range turns {
		if turn.Role == "assistant" && turn.TurnID == requestTurnID {
			for _, message := range turn.Messages {
				if message.Content == "second half" {
					t.Fatal("post-steer output was filed under the request turn")
				}
			}
		}
	}
}
