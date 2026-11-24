ALTER TABLE users ADD COLUMN login_ip_addresses_json VARCHAR(3072) NOT NULL DEFAULT '[]';
