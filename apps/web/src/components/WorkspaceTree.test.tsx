import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  CreateNodeResult,
  DocumentShareConfig,
  DocumentTemplateDetail,
  DocumentTemplateSummary,
  ImportWorkspaceDocumentsResult,
  NodeType,
  TreeNode,
  UpdateDocumentShareInput,
  Visibility
} from "../data-access";
import { WorkspaceTree } from "./WorkspaceTree";

const { toastErrorMock } = vi.hoisted(() => ({
  toastErrorMock: vi.fn()
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastErrorMock
  }
}));

function buildTemplateSummary(input: Partial<DocumentTemplateSummary>): DocumentTemplateSummary {
  return {
    templateId: input.templateId ?? "tpl-default",
    sceneKey: input.sceneKey ?? "default",
    sceneName: input.sceneName ?? "默认场景",
    name: input.name ?? "默认模板",
    description: input.description ?? "",
    defaultTitle: input.defaultTitle ?? "",
    sort: input.sort ?? 0,
    builtin: input.builtin ?? false,
    enabled: input.enabled ?? true,
    updatedAt: input.updatedAt ?? "2026-03-04T00:00:00Z"
  };
}

function buildTemplateDetail(input: Partial<DocumentTemplateDetail>): DocumentTemplateDetail {
  return {
    templateId: input.templateId ?? "tpl-default",
    sceneKey: input.sceneKey ?? "default",
    sceneName: input.sceneName ?? "默认场景",
    name: input.name ?? "默认模板",
    description: input.description ?? "",
    defaultTitle: input.defaultTitle ?? "",
    contentMd: input.contentMd ?? "",
    sort: input.sort ?? 0,
    builtin: input.builtin ?? false,
    enabled: input.enabled ?? true,
    createdByUserId: input.createdByUserId,
    updatedByUserId: input.updatedByUserId,
    createdAt: input.createdAt ?? "2026-03-04T00:00:00Z",
    updatedAt: input.updatedAt ?? "2026-03-04T00:00:00Z"
  };
}

