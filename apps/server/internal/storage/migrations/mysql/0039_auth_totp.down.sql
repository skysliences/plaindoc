DROP TABLE IF EXISTS auth_mfa_challenges;
ALTER TABLE users DROP COLUMN totp_last_step;
ALTER TABLE users DROP COLUMN totp_secret;
