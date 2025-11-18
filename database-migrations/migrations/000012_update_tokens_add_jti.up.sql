ALTER TABLE tokens ADD COLUMN IF NOT EXISTS jti UUID;
ALTER TABLE tokens ALTER COLUMN refresh_token TYPE TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tokens_jti ON tokens(jti);

UPDATE tokens SET jti = gen_random_uuid() WHERE jti IS NULL;

ALTER TABLE tokens ALTER COLUMN jti SET NOT NULL;
