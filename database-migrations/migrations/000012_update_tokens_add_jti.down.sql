DROP INDEX IF EXISTS idx_tokens_jti;
ALTER TABLE tokens DROP COLUMN IF EXISTS jti;
ALTER TABLE tokens ALTER COLUMN refresh_token TYPE UUID USING refresh_token::uuid;
