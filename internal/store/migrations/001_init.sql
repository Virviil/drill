CREATE TABLE users (
    id         BIGSERIAL PRIMARY KEY,
    nick       TEXT        NOT NULL UNIQUE,
    tz         TEXT        NOT NULL DEFAULT 'UTC',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One person can log in with several keys.
CREATE TABLE user_keys (
    fingerprint TEXT PRIMARY KEY, -- SHA256:...
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pubkey      TEXT        NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One-time codes to attach a new key to an existing account.
CREATE TABLE link_codes (
    code       TEXT PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Editor autosave.
CREATE TABLE drafts (
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_slug  TEXT        NOT NULL,
    lang       TEXT        NOT NULL,
    code       TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, task_slug, lang)
);

CREATE TABLE submissions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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
CREATE INDEX submissions_user_time ON submissions (user_id, created_at DESC);
CREATE INDEX submissions_solved ON submissions (user_id, task_slug, lang) WHERE kind = 'submit' AND status = 'ok';

-- Non-code work from the plan: a chapter read, a project step, a checkpoint.
CREATE TABLE activity (
    id        BIGSERIAL PRIMARY KEY,
    user_id   BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_item TEXT        NOT NULL,
    done_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    minutes   INT,
    note      TEXT,
    UNIQUE (user_id, plan_item)
);
