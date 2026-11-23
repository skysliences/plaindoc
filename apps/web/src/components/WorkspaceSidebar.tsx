import { ChevronRight, FileQuestionMark, FileSpreadsheet, FileText, Folder } from "lucide-react";
import { memo } from "react";
import { toast } from "sonner";
import type {
  CreateNodeResult,
  DocumentFormat,
  DocumentShareConfig,
  DocumentTemplateDetail,
  DocumentTemplateSummary,
  ImportWorkspaceDocumentsResult,
  NodeType,
  TreeNode,
  UpdateDocumentShareInput,
  Visibility,
  WorkspaceFolderManifest
} from "../data-access";
import { WorkspaceTree } from "./WorkspaceTree";

function containsDocument(nodes: TreeNode[], docId: string): boolean {
  return nodes.some((node) => node.documentId === docId || containsDocument(node.children, docId));
}

function EditableDocumentList({
  nodes, activeDocId, onOpenDocument
}: Pick<WorkspaceSidebarProps, "activeDocId" | "onOpenDocument"> & { nodes: TreeNode[] }) {
  return (
    <ul className="space-y-1 pl-2">
      {nodes.map((node) => {
        const children = node.children.length > 0 ? (
            <EditableDocumentList nodes={node.children} activeDocId={activeDocId} onOpenDocument={onOpenDocument} />
        ) : null;

        if (node.type === "folder") {
          return (
            <li key={node.id}>
              <details className="group/folder" open={!!activeDocId && containsDocument(node.children, activeDocId)}>
                <summary className="flex cursor-pointer list-none items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-slate-200 [&::-webkit-details-marker]:hidden">
                  <ChevronRight size={13} className="shrink-0 text-slate-500 group-open/folder:rotate-90" aria-hidden />
                  <Folder size={15} className="shrink-0 text-slate-600" aria-hidden />
                  <span className="truncate" title={node.title}>{node.title}</span>
                </summary>
                {children}
              </details>
            </li>
          );
        }

        const unsupported = node.importedFileKind === "unsupported";
        const Icon = unsupported ? FileQuestionMark : node.documentFormat === "xlsx" ? FileSpreadsheet : FileText;
        return (
          <li key={node.id}>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 pl-[29px] text-left text-sm hover:bg-slate-200 aria-[current=true]:bg-slate-200"
              aria-current={!!node.documentId && activeDocId === node.documentId}
              title={node.title}
              onClick={() => {
                if (unsupported) {
                  toast.error("无法打开此文件");
                } else if (node.documentId) {
                  void onOpenDocument(node.documentId);
                }
              }}
            >
              <Icon size={15} className="shrink-0 text-slate-600" aria-hidden />
              <span className="truncate">{node.title}</span>
            </button>
            {children}
          </li>
        );
      })}
    </ul>
  );
}

// 工作区侧边栏入参：仅管理“当前空间”文档，不提供空间维护能力。
interface WorkspaceSidebarProps {
  activeSpaceName: string;
  onOpenReaderDirectory?: () => Promise<void>;
  canCreateNodes: boolean;
  onCreateDocumentRequestHandled?: () => void;
  workspaceTree: TreeNode[];
  onOpenDocument: (docId: string) => Promise<void>;
  officeCreationEnabled: boolean;
  onCreateNode: (input: {
    parentId: string | null;
    type: NodeType;
    title: string;
    documentIdentifier?: string;
    templateId?: string;
    format?: DocumentFormat;
    focusEditor?: boolean;
  }) => Promise<CreateNodeResult>;
  onImportDocuments: (input: {
    targetNodeId: string | null;
    files: File[];
    autoExtractTitle: boolean;
    folderManifest?: WorkspaceFolderManifest;
  }) => Promise<ImportWorkspaceDocumentsResult>;
  onListDocumentTemplates: () => Promise<DocumentTemplateSummary[]>;
  onGetDocumentTemplate: (templateId: string) => Promise<DocumentTemplateDetail>;
  onUpdateDocumentIdentifier: (docId: string, identifier: string | null) => Promise<void>;
  onGetDocumentShareConfig: (docId: string) => Promise<DocumentShareConfig>;
  onUpdateDocumentShareConfig: (docId: string, input: UpdateDocumentShareInput) => Promise<DocumentShareConfig>;
  onDisableDocumentShare: (docId: string) => Promise<void>;
  onUpdateDocumentVisibility: (docId: string, visibility: Visibility) => Promise<void>;
  onRenameNode: (nodeId: string, title: string) => Promise<void>;
  onDeleteNode: (nodeId: string) => Promise<void>;
  onMoveNode: (input: { nodeId: string; targetParentId: string | null; targetIndex: number }) => Promise<void>;
  activeDocId: string | null;
}

// 工作区侧边栏：仅承载当前空间目录树，不含顶部空间管理头部。
export const WorkspaceSidebar = memo(function WorkspaceSidebar({
  activeSpaceName,
  onOpenReaderDirectory,
  canCreateNodes,
  onCreateDocumentRequestHandled,
  workspaceTree,
  onOpenDocument,
  officeCreationEnabled,
  onCreateNode,
  onImportDocuments,
  onListDocumentTemplates,
  onGetDocumentTemplate,
  onUpdateDocumentIdentifier,
  onGetDocumentShareConfig,
  onUpdateDocumentShareConfig,
  onDisableDocumentShare,
  onUpdateDocumentVisibility,
  onRenameNode,
  onDeleteNode,
  onMoveNode,
  activeDocId
}: WorkspaceSidebarProps) {
  return (
    <aside
      className="flex h-full min-h-0 max-h-[230px] flex-col overflow-auto bg-[#f3f3f4] p-3 transition-opacity duration-150 md:max-h-none md:px-[10px] md:pt-2 md:pb-3"
      aria-label={`${activeSpaceName} 目录`}
    >
      {onOpenReaderDirectory ? (
        <div className="flex items-center justify-between gap-2 px-2 py-2">
          <h2 className="truncate text-sm font-semibold" title={activeSpaceName}>{activeSpaceName}</h2>
          <button
            type="button"
            className="shrink-0 cursor-pointer text-xs text-slate-600 hover:underline"
            onClick={() => { void onOpenReaderDirectory(); }}
          >
            阅读目录
          </button>
        </div>
      ) : null}
      {canCreateNodes ? <WorkspaceTree
        nodes={workspaceTree}
        onCreateDocumentRequestHandled={onCreateDocumentRequestHandled}
        activeDocId={activeDocId}
        officeCreationEnabled={officeCreationEnabled}
        onOpenDocument={onOpenDocument}
        onCreateNode={onCreateNode}
        onImportDocuments={onImportDocuments}
        onListDocumentTemplates={onListDocumentTemplates}
        onGetDocumentTemplate={onGetDocumentTemplate}
        onUpdateDocumentIdentifier={onUpdateDocumentIdentifier}
        onGetDocumentShareConfig={onGetDocumentShareConfig}
        onUpdateDocumentShareConfig={onUpdateDocumentShareConfig}
        onDisableDocumentShare={onDisableDocumentShare}
        onUpdateDocumentVisibility={onUpdateDocumentVisibility}
        onRenameNode={onRenameNode}
        onDeleteNode={onDeleteNode}
        onMoveNode={onMoveNode}
      /> : <EditableDocumentList nodes={workspaceTree} activeDocId={activeDocId} onOpenDocument={onOpenDocument} />}
    </aside>
  );
});
