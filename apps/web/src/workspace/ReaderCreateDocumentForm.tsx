import { ChevronDown, FileText, FileUp, FolderOpen, FolderPlus, FolderUp, LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Toaster } from "../components/ui/sonner";
import type { TreeNode, WorkspaceGateway } from "../data-access";
import { formatError } from "../editor/status-utils";
import { validateDocumentIdentifier } from "./document-identifier";
import { FolderUploadPicker, getWorkspaceFolderName, type FolderUploadSelection } from "./FolderUploadPicker";
import { isHiddenWorkspaceName } from "./workspace-node-name";

interface ReaderCreateDocumentFormProps {
  spaceId: string;
  parentOrigin: string;
  initialParentId?: string;
  gateway: Pick<WorkspaceGateway, "createNode" | "getTree" | "importDocuments">;
  onCreated: (docId: string, identifier?: string) => void;
  onDocumentImported: (docId: string) => void;
  onCancel: () => void;
  onFolderCreated: (nodeId: string) => void;
  onFolderImported: (nodeId?: string) => void;
}

const CREATE_MODES = [
  { id: "document", label: "新建文档", submitLabel: "创建文档", Icon: FileText },
  { id: "folder", label: "新建文件夹", submitLabel: "创建文件夹", Icon: FolderPlus },
  { id: "upload-file", label: "上传文件", submitLabel: "导入", Icon: FileUp },
  { id: "upload-folder", label: "上传文件夹", submitLabel: "导入", Icon: FolderUp }
] as const;

type CreateMode = typeof CREATE_MODES[number]["id"];

interface SaveLocation {
  id: string;
  path: string;
}

function collectLocations(nodes: TreeNode[]): SaveLocation[] {
  const locations: SaveLocation[] = [];
  function visit(children: TreeNode[], parents: string[]) {
    for (const node of children) {
      const path = [...parents, node.title];
      if (node.importedFileKind !== "unsupported") locations.push({ id: node.id, path: path.join(" / ") });
      visit(node.children, path);
    }
  }
  visit(nodes, []);
  return locations;
}

