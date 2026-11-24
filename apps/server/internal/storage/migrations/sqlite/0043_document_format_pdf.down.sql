-- 回滚：把 documents.format 的 CHECK 恢复为 markdown/docx/xlsx。
-- 注意：若库里已存在 format = 'pdf' 的文档，本回滚会因 CHECK 校验失败而中断，
-- 需要先删除或转换这些文档。
PRAGMA writable_schema = ON;

UPDATE sqlite_master
SET sql = REPLACE(
		sql,
		'CHECK (format IN (''markdown'', ''docx'', ''xlsx'', ''pdf''))',
		'CHECK (format IN (''markdown'', ''docx'', ''xlsx''))'
	)
WHERE type = 'table'
	AND name = 'documents'
	AND sql LIKE '%CHECK (format IN (''markdown'', ''docx'', ''xlsx'', ''pdf''))%';

PRAGMA writable_schema = RESET;

CREATE TABLE IF NOT EXISTS __documents_format_bump (id INTEGER);

DROP TABLE IF EXISTS __documents_format_bump;
