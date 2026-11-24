import { useEffect, useId, useRef, useState } from "react";
import type { TreeNode, WorkspaceGateway } from "../data-access";
import { formatError } from "../editor/status-utils";
import { Button } from "./ui/button";

interface UnsupportedFileDialogProps {
  file: TreeNode;
  downloadImportedOriginal: WorkspaceGateway["downloadImportedOriginal"];
  onClose: () => void;
}

export function UnsupportedFileDialog({ file, downloadImportedOriginal, onClose }: UnsupportedFileDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) dialog.showModal();
  }, []);

  async function download() {
    if (!file.documentId || !file.importedOriginalDownloadUrl || pending) return;
    setPending(true);
    setError("");
    try {
      const { blob, fileName } = await downloadImportedOriginal(file.documentId);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = fileName ?? file.title;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 15_000);
    } catch (cause) {
      setError(`下载失败：${formatError(cause)}`);
    } finally {
      setPending(false);
    }
  }

  return (
    <dialog ref={dialogRef} onClose={onClose} aria-labelledby={titleId} aria-describedby={descriptionId}
      className="m-auto w-[calc(100%_-_2rem)] max-w-[440px] rounded-xl border border-solid border-slate-200 bg-white p-5 text-slate-900 shadow-xl backdrop:bg-slate-900/45">
      <h2 id={titleId} className="m-0 text-lg font-semibold">文件无法打开</h2>
      <p id={descriptionId} className="mb-0 mt-3 break-words text-sm">“{file.title}”无法预览。</p>
      {error && <p role="alert" className="mb-0 mt-3 text-sm text-red-700">{error}</p>}
      <form method="dialog" className="mt-4 flex justify-end gap-2">
        <Button type="submit" variant="outline" autoFocus>关闭</Button>
        {file.documentId && file.importedOriginalDownloadUrl && (
          <Button type="button" disabled={pending} onClick={() => { void download(); }}>
            {pending ? "正在下载…" : "下载原文件"}
          </Button>
        )}
      </form>
    </dialog>
  );
}
