-- 回滚：恢复为 markdown/docx/xlsx。若已存在 pdf 文档会因 CHECK 校验失败而中断。
ALTER TABLE documents DROP CHECK ck_documents_format;

ALTER TABLE documents ADD CONSTRAINT ck_documents_format CHECK (format IN ('markdown', 'docx', 'xlsx'));
