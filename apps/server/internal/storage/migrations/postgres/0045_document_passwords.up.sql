CREATE TABLE IF NOT EXISTS document_passwords (
	document_id VARCHAR(26) NOT NULL PRIMARY KEY REFERENCES documents(document_id) ON DELETE CASCADE,
	password_hash TEXT NOT NULL CHECK (LENGTH(password_hash) > 0),
	generation VARCHAR(26) NOT NULL CHECK (LENGTH(generation) = 26)
);

CREATE INDEX IF NOT EXISTS idx_file_blobs_provider_object_key
	ON file_blobs(storage_provider, object_key);
CREATE INDEX IF NOT EXISTS idx_document_attachments_provider_object_key
	ON document_attachments(storage_provider, object_key, status);
