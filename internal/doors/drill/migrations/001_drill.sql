-- Trainer data. The first MVP kept these tables in "public": move them.
CREATE SCHEMA IF NOT EXISTS drill;

DO $$
BEGIN
    IF to_regclass('public.drafts') IS NOT NULL THEN
        ALTER TABLE public.drafts      SET SCHEMA drill;
        ALTER TABLE public.submissions SET SCHEMA drill;
        ALTER TABLE public.activity    SET SCHEMA drill;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS drill.drafts (
    user_id    BIGINT      NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    task_slug  TEXT        NOT NULL,
    lang       TEXT        NOT NULL,
    code       TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, task_slug, lang)
);

CREATE TABLE IF NOT EXISTS drill.submissions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    task_slug   TEXT        NOT NULL,
    lang        TEXT        NOT NULL,
    kind        TEXT        NOT NULL CHECK (kind IN ('run', 'submit')),
    code        TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'queued',
    passed      INT,
    total       INT,
    time_ms     INT,
    output      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS submissions_user_time ON drill.submissions (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS submissions_solved ON drill.submissions (user_id, task_slug, lang)
    WHERE kind = 'submit' AND status = 'ok';

CREATE TABLE IF NOT EXISTS drill.activity (
    id        BIGSERIAL PRIMARY KEY,
    user_id   BIGINT      NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    plan_item TEXT        NOT NULL,
    done_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    minutes   INT,
    note      TEXT,
    UNIQUE (user_id, plan_item)
);
