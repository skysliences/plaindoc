CREATE TABLE auth_passkeys (
    passkey_id VARCHAR(26) NOT NULL PRIMARY KEY,
    credential_id_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    user_id VARCHAR(26) NOT NULL,
    name VARCHAR(128) NOT NULL,
    encrypted_credential MEDIUMTEXT NOT NULL,
    mfa_generation BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL,
    last_used_at DATETIME(3) NULL,
    CONSTRAINT fk_auth_passkeys_user_id FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE,
    KEY idx_auth_passkeys_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE auth_passkey_challenges (
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    user_id VARCHAR(26) NOT NULL,
    purpose VARCHAR(16) NOT NULL,
    session_json TEXT NOT NULL,
    expires_at BIGINT NOT NULL,
    KEY idx_auth_passkey_challenges_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
