const MAX_DOCUMENT_IDENTIFIER_LENGTH = 80;
const DOCUMENT_IDENTIFIER_PATTERN = /^[a-z0-9-]+$/;
const RESERVED_DOCUMENT_IDENTIFIERS = new Set(["admin", "api", "explore", "login", "register", "search"]);

export function validateDocumentIdentifier(rawValue: string, options: { allowEmpty: boolean }): {
  value: string | null;
  error: string | null;
} {
  const normalizedValue = rawValue.trim().toLowerCase();
  if (!normalizedValue) {
    if (options.allowEmpty) {
      return { value: null, error: null };
    }
    return { value: null, error: "文档标识不能为空" };
  }
  if (normalizedValue.length > MAX_DOCUMENT_IDENTIFIER_LENGTH) {
    return { value: null, error: `文档标识长度不能超过 ${MAX_DOCUMENT_IDENTIFIER_LENGTH} 个字符` };
  }
  if (!DOCUMENT_IDENTIFIER_PATTERN.test(normalizedValue)) {
    return { value: null, error: "文档标识仅支持小写字母、数字和连字符（-）" };
  }
  if (normalizedValue.startsWith("-") || normalizedValue.endsWith("-")) {
    return { value: null, error: "文档标识不能以连字符（-）开头或结尾" };
  }
  if (RESERVED_DOCUMENT_IDENTIFIERS.has(normalizedValue)) {
    return { value: null, error: "该标识为系统保留词，请更换其他标识" };
  }
  return { value: normalizedValue, error: null };
}
