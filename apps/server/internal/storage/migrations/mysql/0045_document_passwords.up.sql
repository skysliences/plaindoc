CREATE TABLE IF NOT EXISTS document_passwords (
	document_id VARCHAR(26) NOT NULL PRIMARY KEY,
	password_hash VARCHAR(255) NOT NULL,
	generation VARCHAR(26) NOT NULL,
	CONSTRAINT ck_document_passwords_hash CHECK (CHAR_LENGTH(password_hash) > 0),
	CONSTRAINT ck_document_passwords_generation CHECK (CHAR_LENGTH(generation) = 26),
	CONSTRAINT fk_document_passwords_document_id FOREIGN KEY (document_id) REFERENCES documents(document_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_file_blobs_provider_object_key
	ON file_blobs(storage_provider, object_key);
CREATE INDEX idx_document_attachments_provider_object_key
	ON document_attachments(storage_provider, object_key, status);
