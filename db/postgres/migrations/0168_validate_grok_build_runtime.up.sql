-- 0168_validate_grok_build_runtime
-- Separate from 0167: VALIDATE takes SHARE UPDATE EXCLUSIVE, allowing normal
-- reads and writes while scanning. No DROP/ADD belongs in this transaction.
ALTER TABLE bots VALIDATE CONSTRAINT bots_chat_runtime_check;
ALTER TABLE bot_sessions VALIDATE CONSTRAINT bot_sessions_runtime_type_check;
ALTER TABLE bot_history_messages VALIDATE CONSTRAINT bot_history_messages_runtime_type_check;
ALTER TABLE schedule VALIDATE CONSTRAINT schedule_runtime_type_check;
ALTER TABLE agent_authorizations VALIDATE CONSTRAINT agent_authorizations_runtime_check;

-- As in 0141/0144, legacy schedule shapes must not block the upgrade.
DO $schedule_acp_fields_check$
BEGIN
  ALTER TABLE schedule VALIDATE CONSTRAINT schedule_acp_fields_check;
EXCEPTION
  WHEN check_violation THEN NULL;
END
$schedule_acp_fields_check$;