function createProps(overrides?: {
  nodes?: TreeNode[];
  activeDocId?: string | null;
  officeCreationEnabled?: boolean;
  onCreateNode?: (input: {
    parentId: string | null;
    type: NodeType;
    title: string;
    documentIdentifier?: string;
    templateId?: string;
    format?: "markdown" | "docx" | "xlsx";
  }) => Promise<CreateNodeResult>;
  onImportDocuments?: (input: {
    targetNodeId: string | null;
    files: File[];
    autoExtractTitle: boolean;
  }) => Promise<ImportWorkspaceDocumentsResult>;
  onListDocumentTemplates?: () => Promise<DocumentTemplateSummary[]>;
  onGetDocumentTemplate?: (templateId: string) => Promise<DocumentTemplateDetail>;
}) {
  const onOpenDocument = vi.fn<(docId: string) => Promise<void>>().mockResolvedValue(undefined);
  const onOpenUnsupportedFile = vi.fn<(node: TreeNode) => void>();
  const onCreateNode =
    overrides?.onCreateNode ??
    vi.fn<
      (input: {
        parentId: string | null;
        type: NodeType;
        title: string;
        documentIdentifier?: string;
        templateId?: string;
        format?: "markdown" | "docx" | "xlsx";
      }) => Promise<CreateNodeResult>
    >().mockResolvedValue({
      nodeId: "node-created",
      docId: "doc-created"
    });
  const onListDocumentTemplates =
    overrides?.onListDocumentTemplates ??
    vi
      .fn<() => Promise<DocumentTemplateSummary[]>>()
      .mockResolvedValue([buildTemplateSummary({ templateId: "tpl-default" })]);
  const onGetDocumentTemplate =
    overrides?.onGetDocumentTemplate ??
    vi
      .fn<(templateId: string) => Promise<DocumentTemplateDetail>>()
      .mockResolvedValue(buildTemplateDetail({ templateId: "tpl-default", contentMd: "# default" }));
  const onImportDocuments =
    overrides?.onImportDocuments ??
    vi
      .fn<
        (input: {
          targetNodeId: string | null;
          files: File[];
          autoExtractTitle: boolean;
        }) => Promise<ImportWorkspaceDocumentsResult>
      >()
      .mockResolvedValue({
        totalCount: 0,
        successCount: 0,
        failedCount: 0,
        items: [],
        failureItems: [],
        createdNodes: []
      });
  const onUpdateDocumentIdentifier = vi
    .fn<(docId: string, identifier: string | null) => Promise<void>>()
    .mockResolvedValue(undefined);
  const onUpdateDocumentVisibility = vi
    .fn<(docId: string, visibility: Visibility) => Promise<void>>()
    .mockResolvedValue(undefined);
  const onGetDocumentShareConfig = vi.fn<(docId: string) => Promise<DocumentShareConfig>>().mockResolvedValue({
    enabled: false,
    shareId: "share-doc-default",
    documentId: "doc-default",
    spaceId: "space-default",
    mode: "public",
    hasPassword: false,
    passwordHint: "",
    expiresAt: null,
    accessVersion: 1,
    createdAt: "2026-03-05T00:00:00Z",
    updatedAt: "2026-03-05T00:00:00Z"
  });
  const onUpdateDocumentShareConfig = vi
    .fn<(docId: string, input: UpdateDocumentShareInput) => Promise<DocumentShareConfig>>()
    .mockResolvedValue({
      enabled: true,
      shareId: "share-doc-default",
      documentId: "doc-default",
      spaceId: "space-default",
      mode: "public",
      hasPassword: false,
      passwordHint: "",
      expiresAt: null,
      accessVersion: 1,
      createdAt: "2026-03-05T00:00:00Z",
      updatedAt: "2026-03-05T00:00:00Z"
    });
  const onDisableDocumentShare = vi.fn<(docId: string) => Promise<void>>().mockResolvedValue(undefined);
  const onRenameNode = vi.fn<(nodeId: string, title: string) => Promise<void>>().mockResolvedValue(undefined);
  const onDeleteNode = vi.fn<(nodeId: string) => Promise<void>>().mockResolvedValue(undefined);
  const onMoveNode = vi
    .fn<(input: { nodeId: string; targetParentId: string | null; targetIndex: number }) => Promise<void>>()
    .mockResolvedValue(undefined);

  return {
    nodes: overrides?.nodes ?? [],
    activeDocId: overrides?.activeDocId ?? null,
    officeCreationEnabled: overrides?.officeCreationEnabled ?? false,
    onOpenDocument,
    onOpenUnsupportedFile,
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
    onMoveNode
  };
}

