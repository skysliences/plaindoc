CREATE TABLE auth_passkeys (
    passkey_id VARCHAR(26) NOT NULL PRIMARY KEY,
    credential_id_hash CHAR(64) NOT NULL UNIQUE,
    user_id VARCHAR(26) NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    encrypted_credential TEXT NOT NULL,
    mfa_generation INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP NULL
);
CREATE INDEX idx_auth_passkeys_user_id ON auth_passkeys (user_id);

CREATE TABLE auth_passkey_challenges (
    token_hash CHAR(64) NOT NULL PRIMARY KEY,
    user_id VARCHAR(26) NOT NULL,
    purpose VARCHAR(16) NOT NULL,
    session_json TEXT NOT NULL,
    expires_at BIGINT NOT NULL
);
CREATE INDEX idx_auth_passkey_challenges_expires_at ON auth_passkey_challenges (expires_at);
