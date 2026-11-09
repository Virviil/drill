-- For "кто онлайн" / "был недавно".
ALTER TABLE core.users ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
