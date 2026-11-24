ALTER TABLE users
  ADD COLUMN normalized_login_name VARCHAR(128) GENERATED ALWAYS AS (LOWER(TRIM(name))) STORED,
  ADD UNIQUE INDEX ux_users_login_name (normalized_login_name);
