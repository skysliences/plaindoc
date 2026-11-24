-- 允许 documents.format 取 pdf（ONLYOFFICE pdf 文档类型：可阅读、批注、编辑）。
-- SQLite 无法直接修改列上的 CHECK 约束。这里通过 writable_schema 原地改写表定义，
-- 不做重建表搬运数据，避免其它表对 documents 的 REFERENCES 被 RENAME 改写成临时表名。
PRAGMA writable_schema = ON;

UPDATE sqlite_master
SET sql = REPLACE(
		sql,
		'CHECK (format IN (''markdown'', ''docx'', ''xlsx''))',
		'CHECK (format IN (''markdown'', ''docx'', ''xlsx'', ''pdf''))'
	)
WHERE type = 'table'
	AND name = 'documents'
	AND sql LIKE '%CHECK (format IN (''markdown'', ''docx'', ''xlsx''))%';

PRAGMA writable_schema = RESET;

-- 触发 schema_version 变化，让连接池内其它连接重新加载 schema。
CREATE TABLE IF NOT EXISTS __documents_format_bump (id INTEGER);

DROP TABLE IF EXISTS __documents_format_bump;
