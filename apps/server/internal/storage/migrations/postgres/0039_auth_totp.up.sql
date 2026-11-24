ALTER TABLE users ADD COLUMN totp_secret VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_last_step BIGINT NOT NULL DEFAULT -1;

CREATE TABLE auth_mfa_challenges (
    subject_key CHAR(64) NOT NULL PRIMARY KEY,
    token_hash CHAR(64) NOT NULL UNIQUE,
    encrypted_state TEXT NOT NULL,
    attempts_left INTEGER NOT NULL,
    expires_at BIGINT NOT NULL
);
CREATE INDEX idx_auth_mfa_challenges_expires_at ON auth_mfa_challenges (expires_at);
