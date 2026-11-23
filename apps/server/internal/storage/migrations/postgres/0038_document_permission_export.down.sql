DELETE FROM document_permissions WHERE inherit_access = TRUE;
ALTER TABLE document_permissions DROP COLUMN inherit_access;
ALTER TABLE document_permissions DROP COLUMN export_denied;
