CREATE UNIQUE INDEX ux_users_login_name ON users (LOWER(TRIM(name)));
