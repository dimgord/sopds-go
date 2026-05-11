-- Track when verification email was last sent — used for resend cooldown.
-- Null for users created before this migration (treated as "no recent send";
-- they can resend immediately). Updated on signup, login-triggered resend,
-- and explicit /resend-verification requests.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS verify_token_sent_at TIMESTAMP;

-- Optional: index for "who hasn't received a recent verification email"
-- queries (e.g. nightly cron to nag stale unverified accounts). Not used
-- by current code but cheap to maintain (small column, narrow type).
CREATE INDEX IF NOT EXISTS idx_users_verify_sent_at
    ON users(verify_token_sent_at)
    WHERE email_verified = FALSE;
