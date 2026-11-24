import { FolderUp } from "lucide-react";
import { useEffect, useRef, useState, type DragEvent } from "react";
import { toast } from "sonner";
import type { WorkspaceFolderManifest } from "../data-access";
import { isHiddenWorkspaceName } from "./workspace-node-name";

export interface FolderUploadSelection {
  files: File[];
  folderManifest: WorkspaceFolderManifest;
}

interface FolderUploadPickerProps {
  disabled?: boolean;
  onSelect: (selection: FolderUploadSelection) => void;
}

export function getWorkspaceFolderName(manifest: WorkspaceFolderManifest): string {
  return (manifest.directories?.[0] ?? manifest.paths[0] ?? "").split("/")[0];
}

async function readFolderSelection(root: FileSystemDirectoryEntry, signal: AbortSignal): Promise<FolderUploadSelection> {
  const files: File[] = [];
  const paths: string[] = [];
  const directories: string[] = [];

  async function readDirectory(directory: FileSystemDirectoryEntry, path: string) {
    directories.push(path);
    const reader = directory.createReader();
    const readEntries = () => new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
    let entries = await readEntries();
    while (entries.length > 0) {
      for (const entry of entries) {
        if (signal.aborted) {
          return;
        }
        if (isHiddenWorkspaceName(entry.name)) continue;
        const entryPath = `${path}/${entry.name}`;
        if (entry.isDirectory) {
          await readDirectory(entry as FileSystemDirectoryEntry, entryPath);
        } else if (entry.isFile) {
          const file = await new Promise<File>((resolve, reject) => {
            (entry as FileSystemFileEntry).file(resolve, reject);
          });
          files.push(file);
          paths.push(entryPath);
        }
      }
      entries = await readEntries();
    }
  }

  await readDirectory(root, root.name);
  return { files, folderManifest: { paths, directories } };
}

export function FolderUploadPicker({ disabled, onSelect }: FolderUploadPickerProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const pendingDropRef = useRef<AbortController | null>(null);
  const [isReading, setIsReading] = useState(false);

  useEffect(() => () => pendingDropRef.current?.abort(), []);
  useEffect(() => {
    if (disabled) {
      pendingDropRef.current?.abort();
      setIsReading(false);
    }
  }, [disabled]);

  async function handleDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    if (disabled || pendingDropRef.current) {
      return;
    }
    const entries = Array.from(event.dataTransfer.items).map((item) => item.webkitGetAsEntry?.());
    if (entries.length !== 1 || !entries[0]?.isDirectory) {
      toast.error("请拖入一个文件夹");
      return;
    }
    if (isHiddenWorkspaceName(entries[0].name)) {
      toast.error("点号开头的文件夹不会上传");
      return;
    }

    const pendingDrop = new AbortController();
    pendingDropRef.current = pendingDrop;
    setIsReading(true);
    try {
      const selection = await readFolderSelection(entries[0] as FileSystemDirectoryEntry, pendingDrop.signal);
      if (!pendingDrop.signal.aborted) {
        onSelect(selection);
      }
    } catch {
      if (!pendingDrop.signal.aborted) {
        toast.error("无法读取文件夹，请重新选择");
      }
    } finally {
      pendingDropRef.current = null;
      if (!pendingDrop.signal.aborted) {
        setIsReading(false);
      }
    }
  }

  return (
    <div
      className="inline-flex shrink-0"
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => { void handleDrop(event); }}
    >
      <input
        ref={inputRef}
        className="hidden"
        type="file"
        multiple
        {...{ webkitdirectory: "" }}
        aria-label="本地文件夹"
        disabled={disabled || isReading}
        onChange={(event) => {
          const files = Array.from(event.currentTarget.files ?? []);
          event.currentTarget.value = "";
          const paths = files.map((file) => file.webkitRelativePath);
          if (files.length === 0 || paths.some((path) => !path)) {
            toast.error("未读取到文件。可以把空文件夹拖到选择按钮上。");
            return;
          }
          const rootName = getWorkspaceFolderName({ paths });
          if (isHiddenWorkspaceName(rootName)) {
            toast.error("点号开头的文件夹不会上传");
            return;
          }
          const visibleFiles: File[] = [];
          const folderManifest: WorkspaceFolderManifest = { paths: [] };
          const visibleDirectories = new Set<string>();
          for (const [index, path] of paths.entries()) {
            const parts = path.split("/");
            const hiddenIndex = parts.findIndex(isHiddenWorkspaceName);
            if (hiddenIndex >= 0) {
              if (hiddenIndex > 0) visibleDirectories.add(parts.slice(0, hiddenIndex).join("/"));
              continue;
            }
            visibleFiles.push(files[index]);
            folderManifest.paths.push(path);
          }
          if (visibleDirectories.size) folderManifest.directories = [...visibleDirectories];
          onSelect({ files: visibleFiles, folderManifest });
        }}
      />
      <button
        type="button"
        className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-lg border border-[#d0d5db] bg-white px-3 text-[13px] text-[#344054] hover:bg-[#f8fafc] focus-visible:outline-2 focus-visible:outline-blue-700 disabled:cursor-not-allowed disabled:opacity-60"
        title="也可以将文件夹拖到这里"
        disabled={disabled || isReading}
        onClick={() => inputRef.current?.click()}
      >
        <FolderUp size={14} aria-hidden />
        {isReading ? "读取中…" : "选择文件夹"}
      </button>
    </div>
  );
}
