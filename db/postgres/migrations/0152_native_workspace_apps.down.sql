-- 0152_native_workspace_apps
-- Restore the target column and keys for retained native installations.
-- Deleted remote installation records require a pre-upgrade database backup.

ALTER TABLE public.bot_dependency_installations
    DROP CONSTRAINT IF EXISTS bot_dependency_installations_identity_key;
ALTER TABLE public.bot_dependency_installations
    ADD COLUMN IF NOT EXISTS workspace_target_id TEXT NOT NULL DEFAULT 'native';
ALTER TABLE public.bot_dependency_installations
    ALTER COLUMN workspace_target_id DROP DEFAULT;
ALTER TABLE public.bot_dependency_installations
    ADD CONSTRAINT bot_dependency_installations_identity_key UNIQUE (team_id, bot_id, workspace_target_id, dependency_id);

ALTER TABLE public.bot_app_installations
    DROP CONSTRAINT IF EXISTS bot_app_installations_identity_key;
ALTER TABLE public.bot_app_installations
    ADD COLUMN IF NOT EXISTS workspace_target_id TEXT NOT NULL DEFAULT 'native';
ALTER TABLE public.bot_app_installations
    ALTER COLUMN workspace_target_id DROP DEFAULT;
ALTER TABLE public.bot_app_installations
    ADD CONSTRAINT bot_app_installations_identity_key UNIQUE (team_id, bot_id, workspace_target_id, registry_id, app_id);
