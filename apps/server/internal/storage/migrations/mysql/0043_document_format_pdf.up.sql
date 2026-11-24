-- 允许 documents.format 取 pdf（ONLYOFFICE pdf 文档类型）。
ALTER TABLE documents DROP CHECK ck_documents_format;

ALTER TABLE documents ADD CONSTRAINT ck_documents_format CHECK (format IN ('markdown', 'docx', 'xlsx', 'pdf'));
