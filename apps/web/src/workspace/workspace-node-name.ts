export function isHiddenWorkspaceName(name: string): boolean {
  return name.trim().startsWith(".");
}
