-- Identity shared by every door: users, their SSH keys, link codes.
-- Databases created by the first MVP kept these tables in "public":
-- move them (with data, sequences and indexes) instead of recreating.
CREATE SCHEMA IF NOT EXISTS core;

DO $$
BEGIN
    IF to_regclass('public.users') IS NOT NULL THEN
        ALTER TABLE public.users      SET SCHEMA core;
        ALTER TABLE public.user_keys  SET SCHEMA core;
        ALTER TABLE public.link_codes SET SCHEMA core;
    END IF;
END $$;

-- the MVP's single-module migration table
DROP TABLE IF EXISTS public.schema_migrations;

CREATE TABLE IF NOT EXISTS core.users (
    id         BIGSERIAL PRIMARY KEY,
    nick       TEXT        NOT NULL UNIQUE,
    tz         TEXT        NOT NULL DEFAULT 'UTC',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS core.user_keys (
    fingerprint TEXT PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    pubkey      TEXT        NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS core.link_codes (
    code       TEXT PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);
