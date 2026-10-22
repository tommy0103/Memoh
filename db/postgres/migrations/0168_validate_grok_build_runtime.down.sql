-- 0168_validate_grok_build_runtime
-- Restore 0167's NOT VALID state without scanning or changing admitted data.
-- PostgreSQL has no UNVALIDATE operation; recreate the same CHECK constraints.

ALTER TABLE bots
  DROP CONSTRAINT IF EXISTS bots_chat_runtime_check;
ALTER TABLE bots
  ADD CONSTRAINT bots_chat_runtime_check
  CHECK (chat_runtime IN ('model', 'acp_agent', 'codex', 'claude-code', 'grok-build')) NOT VALID;

ALTER TABLE bot_sessions
  DROP CONSTRAINT IF EXISTS bot_sessions_runtime_type_check;
ALTER TABLE bot_sessions
  ADD CONSTRAINT bot_sessions_runtime_type_check
  CHECK (runtime_type IN ('model', 'acp_agent', 'codex', 'claude-code', 'grok-build')) NOT VALID;

ALTER TABLE bot_history_messages
  DROP CONSTRAINT IF EXISTS bot_history_messages_runtime_type_check;
ALTER TABLE bot_history_messages
  ADD CONSTRAINT bot_history_messages_runtime_type_check
  CHECK (runtime_type IN ('model', 'acp_agent', 'codex', 'claude-code', 'grok-build')) NOT VALID;

ALTER TABLE schedule
  DROP CONSTRAINT IF EXISTS schedule_runtime_type_check;
ALTER TABLE schedule
  ADD CONSTRAINT schedule_runtime_type_check
  CHECK (runtime_type IS NULL OR runtime_type IN ('model', 'acp_agent', 'codex', 'claude-code', 'grok-build')) NOT VALID;

ALTER TABLE agent_authorizations
  DROP CONSTRAINT IF EXISTS agent_authorizations_runtime_check;
ALTER TABLE agent_authorizations
  ADD CONSTRAINT agent_authorizations_runtime_check
  CHECK (runtime IN ('codex', 'claude-code', 'grok-build')) NOT VALID;

-- The execution-shape constraint returns to NOT VALID as well.
DO $schedule_acp_fields_check$
BEGIN
  ALTER TABLE schedule
    DROP CONSTRAINT IF EXISTS schedule_acp_fields_check;
  ALTER TABLE schedule
    ADD CONSTRAINT schedule_acp_fields_check
    CHECK (
      run_target <> 'new_session'
      OR (runtime_type = 'acp_agent' AND acp_agent_id IS NOT NULL AND model_id IS NULL)
      OR (runtime_type IN ('codex', 'claude-code', 'grok-build') AND bot_agent_id IS NOT NULL AND acp_agent_id IS NULL AND model_id IS NULL)
      OR (COALESCE(runtime_type, 'model') = 'model' AND bot_agent_id IS NULL AND acp_agent_id IS NULL AND acp_model_id IS NULL)
    ) NOT VALID;

END
$schedule_acp_fields_check$;