export function ReaderCreateDocumentForm({
  spaceId,
  parentOrigin,
  initialParentId = "",
  gateway,
  onCreated,
  onDocumentImported,
  onCancel,
  onFolderCreated,
  onFolderImported
}: ReaderCreateDocumentFormProps) {
  const formRef = useRef<HTMLFormElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const submittingRef = useRef(false);
  const partialImportRef = useRef(false);
  const [mode, setMode] = useState<CreateMode>("document");
  const [title, setTitle] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [folderName, setFolderName] = useState("");
  const [locations, setLocations] = useState<SaveLocation[]>([]);
  const [locationLoadFailed, setLocationLoadFailed] = useState(false);
  const [location, setLocation] = useState(initialParentId);
  const [locationsLoaded, setLocationsLoaded] = useState(false);
  const [folderSelection, setFolderSelection] = useState<FolderUploadSelection | null>(null);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [hasPartialImport, setHasPartialImport] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");

  const close = useCallback(() => {
    if (submittingRef.current) return;
    if (partialImportRef.current) {
      onFolderImported();
    } else {
      onCancel();
    }
  }, [onCancel, onFolderImported]);

  useEffect(() => {
    let active = true;
    void gateway.getTree(spaceId).then((nodes) => {
      if (active) {
        setLocations(collectLocations(nodes));
        setLocationsLoaded(true);
      }
    }).catch(() => {
      if (active) setLocationLoadFailed(true);
    });
    return () => { active = false; };
  }, [gateway, spaceId]);

  useEffect(() => {
    function handleParentMessage(event: MessageEvent) {
      if (event.source === window.parent && event.origin === parentOrigin &&
          event.data?.type === "plaindoc:reader-create-close-request") {
        close();
      }
    }
    window.addEventListener("message", handleParentMessage);
    return () => window.removeEventListener("message", handleParentMessage);
  }, [close, parentOrigin]);

  useEffect(() => {
    const form = formRef.current;
    if (!form || window.parent === window) return;
    function reportHeight() {
      if (!form) return;
      window.parent.postMessage({
        type: "plaindoc:reader-create-resize",
        height: Math.ceil(form.getBoundingClientRect().height)
      }, parentOrigin);
    }
    reportHeight();
    const observer = new ResizeObserver(reportHeight);
    observer.observe(form, { box: "border-box" });
    return () => observer.disconnect();
  }, [parentOrigin]);

  function startSubmission() {
    submittingRef.current = true;
    setIsSubmitting(true);
    setError("");
  }

  function endSubmission() {
    submittingRef.current = false;
    setIsSubmitting(false);
  }

  async function createNode() {
    const isFolder = mode === "folder";
    const nameLabel = isFolder ? "文件夹名称" : "文档标题";
    const normalizedTitle = (isFolder ? folderName : title).trim();
    if (!normalizedTitle) {
      setError(`请输入${nameLabel}`);
      return;
    }
    if (isHiddenWorkspaceName(normalizedTitle)) {
      setError(`${nameLabel}不能以点号开头。`);
      return;
    }
    if ([...normalizedTitle].length > 255) {
      setError(`${nameLabel}长度不能超过 255 个字符`);
      return;
    }
    let documentIdentifier: string | undefined;
    if (!isFolder) {
      const validation = validateDocumentIdentifier(identifier, { allowEmpty: true });
      if (validation.error) {
        setError(validation.error);
        return;
      }
      documentIdentifier = validation.value ?? undefined;
    }

    startSubmission();
    try {
      const created = await gateway.createNode({
        spaceId,
        parentId: location || null,
        type: isFolder ? "folder" : "doc",
        title: normalizedTitle,
        documentIdentifier
      });
      if (isFolder) {
        onFolderCreated(created.nodeId);
        return;
      }
      if (created.docId) {
        onCreated(created.docId, documentIdentifier);
        return;
      }
      setError("文档已创建，请在目录中打开。");
    } catch (cause) {
      setError(`创建失败：${formatError(cause)}`);
    }
    endSubmission();
  }

  const uploadFolderMode = mode === "upload-folder";
  const uploadFileMode = mode === "upload-file";
  const uploadMode = uploadFolderMode || uploadFileMode;
  const selectedArchive = uploadFileMode && selectedFile !== null && /\.zip$/i.test(selectedFile.name);

  async function importSelection() {
    const selection = uploadFolderMode ? folderSelection : selectedFile ? { files: [selectedFile] } : null;
    if (!selection || partialImportRef.current) return;

    startSubmission();
    try {
      const result = await gateway.importDocuments({
        spaceId,
        targetNodeId: location || null,
        ...selection,
        autoExtractTitle: false
      });
      if (result.failedCount === 0) {
        if (uploadFolderMode || selectedArchive) {
          const archiveFolder = selectedArchive
            ? result.createdNodes.find((node) => node.type === "folder" && (node.parentId ?? "") === location)
            : undefined;
          onFolderImported(archiveFolder?.nodeId);
          return;
        }
        const documentId = result.items.find((item) => item.status === "success" && item.detectedType !== "unsupported")?.createdDocumentId
          ?? result.createdNodes.find((node) => node.type === "doc" && node.importedFileKind !== "unsupported")?.documentId;
        if (documentId) {
          onDocumentImported(documentId);
          return;
        }
        if (result.createdNodes.length > 0) {
          onFolderImported(location || undefined);
          return;
        }
      }
      const partiallyImported = result.createdNodes.length > 0;
      partialImportRef.current = partiallyImported;
      setHasPartialImport(partiallyImported);
      const firstFailure = result.failureItems[0];
      const detail = firstFailure?.errorMessage ? `。${firstFailure.sourceName}：${firstFailure.errorMessage}` : "";
      setError(result.failedCount > 0
        ? `有 ${result.failedCount} 项导入失败${detail}`
        : partiallyImported ? "文件已导入，请在目录中打开。" : "未能导入文件，请重新选择。");
    } catch (cause) {
      setError(`导入失败：${formatError(cause)}`);
    }
    endSubmission();
  }

  const locationUnavailable = Boolean(location) && !locations.some((item) => item.id === location);
  const controlsDisabled = isSubmitting || hasPartialImport;
  const folderMode = mode === "folder";
  const submitLabel = CREATE_MODES.find((item) => item.id === mode)?.submitLabel;

  return (
    <div className="h-full overflow-y-auto overscroll-contain">
      <form
        ref={formRef}
        className="m-0 text-sm text-slate-900"
        aria-busy={isSubmitting}
        onSubmit={(event) => {
          event.preventDefault();
          if (submittingRef.current || locationUnavailable) return;
          void (uploadMode ? importSelection() : createNode());
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape" && !event.nativeEvent.isComposing) {
            event.preventDefault();
            close();
          }
        }}
      >
        <Toaster />
        <div className="grid min-w-0 gap-4 px-4 pt-3 pb-4 min-[420px]:gap-5 min-[420px]:px-6 min-[420px]:pb-5">
          <div className="grid grid-cols-2 gap-1 rounded-xl bg-slate-100 p-1 min-[330px]:grid-cols-4" role="group" aria-label="添加方式">
            {CREATE_MODES.map(({ id, label, Icon }) => (
              <button
                key={id}
                type="button"
                className="inline-flex min-h-11 min-w-0 cursor-pointer flex-col items-center justify-center gap-0.5 rounded-lg border border-solid border-transparent bg-transparent px-1 py-1.5 text-xs font-semibold text-slate-600 transition-colors hover:bg-slate-200/60 hover:text-slate-900 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-blue-600 aria-pressed:border-blue-200 aria-pressed:bg-white aria-pressed:text-blue-700 aria-pressed:shadow-xs disabled:cursor-not-allowed disabled:opacity-55 min-[420px]:flex-row min-[420px]:gap-2 min-[420px]:text-sm"
                aria-pressed={mode === id}
                disabled={controlsDisabled}
                onClick={() => {
                  setMode(id);
                  setError("");
                }}
              >
                <Icon size={16} aria-hidden="true" />
                <span>{label}</span>
              </button>
            ))}
          </div>

          {uploadFolderMode ? (
            <div className="grid min-w-0 gap-2 rounded-xl border border-dashed border-slate-300 bg-slate-50 px-3.5 py-3 [&>div]:w-full [&>div>button]:w-full">
              <FolderUploadPicker disabled={controlsDisabled} onSelect={(selection) => {
                setFolderSelection(selection);
                setError("");
              }} />
              {folderSelection ? (
                <p className="m-0 min-w-0 text-sm break-all text-slate-700">
                  {getWorkspaceFolderName(folderSelection.folderManifest)} · {folderSelection.files.length} 个文件
                </p>
              ) : (
                <p className="m-0 text-center text-xs text-slate-600">选择文件夹，或将它拖到按钮上。</p>
              )}
            </div>
          ) : uploadFileMode ? (
            <div className="grid min-w-0 gap-2 rounded-xl border border-dashed border-slate-300 bg-slate-50 px-3.5 py-3">
              <input
                ref={fileInputRef}
                className="hidden"
                type="file"
                aria-label="本地文件"
                disabled={controlsDisabled}
                onChange={(event) => {
                  const file = event.currentTarget.files?.[0];
                  event.currentTarget.value = "";
                  if (!file) return;
                  if (isHiddenWorkspaceName(file.name)) {
                    setSelectedFile(null);
                    setError("点号开头的文件不会上传。");
                    return;
                  }
                  setSelectedFile(file);
                  setError("");
                }}
              />
              <Button type="button" variant="outline" className="rounded-lg" disabled={controlsDisabled}
                onClick={() => fileInputRef.current?.click()}>
                <FileUp size={14} aria-hidden="true" />
                {selectedFile ? "更换文件" : "选择文件"}
              </Button>
              <p className={`m-0 min-w-0 ${selectedFile ? "break-all text-sm text-slate-700" : "text-center text-xs text-slate-600"}`}>
                {selectedFile ? selectedFile.name : "支持 CSV 和 Excel 表格；CSV 将转换为 .xlsx。ZIP 会解压到同名目录。"}
              </p>
            </div>
          ) : (
            <div className="grid min-w-0 gap-4">
              <label className="grid min-w-0 gap-1.5 font-medium text-slate-800">
                <span>{folderMode ? "文件夹名称" : "文档标题"}</span>
                <Input
                  key={mode}
                  autoFocus
                  required
                  value={folderMode ? folderName : title}
                  placeholder={folderMode ? "例如：项目资料" : "例如：项目计划"}
                  className="h-10 rounded-lg border-slate-300 bg-white shadow-none focus-visible:ring-blue-600"
                  disabled={isSubmitting}
                  onChange={(event) => {
                    if (folderMode) setFolderName(event.target.value);
                    else setTitle(event.target.value);
                    setError("");
                  }}
                />
              </label>
              {!folderMode && (
                <label className="grid min-w-0 gap-1.5 font-medium text-slate-800">
                  <span>文档标识<span className="font-normal text-slate-600">（可选）</span></span>
                  <Input
                    value={identifier}
                    placeholder="留空自动生成阅读链接"
                    className="h-10 rounded-lg border-slate-300 bg-white shadow-none focus-visible:ring-blue-600"
                    disabled={isSubmitting}
                    onChange={(event) => {
                      setIdentifier(event.target.value);
                      setError("");
                    }}
                  />
                </label>
              )}
            </div>
          )}

          <label className="grid min-w-0 gap-1.5 font-medium text-slate-800">
            <span>保存位置</span>
            <span className="relative block min-w-0">
              <FolderOpen size={16} aria-hidden="true" className="pointer-events-none absolute top-3 left-3 text-slate-500" />
              <select
                className="h-10 w-full min-w-0 cursor-pointer appearance-none truncate rounded-lg border border-solid border-slate-300 bg-white pr-8 pl-9 text-sm text-slate-700 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-blue-600 disabled:cursor-not-allowed disabled:opacity-60"
                value={location}
                disabled={controlsDisabled}
                aria-describedby={locationLoadFailed ? "reader-create-location-error" : undefined}
                onChange={(event) => {
                  setLocation(event.target.value);
                  setError("");
                }}
              >
                <option value="">空间根目录</option>
                {locationUnavailable && (
                  <option value={location} disabled>
                    {locationsLoaded || locationLoadFailed ? "所选位置不可用，请重新选择" : "正在加载所选位置…"}
                  </option>
                )}
                {locations.map((item) => <option key={item.id} value={item.id}>{item.path}</option>)}
              </select>
              <ChevronDown size={15} aria-hidden="true" className="pointer-events-none absolute top-3 right-3 text-slate-600" />
            </span>
          </label>

          {locationLoadFailed && (
            <p id="reader-create-location-error" className="m-0 text-xs text-slate-600">暂时无法加载保存位置，可以选择空间根目录或重新打开表单。</p>
          )}
          {error && (
            <p className="m-0 rounded-lg bg-red-50 px-3 py-2 text-sm break-words text-red-800" role="alert">{error}</p>
          )}
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2 border-0 border-t border-solid border-slate-200 bg-slate-50 px-4 py-3 min-[420px]:px-6">
          {hasPartialImport ? (
            <Button type="button" className="h-10 rounded-lg bg-blue-700 hover:bg-blue-800" onClick={close}>完成</Button>
          ) : (
            <>
              <Button type="button" variant="outline" className="h-10 rounded-lg" onClick={close} disabled={isSubmitting}>取消</Button>
              <Button type="submit" className="h-10 rounded-lg bg-blue-700 hover:bg-blue-800"
                disabled={isSubmitting || locationUnavailable || (uploadFolderMode && !folderSelection) || (uploadFileMode && !selectedFile)}>
                {isSubmitting && <LoaderCircle size={16} className="motion-safe:animate-spin" aria-hidden="true" />}
                {isSubmitting ? (uploadMode ? "导入中…" : "创建中…") : submitLabel}
              </Button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}
