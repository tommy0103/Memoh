//go:build integration

package db_test

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
)

func TestNativeWorkspaceAppsMigration(t *testing.T) {
	ctx := context.Background()
	pool := teamScopedPool(t)
	dsn := teamMigrationDSN(t)
	steps := countMigrationsFrom(t, "0152_native_workspace_apps.up.sql")
	stepDown(t, dsn, steps)
	seedAppBot(t, pool)
	const secondTeam = "00000000-0000-4000-8000-000000000002"
	const secondBot = "20000000-0000-4000-8000-000000000002"
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO teams(id) VALUES ($1)`, []any{secondTeam}},
		{`INSERT INTO team_members(team_id,user_id) VALUES ($1,$2)`, []any{secondTeam, appUserID}},
		{`INSERT INTO bots(id,team_id,owner_user_id,name) VALUES ($1,$2,$3,'second-bot')`, []any{secondBot, secondTeam, appUserID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	nativeIDs := map[string]string{}
	for index, pair := range [][2]string{{appTeamID, appBotOneID}, {secondTeam, secondBot}} {
		teamID, botID := pair[0], pair[1]
		connectionID := fmt.Sprintf("connection-%d", index)
		if _, err := pool.Exec(ctx, `INSERT INTO connectors(team_id,bot_id,connection_id,alias) VALUES ($1,$2,$3,'notion')`, teamID, botID, connectionID); err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"native", "remote-computer", ""} {
			var installationID string
			if err := pool.QueryRow(ctx, `INSERT INTO bot_app_installations(team_id,bot_id,workspace_target_id,registry_id,app_id,revision,status,version)
    VALUES ($1,$2,$3,'memoh','documents',$4,'installed','1.2.3') RETURNING id`, teamID, botID, target, appRevision).Scan(&installationID); err != nil {
				t.Fatal(err)
			}
			if target == "native" {
				nativeIDs[botID] = installationID
			}
			for _, stmt := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO bot_dependency_installations(team_id,bot_id,workspace_target_id,dependency_id,source,status,installed_version)
      VALUES ($1,$2,$3,'python','managed','installed','3.14')`, []any{teamID, botID, target}},
				{`INSERT INTO bot_app_dependency_refs(team_id,installation_id,dependency_id) VALUES ($1,$2,'python')`, []any{teamID, installationID}},
				{`INSERT INTO bot_app_connector_refs(team_id,installation_id,connector_type,connection_id) VALUES ($1,$2,'notion',$3)`, []any{teamID, installationID, connectionID}},
			} {
				if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	check := func() {
		t.Helper()
		for _, table := range []string{"bot_app_installations", "bot_dependency_installations", "bot_app_dependency_refs", "bot_app_connector_refs", "connectors"} {
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM public."+table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 2 {
				t.Fatalf("%s count=%d, want two native records", table, count)
			}
			var enabled, forced bool
			if err := pool.QueryRow(ctx, `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid=$1::regclass`, "public."+table).Scan(&enabled, &forced); err != nil {
				t.Fatal(err)
			}
			if !enabled || !forced {
				t.Fatalf("%s lost RLS", table)
			}
		}
		for botID, id := range nativeIDs {
			var gotID, version, revision string
			if err := pool.QueryRow(ctx, `SELECT id,version,revision FROM bot_app_installations WHERE bot_id=$1`, botID).Scan(&gotID, &version, &revision); err != nil {
				t.Fatal(err)
			}
			if gotID != id || version != "1.2.3" || revision != appRevision {
				t.Fatalf("native installation changed: %s %s %s", gotID, version, revision)
			}
		}
		var targetColumns int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public'
   AND table_name IN ('bot_app_installations','bot_dependency_installations') AND column_name='workspace_target_id'`).Scan(&targetColumns); err != nil {
			t.Fatal(err)
		}
		if targetColumns != 0 {
			t.Fatal("target columns remain")
		}
	}
	stepUp(t, dsn, steps)
	check()
	// DDL guards also support rerunning the cleanup against the canonical schema.
	migrationSQL, err := fs.ReadFile(postgresMigrationsFS(t), "0152_native_workspace_apps.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := pool.Exec(ctx, `INSERT INTO bot_app_installations(bot_id,registry_id,app_id,revision) VALUES ($1,'memoh','documents',$2)`, appBotOneID, appRevision); sqlState(err) != "23505" {
		t.Fatalf("App uniqueness: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bot_dependency_installations(bot_id,dependency_id,source,status) VALUES ($1,'python','managed','installed')`, appBotOneID); sqlState(err) != "23505" {
		t.Fatalf("dependency uniqueness: %v", err)
	}
	stepDown(t, dsn, steps)
	for _, table := range []string{"bot_app_installations", "bot_dependency_installations"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM public."+table+" WHERE workspace_target_id <> 'native'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rollback invented remote rows in %s", table)
		}
	}
	stepUp(t, dsn, steps)
	check()
}
