ALTER TABLE document_permissions ADD COLUMN export_denied BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE document_permissions ADD COLUMN inherit_access BOOLEAN NOT NULL DEFAULT FALSE;
