import { useRef, useState } from "react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Toaster } from "../components/ui/sonner";
import type { WorkspaceGateway } from "../data-access";
import { formatError } from "../editor/status-utils";
import { validateDocumentIdentifier } from "./document-identifier";
import { FolderUploadPicker, getWorkspaceFolderName, type FolderUploadSelection } from "./FolderUploadPicker";

interface ReaderCreateDocumentFormProps {
  spaceId: string;
  gateway: WorkspaceGateway;
  onCreated: (docId: string, identifier?: string) => void;
  onCancel: () => void;
  onFolderImported: () => void;
}

export function ReaderCreateDocumentForm({
  spaceId,
  gateway,
  onCreated,
  onCancel,
  onFolderImported
}: ReaderCreateDocumentFormProps) {
  const [mode, setMode] = useState<"document" | "folder">("document");
  const [title, setTitle] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [folderSelection, setFolderSelection] = useState<FolderUploadSelection | null>(null);
  const [folderImported, setFolderImported] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");
  const submittingRef = useRef(false);

  async function createDocument() {
    if (submittingRef.current) return;

    const normalizedTitle = title.trim();
    if (!normalizedTitle) {
      setError("请输入文档标题");
      return;
    }
    if ([...normalizedTitle].length > 255) {
      setError("文档标题长度不能超过 255 个字符");
      return;
    }
    const identifierValidation = validateDocumentIdentifier(identifier, { allowEmpty: true });
    if (identifierValidation.error) {
      setError(identifierValidation.error);
      return;
    }

    const documentIdentifier = identifierValidation.value ?? undefined;
    submittingRef.current = true;
    setIsSubmitting(true);
    setError("");
    let docId: string | undefined;
    try {
      const created = await gateway.createNode({
        spaceId,
        parentId: null,
        type: "doc",
        title: normalizedTitle,
        documentIdentifier
      });
      docId = created.docId;
    } catch (cause) {
      setError(`创建失败：${formatError(cause)}`);
      return;
    } finally {
      if (!docId) {
        submittingRef.current = false;
        setIsSubmitting(false);
      }
    }

    if (docId) {
      onCreated(docId, documentIdentifier);
    } else {
      setError("文档已创建，暂时无法进入编辑。请关闭弹窗后在文档目录中查看。");
    }
  }

  async function importFolder() {
    if (submittingRef.current || !folderSelection || folderImported) return;

    submittingRef.current = true;
    setIsSubmitting(true);
    setError("");
    try {
      const result = await gateway.importDocuments({
        spaceId,
        targetNodeId: null,
        files: folderSelection.files,
        folderManifest: folderSelection.folderManifest,
        autoExtractTitle: false
      });
      if (result.failedCount === 0) {
        onFolderImported();
        return;
      }
      setFolderImported(result.createdNodes.length > 0);
      const firstFailure = result.failureItems[0];
      const detail = firstFailure?.errorMessage ? `。${firstFailure.sourceName}：${firstFailure.errorMessage}` : "";
      setError(`有 ${result.failedCount} 项导入失败${detail}`);
    } catch (cause) {
      setError(`导入失败：${formatError(cause)}`);
    } finally {
      submittingRef.current = false;
      setIsSubmitting(false);
    }
  }

  function close() {
    if (submittingRef.current) return;
    if (folderImported) {
      onFolderImported();
      return;
    }
    onCancel();
  }

  function changeMode(nextMode: typeof mode) {
    setMode(nextMode);
    setError("");
  }

  return (
    <form
      className="grid h-full content-start gap-4 overflow-y-auto p-4 text-slate-900 sm:px-5"
      aria-busy={isSubmitting}
      onSubmit={(event) => {
        event.preventDefault();
        if (mode === "folder") {
          void importFolder();
        } else {
          void createDocument();
        }
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape" && !event.nativeEvent.isComposing) {
          event.preventDefault();
          close();
        }
      }}
    >
      <Toaster />
      <div className="flex gap-2" role="group" aria-label="创建方式">
        <Button type="button" size="sm" variant={mode === "document" ? "secondary" : "ghost"} aria-pressed={mode === "document"}
          onClick={() => changeMode("document")} disabled={isSubmitting || folderImported}>新建文档</Button>
        <Button type="button" size="sm" variant={mode === "folder" ? "secondary" : "ghost"} aria-pressed={mode === "folder"}
          onClick={() => changeMode("folder")} disabled={isSubmitting || folderImported}>上传文件夹</Button>
      </div>
      {mode === "folder" ? (
        <div className="grid justify-items-start gap-2">
          <FolderUploadPicker disabled={isSubmitting || folderImported} onSelect={(selection) => {
            setFolderSelection(selection);
            setError("");
          }} />
          {folderSelection && (
            <p className="m-0 break-all text-sm text-slate-700">
              {getWorkspaceFolderName(folderSelection.folderManifest)} · {folderSelection.files.length} 个文件
            </p>
          )}
        </div>
      ) : (
        <>
          <label className="grid gap-1.5 text-sm font-medium text-slate-700">
            <span>文档标题</span>
            <Input autoFocus required value={title} onChange={(event) => setTitle(event.target.value)} disabled={isSubmitting} />
          </label>
          <label className="grid gap-1.5 text-sm font-medium text-slate-700">
            <span>文档标识（可选）</span>
            <Input value={identifier} onChange={(event) => setIdentifier(event.target.value)} disabled={isSubmitting} />
          </label>
        </>
      )}
      {error && <p className="m-0 text-sm text-red-700" role="alert">{error}</p>}
      <div className="flex justify-end gap-2">
        {folderImported ? (
          <Button type="button" onClick={close}>完成</Button>
        ) : (
          <>
            <Button type="button" variant="secondary" onClick={close} disabled={isSubmitting}>取消</Button>
            {mode === "folder" ? (
              <Button type="submit" disabled={isSubmitting || !folderSelection}>{isSubmitting ? "导入中…" : "导入"}</Button>
            ) : (
              <Button type="submit" disabled={isSubmitting}>{isSubmitting ? "创建中…" : "创建"}</Button>
            )}
          </>
        )}
      </div>
    </form>
  );
}
