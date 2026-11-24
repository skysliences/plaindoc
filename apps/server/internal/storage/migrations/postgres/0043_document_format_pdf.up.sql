-- 允许 documents.format 取 pdf（ONLYOFFICE pdf 文档类型）。
ALTER TABLE documents DROP CONSTRAINT IF EXISTS chk_documents_format;

ALTER TABLE documents ADD CONSTRAINT chk_documents_format CHECK (format IN ('markdown', 'docx', 'xlsx', 'pdf'));

COMMENT ON COLUMN documents.format IS '文档正文格式：markdown/docx/xlsx/pdf';
