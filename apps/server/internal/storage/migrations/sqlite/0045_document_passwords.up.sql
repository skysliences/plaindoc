CREATE TABLE IF NOT EXISTS document_passwords (
	document_id TEXT NOT NULL PRIMARY KEY,
	password_hash TEXT NOT NULL CHECK (length(password_hash) > 0),
	generation TEXT NOT NULL CHECK (length(generation) = 26),
	FOREIGN KEY (document_id) REFERENCES documents(document_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_file_blobs_provider_object_key
	ON file_blobs(storage_provider, object_key);
CREATE INDEX IF NOT EXISTS idx_document_attachments_provider_object_key
	ON document_attachments(storage_provider, object_key, status);
