-- 0152_native_workspace_apps
-- Restrict App and dependency installations to each bot's isolated workspace.
-- Removing remote records never runs filesystem operations or disconnects connectors.

BEGIN;

-- Like other cross-team cleanup migrations, hold exclusive table locks while
-- bypassing request-scoped policies. Restore FORCE RLS before committing.
ALTER TABLE public.bot_app_installations NO FORCE ROW LEVEL SECURITY;
ALTER TABLE public.bot_app_installations DISABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_dependency_installations NO FORCE ROW LEVEL SECURITY;
ALTER TABLE public.bot_dependency_installations DISABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'bot_app_installations'
          AND column_name = 'workspace_target_id'
    ) THEN
        DELETE FROM public.bot_app_installations WHERE workspace_target_id <> 'native';
    END IF;
END $$;

ALTER TABLE public.bot_app_installations
    DROP CONSTRAINT IF EXISTS bot_app_installations_identity_key;
ALTER TABLE public.bot_app_installations
    DROP COLUMN IF EXISTS workspace_target_id;
ALTER TABLE public.bot_app_installations
    ADD CONSTRAINT bot_app_installations_identity_key UNIQUE (team_id, bot_id, registry_id, app_id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'bot_dependency_installations'
          AND column_name = 'workspace_target_id'
    ) THEN
        DELETE FROM public.bot_dependency_installations WHERE workspace_target_id <> 'native';
    END IF;
END $$;

ALTER TABLE public.bot_dependency_installations
    DROP CONSTRAINT IF EXISTS bot_dependency_installations_identity_key;
ALTER TABLE public.bot_dependency_installations
    DROP COLUMN IF EXISTS workspace_target_id;
ALTER TABLE public.bot_dependency_installations
    ADD CONSTRAINT bot_dependency_installations_identity_key UNIQUE (team_id, bot_id, dependency_id);

ALTER TABLE public.bot_app_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_app_installations FORCE ROW LEVEL SECURITY;
ALTER TABLE public.bot_dependency_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_dependency_installations FORCE ROW LEVEL SECURITY;

COMMIT;
