export function readContentDispositionFileName(contentDisposition: string | null): string | null {
  if (!contentDisposition) return null;

  const encodedName = /(?:^|;)\s*filename\*=UTF-8''([^;]+)/i.exec(contentDisposition)?.[1]?.trim();
  if (encodedName) {
    try {
      return decodeURIComponent(encodedName);
    } catch {
      return encodedName;
    }
  }

  const plainName = /(?:^|;)\s*filename=(?:"((?:[^"\\]|\\.)*)"|([^;]+))/i.exec(contentDisposition);
  return (plainName?.[1]?.replace(/\\(.)/g, "$1") ?? plainName?.[2])?.trim() || null;
}