describe("WorkspaceTree", () => {
  beforeEach(() => {
    toastErrorMock.mockReset();
  });

  it("loads templates, previews selected template, and forwards templateId on create", async () => {
    const templates: DocumentTemplateSummary[] = [
      buildTemplateSummary({
        templateId: "tpl-weekly",
        sceneKey: "report",
        sceneName: "报告",
        name: "周报模板",
        sort: 20
      }),
      buildTemplateSummary({
        templateId: "tpl-meeting",
        sceneKey: "meeting",
        sceneName: "会议",
        name: "会议纪要模板",
        defaultTitle: "会议纪要",
        sort: 10
      })
    ];
    const details: Record<string, DocumentTemplateDetail> = {
      "tpl-meeting": buildTemplateDetail({
        templateId: "tpl-meeting",
        sceneKey: "meeting",
        sceneName: "会议",
        name: "会议纪要模板",
        defaultTitle: "会议纪要",
        contentMd: "# 会议目标\n\n- 事项 A"
      }),
      "tpl-weekly": buildTemplateDetail({
        templateId: "tpl-weekly",
        sceneKey: "report",
        sceneName: "报告",
        name: "周报模板",
        contentMd: "# 本周进展"
      })
    };

    const onListDocumentTemplates = vi.fn<() => Promise<DocumentTemplateSummary[]>>().mockResolvedValue(templates);
    const onGetDocumentTemplate = vi
      .fn<(templateId: string) => Promise<DocumentTemplateDetail>>()
      .mockImplementation(async (templateId: string) => details[templateId]);
    const onCreateNode = vi
      .fn<
        (input: {
          parentId: string | null;
          type: NodeType;
          title: string;
          documentIdentifier?: string;
          templateId?: string;
          format?: "markdown" | "docx" | "xlsx";
        }) => Promise<CreateNodeResult>
      >()
      .mockResolvedValue({
        nodeId: "node-new",
        docId: "doc-new"
      });

    const props = createProps({
      onCreateNode,
      onListDocumentTemplates,
      onGetDocumentTemplate
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "从模板新建" }));

    await screen.findByRole("heading", { name: "新建文档" });
    await waitFor(() => {
      expect(onListDocumentTemplates).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByRole("option", { name: "会议纪要模板 (tpl-meeting)" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "周报模板 (tpl-weekly)" })).toBeInTheDocument();

    const templateSelect = screen.getByLabelText("模板（可选）");
    await user.selectOptions(templateSelect, "tpl-meeting");

    await waitFor(() => {
      expect(onGetDocumentTemplate).toHaveBeenCalledWith("tpl-meeting");
    });
    expect(await screen.findByText("会议纪要模板")).toBeInTheDocument();
    expect(screen.getByText("默认标题：会议纪要")).toBeInTheDocument();
    expect(screen.getByText(/会议目标/)).toBeInTheDocument();
    expect(screen.getByText(/事项 A/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "创建" }));

    await waitFor(() => {
      expect(onCreateNode).toHaveBeenCalledTimes(1);
    });
    expect(onCreateNode).toHaveBeenCalledWith({
      parentId: null,
      type: "doc",
      title: "未命名文档",
      documentIdentifier: undefined,
      templateId: "tpl-meeting"
    });
    await waitFor(() => {
      expect(screen.queryByRole("heading", { name: "新建文档" })).not.toBeInTheDocument();
    });
  });

  it("creates an untitled document ready for typing without opening creation options", async () => {
    const props = createProps();
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "新建文档" }));

    expect(props.onCreateNode).toHaveBeenCalledWith({
      parentId: null, type: "doc", title: "未命名文档", focusEditor: true
    });
    expect(props.onListDocumentTemplates).not.toHaveBeenCalled();
    expect(screen.queryByRole("heading", { name: "新建文档" })).not.toBeInTheDocument();
  });

  it("retries preview loading after template detail request fails", async () => {
    const onGetDocumentTemplate = vi
      .fn<(templateId: string) => Promise<DocumentTemplateDetail>>()
      .mockRejectedValueOnce(new Error("network unavailable"))
      .mockResolvedValue(
        buildTemplateDetail({
          templateId: "tpl-postmortem",
          sceneKey: "incident",
          sceneName: "故障复盘",
          name: "复盘模板",
          contentMd: "# 事故时间线"
        })
      );
    const onListDocumentTemplates = vi
      .fn<() => Promise<DocumentTemplateSummary[]>>()
      .mockResolvedValue([
        buildTemplateSummary({
          templateId: "tpl-postmortem",
          sceneKey: "incident",
          sceneName: "故障复盘",
          name: "复盘模板"
        })
      ]);

    const props = createProps({
      onListDocumentTemplates,
      onGetDocumentTemplate
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "从模板新建" }));
    await screen.findByRole("heading", { name: "新建文档" });

    await user.selectOptions(screen.getByLabelText("模板（可选）"), "tpl-postmortem");

    expect(await screen.findByText("模板预览加载失败：network unavailable")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "重试" }));

    await waitFor(() => {
      expect(onGetDocumentTemplate).toHaveBeenCalledTimes(2);
    });
    expect(await screen.findByText("复盘模板")).toBeInTheDocument();
    expect(screen.getByText("# 事故时间线")).toBeInTheDocument();
  });

  it("shows office format selector only when office creation is enabled", async () => {
    const enabledProps = createProps({ officeCreationEnabled: true });
    const user = userEvent.setup();
    const { unmount } = render(<WorkspaceTree {...enabledProps} />);

    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "从模板新建" }));
    expect(await screen.findByLabelText("文档格式")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "取消" }));
    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    expect(screen.getByRole("menuitem", { name: "新建 Word" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "新建表格" })).toBeInTheDocument();
    unmount();

    const disabledProps = createProps({ officeCreationEnabled: false });
    render(<WorkspaceTree {...disabledProps} />);

    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "从模板新建" }));
    expect(screen.queryByLabelText("文档格式")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "取消" }));
    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    expect(screen.queryByRole("menuitem", { name: "新建 Word" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "新建表格" })).not.toBeInTheDocument();
  });

  it("forwards office format and disables markdown template selection when non-markdown is selected", async () => {
    const onCreateNode = vi
      .fn<
        (input: {
          parentId: string | null;
          type: NodeType;
          title: string;
          documentIdentifier?: string;
          templateId?: string;
          format?: "markdown" | "docx" | "xlsx";
        }) => Promise<CreateNodeResult>
      >()
      .mockResolvedValue({
        nodeId: "node-docx",
        docId: "doc-docx"
      });

    const props = createProps({
      officeCreationEnabled: true,
      onCreateNode
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "从模板新建" }));
    await screen.findByRole("heading", { name: "新建文档" });

    const formatSelect = screen.getByLabelText("文档格式");
    await user.selectOptions(formatSelect, "docx");

    expect(screen.queryByLabelText("模板（可选）")).not.toBeInTheDocument();
    expect(screen.getByText("Office 文档会使用仓库内置空白模板初始化，不支持 Markdown 模板。")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "创建" }));

    await waitFor(() => {
      expect(onCreateNode).toHaveBeenCalledTimes(1);
    });
    expect(onCreateNode).toHaveBeenCalledWith({
      parentId: null,
      type: "doc",
      title: "未命名文档",
      documentIdentifier: undefined,
      templateId: undefined,
      format: "docx"
    });
  });

  it("creates office documents from quick-create submenu", async () => {
    const onCreateNode = vi
      .fn<
        (input: {
          parentId: string | null;
          type: NodeType;
          title: string;
          documentIdentifier?: string;
          templateId?: string;
          format?: "markdown" | "docx" | "xlsx";
        }) => Promise<CreateNodeResult>
      >()
      .mockResolvedValue({
        nodeId: "node-xlsx",
        docId: "doc-xlsx"
      });

    const props = createProps({
      officeCreationEnabled: true,
      onCreateNode
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开目录快捷菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "新建表格" }));

    await screen.findByRole("heading", { name: "新建文档" });
    expect(screen.getByLabelText("文档格式")).toHaveValue("xlsx");

    await user.click(screen.getByRole("button", { name: "创建" }));
    await waitFor(() => {
      expect(onCreateNode).toHaveBeenCalledTimes(1);
    });
    expect(onCreateNode).toHaveBeenCalledWith({
      parentId: null,
      type: "doc",
      title: "未命名文档",
      documentIdentifier: undefined,
      templateId: undefined,
      format: "xlsx"
    });
  });

  it("opens the import dialog and forwards a file without preview support", async () => {
    const onImportDocuments = vi
      .fn<
        (input: {
          targetNodeId: string | null;
          files: File[];
          autoExtractTitle: boolean;
        }) => Promise<ImportWorkspaceDocumentsResult>
      >()
      .mockResolvedValue({
        totalCount: 1,
        successCount: 1,
        failedCount: 0,
        items: [
          {
            sourceName: "demo.bin",
            sourcePath: "demo.bin",
            detectedType: "unsupported",
            status: "success",
            stage: "done",
            createdNodeId: "node-imported"
          }
        ],
        failureItems: [],
        createdNodes: [
          {
            nodeId: "node-imported",
            parentId: "folder-1",
            title: "demo.bin",
            type: "doc",
            importedFileKind: "unsupported"
          }
        ]
      });
    const props = createProps({
      nodes: [
        {
          id: "folder-1",
          spaceId: "space-1",
          parentId: null,
          type: "folder",
          title: "目录",
          sort: 1,
          children: []
        }
      ],
      onImportDocuments
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开文档操作菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "导入" }));
    await screen.findByRole("heading", { name: "导入" });

    const fileInput = screen.getByLabelText("导入文件");
    const file = new File([new Uint8Array([0, 255])], "demo.bin");
    await user.upload(fileInput, file);
    await user.click(screen.getByRole("button", { name: "开始导入" }));

    await waitFor(() => {
      expect(onImportDocuments).toHaveBeenCalledTimes(1);
    });
    expect(onImportDocuments).toHaveBeenCalledWith({
      targetNodeId: "folder-1",
      files: [file],
      autoExtractTitle: false
    });
    expect(await screen.findByText("导入结果")).toBeInTheDocument();
  });

  it("forwards auto extract title option when checked", async () => {
    const onImportDocuments = vi
      .fn<
        (input: {
          targetNodeId: string | null;
          files: File[];
          autoExtractTitle: boolean;
        }) => Promise<ImportWorkspaceDocumentsResult>
      >()
      .mockResolvedValue({
        totalCount: 1,
        successCount: 1,
        failedCount: 0,
        items: [],
        failureItems: [],
        createdNodes: []
      });
    const props = createProps({
      nodes: [
        {
          id: "folder-1",
          spaceId: "space-1",
          parentId: null,
          type: "folder",
          title: "目录",
          sort: 1,
          children: []
        }
      ],
      onImportDocuments
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开文档操作菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "导入" }));
    await screen.findByRole("heading", { name: "导入" });

    const fileInput = screen.getByLabelText("导入文件");
    const file = new File(["# Title"], "demo.md", { type: "text/markdown" });
    await user.upload(fileInput, file);
    await user.click(screen.getByRole("checkbox", { name: /自动提取文件名/i }));
    await user.click(screen.getByRole("button", { name: "开始导入" }));

    await waitFor(() => {
      expect(onImportDocuments).toHaveBeenCalledTimes(1);
    });
    expect(onImportDocuments).toHaveBeenCalledWith({
      targetNodeId: "folder-1",
      files: [file],
      autoExtractTitle: true
    });
  });

  it("shows format indicators for markdown, word and excel documents in tree", async () => {
    const props = createProps({
      nodes: [
        {
          id: "node-md",
          documentId: "doc-md",
          documentFormat: "markdown",
          spaceId: "space-1",
          parentId: null,
          type: "doc",
          title: "Markdown 文档",
          sort: 1,
          visibility: "member",
          children: []
        },
        {
          id: "node-docx",
          documentId: "doc-docx",
          documentFormat: "docx",
          spaceId: "space-1",
          parentId: null,
          type: "doc",
          title: "Word 文档",
          sort: 2,
          visibility: "member",
          children: []
        },
        {
          id: "node-xlsx",
          documentId: "doc-xlsx",
          documentFormat: "xlsx",
          spaceId: "space-1",
          parentId: null,
          type: "doc",
          title: "Excel 文档",
          sort: 3,
          visibility: "member",
          children: []
        }
      ]
    });

    render(<WorkspaceTree {...props} />);

    expect(screen.getByLabelText("文档格式：Markdown")).toBeInTheDocument();
    expect(screen.getByLabelText("文档格式：Word")).toBeInTheDocument();
    expect(screen.getByLabelText("文档格式：Excel")).toBeInTheDocument();
    expect(screen.queryByLabelText(/^可见性/)).not.toBeInTheDocument();
  });

  it("imports a directory into the selected folder using each file's relative path", async () => {
    const props = createProps({
      nodes: [{
        id: "destination",
        parentId: null,
        spaceId: "space-1",
        title: "项目",
        type: "folder",
        sort: 1,
        children: []
      }]
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    await user.click(screen.getByRole("button", { name: "打开文档操作菜单" }));
    await user.click(screen.getByRole("menuitem", { name: "上传文件夹" }));
    expect(screen.getByRole("heading", { name: "上传文件夹" })).toBeInTheDocument();
    const files = [
      new File(["first"], "notes.txt"),
      new File(["second"], "notes.txt"),
      new File([new Uint8Array([0, 1])], "asset.bin")
    ];
    const paths = ["资料/notes.txt", "资料/章节/notes.txt", "资料/附件/asset.bin"];
    files.forEach((file, index) => {
      Object.defineProperty(file, "webkitRelativePath", { value: paths[index] });
    });
    fireEvent.change(screen.getByLabelText("本地文件夹"), { target: { files } });
    expect(screen.getByText("资料")).toBeInTheDocument();
    expect(screen.getByText("3 个文件")).toBeInTheDocument();
    await user.dblClick(screen.getByRole("button", { name: "开始导入" }));

    expect(props.onImportDocuments).toHaveBeenCalledExactlyOnceWith({
      targetNodeId: "destination",
      files,
      autoExtractTitle: false,
      folderManifest: { paths }
    });
    expect(screen.queryByRole("button", { name: "开始导入" })).not.toBeInTheDocument();
  });

  it("opens nested folders and keeps unsupported files in the tree without opening a document", async () => {
    const props = createProps({
      nodes: [{
        id: "folder",
        parentId: null,
        spaceId: "space-1",
        title: "资料",
        type: "folder",
        sort: 1,
        children: [{
          id: "file-unsupported",
          parentId: "folder",
          spaceId: "space-1",
          type: "doc",
          importedFileKind: "unsupported",
          documentId: "doc-unsupported",
          importedOriginalDownloadUrl: "/api/docs/doc-unsupported/imported-original",
          title: "asset.bin",
          sort: 1,
          children: []
        }, {
          id: "file-text",
          parentId: "folder",
          spaceId: "space-1",
          type: "doc",
          importedFileKind: "text",
          title: "notes.txt",
          documentId: "doc-text",
          sort: 2,
          children: []
        }]
      }]
    });
    const user = userEvent.setup();
    render(<WorkspaceTree {...props} />);

    expect(screen.getByLabelText("文件夹")).toBeInTheDocument();
    expect(screen.queryByText("asset.bin")).not.toBeInTheDocument();
    await user.click(screen.getByText("资料"));
    await user.click(screen.getByText("asset.bin"));
    expect(screen.getByLabelText("不支持的文件")).toBeInTheDocument();
    expect(props.onOpenUnsupportedFile).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
      documentId: "doc-unsupported",
      importedOriginalDownloadUrl: "/api/docs/doc-unsupported/imported-original"
    }));
    expect(props.onOpenDocument).not.toHaveBeenCalled();

    await user.click(screen.getByText("notes.txt"));
    expect(screen.getByLabelText("文本文件")).toBeInTheDocument();
    expect(props.onOpenDocument).toHaveBeenCalledExactlyOnceWith("doc-text");
    screen.getByText("资料").closest("button")?.focus();
    await user.keyboard("{Enter}");
    expect(screen.queryByText("asset.bin")).not.toBeInTheDocument();
  });
});
