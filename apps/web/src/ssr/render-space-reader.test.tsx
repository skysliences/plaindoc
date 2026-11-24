import { describe, expect, it } from "vitest";
import { renderSpaceReader } from "./render-space-reader";
import type { ReaderPagePayload } from "./ssr-types";

function createPayload(
  format: "markdown" | "docx" | "xlsx",
  options?: {
    shareEnabled?: boolean;
    officeRendering?: ReaderPagePayload["officeRendering"];
    officeHtml?: string;
    officeRenderStatus?: "idle" | "pending" | "success" | "failed";
    officeRenderError?: string;
  }
): ReaderPagePayload {
  const shareEnabled = options?.shareEnabled === true;
  return {
    space: {
      id: "space-reader-test",
      name: "测试空间",
      title: "测试文档"
    },
    document: {
      id: "doc-reader-test",
      nodeId: "node-reader-test",
      routeKey: "doc-reader-test",
      themeId: "default",
      format,
      visibility: "public",
      title: format === "xlsx" ? "预算表" : "项目说明",
      contentMd: format === "markdown" ? "# 标题\n\n正文" : options?.officeHtml ?? "",
      renderStatus: format === "markdown" ? undefined : options?.officeRenderStatus ?? "idle",
      renderError: format === "markdown" ? undefined : options?.officeRenderError ?? "",
      version: 1,
      sourceBlobId: format === "markdown" ? undefined : "blob-reader-test",
      sourceFileName: format === "xlsx" ? "预算表.xlsx" : format === "docx" ? "项目说明.docx" : undefined,
      sourceMimeType:
        format === "xlsx"
          ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
          : format === "docx"
            ? "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
            : undefined,
      contentVersion: 1,
      authorNickname: "测试作者",
      updatedAt: "2026-03-06T08:00:00Z"
    },
    share: shareEnabled
      ? {
          enabled: true,
          shareId: "share-reader-test",
          spaceId: "space-reader-test",
          documentRouteKey: "doc-reader-test",
          basePath: "/s/space-reader-test",
          attachmentBasePath: "/api/shares/space-reader-test/doc-reader-test/attachments"
        }
      : undefined,
    attachments: [],
    tree: [],
    activeDocId: "doc-reader-test",
    officeRendering: options?.officeRendering,
    viewer: {
      authenticated: false
    },
    requestOrigin: "https://docs.plaindoc.example"
  };
}

describe("renderSpaceReader", () => {
  it("offers file deletion to writable readers for leaf files, including legacy imports", () => {
    const payload = createPayload("markdown");
    const imported = { id: "legacy", title: "old.docx", type: "doc" as const, importedFileKind: "unsupported" as const, sort: 0, children: [] };
    payload.tree = [{ id: "folder", title: "目录", type: "folder", sort: 0, children: [imported, { ...imported, id: "restricted", deleteRestricted: true }] }];
    const buttons = () => new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html").querySelectorAll("button[data-reader-delete-file]");
    expect(buttons()).toHaveLength(0);
    // The server retains capabilities when it strips viewer identity for the render cache.
    payload.createDocumentUrl = "/editor/space-reader-test?create=document";
    expect(Array.from(buttons(), button => button.getAttribute("data-reader-delete-file"))).toEqual(["legacy"]);
  });
  it("shows folder delete actions only on writable folders, independently from their expand control", () => {
    const payload = createPayload("markdown");
    const folder = { id: "folder", type: "folder" as const, title: "文件夹", sort: 0, children: [] };
    payload.tree = [{ ...folder, id: "parent", children: [folder] }, { ...folder, id: "protected", deleteRestricted: true }];
    const folders = () => Array.from(new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html")
      .querySelectorAll<HTMLButtonElement>("button[data-reader-delete-folder]"));
    expect(folders()).toHaveLength(0);
    payload.createDocumentUrl = "/editor/space-reader-test?create=document";
    expect(folders().map(button => button.dataset.readerDeleteFolder)).toEqual(["parent", "folder"]);
    expect(folders().every(button => button.closest("summary") === null)).toBe(true);
  });

  it("renders folders without document actions when a space has no readable document", () => {
    const payload = createPayload("markdown");
    payload.document = {
      ...payload.document,
      id: "",
      nodeId: "",
      routeKey: "",
      title: "",
      contentMd: ""
    };
    payload.activeDocId = "";
    payload.tree = [{
      id: "folder",
      type: "folder",
      title: "内部文件夹",
      sort: 0,
      children: []
    }];

    const result = renderSpaceReader(payload);
    const reader = new DOMParser().parseFromString(result.html, "text/html");
    expect(result.head.canonical).toBe("/r/space-reader-test");
    expect(reader.querySelector(".reader-article-title")?.textContent).toBe("测试空间");
    expect(reader.querySelector(".reader-tree__label-text")?.textContent).toBe("内部文件夹");
    expect(reader.body.textContent).toContain("此空间暂无可阅读文档。");
    expect(reader.querySelector("[data-reader-export-action]")).toBeNull();
    expect(reader.querySelector('meta[name="robots"]')?.getAttribute("content")).toBe("noindex, nofollow");
  });

  it("renders office reader shell with noindex and without markdown export actions", () => {
    const result = renderSpaceReader(createPayload("docx"));

    expect(result.html).toContain('meta name="robots" content="noindex, nofollow"');
    expect(result.html).toContain('data-reader-office-editor="1"');
    expect(result.html).toContain('data-reader-office-download="1"');
    expect(result.html).not.toContain('data-reader-export-action="markdown"');
    expect(result.html).not.toContain('data-reader-export-action="pdf"');
  });

  it("keeps markdown export actions for markdown documents", () => {
    const result = renderSpaceReader(createPayload("markdown"));

    expect(result.html).not.toContain('meta name="robots" content="noindex, nofollow"');
    expect(result.html).toContain('data-reader-export-action="markdown"');
    expect(result.html).toContain('data-reader-export-action="pdf"');
    const reader = new DOMParser().parseFromString(result.html, "text/html");
    expect(reader.querySelector('.reader-sidebar__navigation > a.reader-home-link[href="/"]:first-child')).not.toBeNull();
    expect(reader.querySelector('.reader-mobile-bar > a.reader-home-link[href="/"]')).not.toBeNull();
    expect(reader.querySelector('.reader-article-shell a.reader-home-link')).toBeNull();
    expect(result.html).not.toContain('data-reader-office-editor="1"');
  });

  it.each([false, true])("preserves single Markdown line breaks with sharing enabled = %s", (shareEnabled) => {
    const payload = createPayload("markdown", { shareEnabled });
    payload.document.contentMd = "1\n2\n3\n4\n5\n6";

    const reader = new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html");
    const paragraph = reader.querySelector("#plaindoc-preview-body > p");
    expect(paragraph?.innerHTML.split(/<br>\s*/)).toEqual(["1", "2", "3", "4", "5", "6"]);
  });

  it.each(["markdown", "docx"] as const)("keeps %s readable while hiding export actions", (format) => {
    const payload = { ...createPayload(format), exportDenied: true };
    const { html } = renderSpaceReader(payload);
    const reader = new DOMParser().parseFromString(html, "text/html");
    expect(reader.body.hasAttribute("data-document-export-denied")).toBe(true);
    expect(reader.querySelector('[data-reader-hook="article-shell"]')).not.toBeNull();
    expect(reader.querySelector("[data-reader-export-action], [data-reader-office-download]")).toBeNull();
  });

  it("keeps navigation within the current document directory", () => {
    const { html } = renderSpaceReader(createPayload("markdown"));
    const reader = new DOMParser().parseFromString(html, "text/html");
    const sidebar = reader.querySelector('[data-reader-hook="sidebar"]');
    expect(sidebar?.querySelector('[aria-label="空间文件夹"]')).toBeNull();
    expect(sidebar?.querySelector('.reader-sidebar__documents')).not.toBeNull();
    expect(sidebar?.textContent).not.toContain("本文档使用");
    expect(sidebar?.querySelector('a[href="/"]')?.textContent).toContain("返回首页");
  });

  it("submits the reader search without limiting it to the current space", () => {
    const payload = { ...createPayload("markdown"), searchEnabled: true };
    const { html } = renderSpaceReader(payload);
    const reader = new DOMParser().parseFromString(html, "text/html");
    const form = reader.querySelector('[data-reader-hook="sidebar"] form[role="search"]');
    expect(form?.getAttribute("action")).toBe("/search");
    expect(form?.getAttribute("method")).toBe("get");
    expect(form?.hasAttribute("target")).toBe(false);
    expect(form?.querySelector('input[type="search"][name="q"][required]')?.getAttribute("aria-label")).toBe(
      "全文搜索关键词"
    );
    expect(form?.querySelectorAll("input")).toHaveLength(1);
    expect(reader.querySelector('[data-reader-hook="mobile-bar"] button[aria-label="打开全文搜索"]')).not.toBeNull();
  });

  it("hides reader search when disabled and on shared pages", () => {
    for (const payload of [
      { ...createPayload("markdown"), searchEnabled: false },
      { ...createPayload("markdown", { shareEnabled: true }), searchEnabled: true }
    ]) {
      const { html } = renderSpaceReader(payload);
      const reader = new DOMParser().parseFromString(html, "text/html");
      expect(reader.querySelector('form[action="/search"], button[data-reader-focus-search]')).toBeNull();
    }
  });

  it("shows folder and file icons and keeps unsupported files out of document navigation", () => {
    const payload = createPayload("markdown");
    payload.tree = [{
      id: "folder",
      type: "folder",
      title: "项目资料",
      sort: 0,
      children: [
        {
          id: payload.document.nodeId,
          documentId: payload.document.id,
          type: "doc",
          title: "说明.md",
          visibility: "member",
          sort: 0,
          children: []
        },
        {
          id: "unreadable-file",
          type: "doc",
          title: "备份.zip",
          importedFileKind: "unsupported",
          importedOriginalDownloadUrl: "/api/docs/unreadable-file/imported-original",
          sort: 1,
          children: []
        }
      ]
    }];

    const reader = new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html");
    const sidebar = reader.querySelector('[data-reader-hook="sidebar"]');
    expect(sidebar?.querySelector('details > summary svg.lucide-folder')).not.toBeNull();
    expect(sidebar?.querySelector('details > ul a[data-reader-doc-link="1"] svg.lucide-file-text')).not.toBeNull();
    expect(sidebar?.querySelector('.reader-visibility-marker')).toBeNull();

    const unsupportedFile = sidebar?.querySelector('details > ul button[data-reader-unsupported-file="备份.zip"]');
    expect(unsupportedFile?.textContent).toContain("备份.zip");
    expect(unsupportedFile?.querySelector('svg.lucide-file')).not.toBeNull();
    expect(unsupportedFile?.closest('[data-reader-doc-link]')).toBeNull();
    expect(unsupportedFile?.getAttribute("data-reader-original-download")).toBe("/api/docs/unreadable-file/imported-original");
    expect(reader.querySelector('dialog[data-reader-unsupported-file-dialog]')?.textContent).toContain("文件无法打开");
    expect(reader.querySelector('[data-reader-original-download-action]')?.hasAttribute("hidden")).toBe(true);
  });

  it("shows imported plain text without interpreting its heading or HTML", () => {
    const payload = createPayload("markdown");
    payload.document.importedFileKind = "text";
    payload.document.contentMd = "\n# 原文\n\n<b>保留标签</b>\n\t下一行";
    payload.editUrl = "/editor/space-reader-test/doc-reader-test?from=reader";

    const reader = new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html");
    const body = reader.querySelector("pre#plaindoc-preview-body");
    expect(body?.textContent).toBe(payload.document.contentMd);
    expect(body?.querySelector("b")).toBeNull();
    expect(reader.querySelector('[data-reader-hook="outline"]')).toBeNull();
    expect(reader.querySelector("[data-reader-inline-edit]")).not.toBeNull();
  });

  it("opens an imported image from its tree row and displays it in the preview body", () => {
    const payload = createPayload("markdown");
    const source = "/api/docs/doc-reader-test/imported-image";
    payload.document = {
      ...payload.document,
      title: "图表.png",
      importedFileKind: "image",
      importedImageUrl: source
    };
    payload.editUrl = "/editor/space-reader-test/doc-reader-test?from=reader";
    payload.tree = [{
      id: payload.document.nodeId,
      documentId: payload.document.id,
      title: payload.document.title,
      type: "doc",
      importedFileKind: "image",
      sort: 1,
      children: []
    }];

    const reader = new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html");
    expect(reader.querySelector('a[data-reader-doc-link="1"] svg.lucide-image')).not.toBeNull();
    const preview = reader.querySelector("#plaindoc-preview-body button[data-reader-imported-image-preview]");
    expect(preview?.getAttribute("aria-label")).toBe("放大图片 图表.png");
    expect(preview?.querySelector("img")?.getAttribute("src")).toBe(source);
    expect(preview?.querySelector("img")?.getAttribute("alt")).toBe("图表.png");
    expect(reader.querySelector("[data-reader-inline-edit], [data-reader-export-action]")).toBeNull();
    expect(reader.querySelector('[data-reader-hook="outline"]')).toBeNull();
  });

  it("renders the permission-scoped share image URL and handles an unavailable source", () => {
    const payload = createPayload("markdown", { shareEnabled: true });
    const source = "/api/shares/space-reader-test/doc-reader-test/imported-image";
    payload.document.importedFileKind = "image";
    payload.document.importedImageUrl = source;

    const read = () => new DOMParser().parseFromString(renderSpaceReader(payload).html, "text/html");
    expect(read().querySelector("#plaindoc-preview-body img")?.getAttribute("src")).toBe(source);
    expect(read().querySelector(".reader-imported-image__error")?.hasAttribute("hidden")).toBe(true);

    payload.document.importedImageUrl = undefined;
    const unavailable = read();
    expect(unavailable.querySelector("#plaindoc-preview-body img")).toBeNull();
    expect(unavailable.querySelector(".reader-imported-image__error")?.hasAttribute("hidden")).toBe(false);
  });

  it("renders authorized Markdown editing as a local action", () => {
    const editUrl = "/editor/space-reader-test/doc-reader-test?from=reader";
    const editEntry = (payload: ReaderPagePayload) => {
      const { html } = renderSpaceReader(payload);
      const reader = new DOMParser().parseFromString(html, "text/html");
      return reader.querySelector(".reader-article-actions [data-reader-inline-edit]");
    };

    const markdownEntry = editEntry({ ...createPayload("markdown"), editUrl });
    expect(markdownEntry?.tagName).toBe("BUTTON");
    expect(markdownEntry?.getAttribute("data-reader-inline-edit")).toBe(editUrl);
    expect(editEntry(createPayload("markdown"))).toBeNull();
    expect(editEntry({ ...createPayload("markdown", { shareEnabled: true }), editUrl })).toBeNull();
    expect(editEntry({ ...createPayload("docx"), editUrl })).toBeNull();
  });

  it("only renders the sidebar creation button when the reader payload permits it", () => {
    const createDocumentUrl = "/editor/space-reader-test?from=reader&create=document";
    const getCreateButton = (payload: ReaderPagePayload) => {
      const { html } = renderSpaceReader(payload);
      return new DOMParser().parseFromString(html, "text/html")
        .querySelector('.reader-sidebar button[aria-label="添加到目录"]');
    };

    expect(getCreateButton(createPayload("markdown"))).toBeNull();
    expect(getCreateButton({ ...createPayload("markdown"), createDocumentUrl })?.getAttribute("data-reader-create-document"))
      .toBe(createDocumentUrl);
    expect(getCreateButton({ ...createPayload("markdown", { shareEnabled: true }), createDocumentUrl }))
      .toBeNull();
  });

  it("renders share page with office read-only shell and no markdown export actions", () => {
    const result = renderSpaceReader(createPayload("xlsx", { shareEnabled: true }));

    expect(result.html).toContain('meta name="robots" content="noindex, nofollow"');
    expect(result.html).toContain('data-reader-office-editor="1"');
    expect(result.html).not.toContain('data-reader-export-action="markdown"');
    expect(result.html).not.toContain('<a href="/" class="reader-home-link">');
    expect(result.html).not.toContain('data-reader-export-action="pdf"');
  });

  it("renders local office html when independent rendering is enabled and succeeded", () => {
    const result = renderSpaceReader(
      createPayload("docx", {
        officeRendering: {
          independentRenderEnabled: true,
          fallbackToOnlyOfficeOnRenderFailure: true
        },
        officeRenderStatus: "success",
        officeHtml: "<h1>转换结果</h1><p>Office HTML</p>"
      })
    );

    expect(result.html).toContain("转换结果");
    expect(result.html).toContain("Office HTML");
    expect(result.html).not.toContain('data-reader-office-editor="1"');
  });

  it("renders office failure state when independent rendering fails without fallback", () => {
    const result = renderSpaceReader(
      createPayload("docx", {
        officeRendering: {
          independentRenderEnabled: true,
          fallbackToOnlyOfficeOnRenderFailure: false
        },
        officeRenderStatus: "failed",
        officeRenderError: "render failed"
      })
    );

    expect(result.html).toContain('data-reader-office-status="failed"');
    expect(result.html).toContain("render failed");
    expect(result.html).not.toContain('data-reader-office-editor="1"');
  });

  it("renders copy button for fenced code blocks", () => {
    const payload = createPayload("markdown");
    payload.document.contentMd = "```ts\nconst answer = 42;\n```";

    const result = renderSpaceReader(payload);

    expect(result.html).toContain('data-code-copy-button="1"');
    expect(result.html).toContain('data-code-copy-source="1"');
    expect(result.html).toContain("复制成功");
  });

  it("renders mermaid loading shell with source payload for async enhancement", () => {
    const payload = createPayload("markdown");
    payload.document.contentMd = "```mermaid\ngraph TD\nA-->B\n```";

    const result = renderSpaceReader(payload);

    expect(result.html).toContain('data-reader-hook="mermaid"');
    expect(result.html).toContain('data-reader-mermaid-source="1"');
    expect(result.html).toContain("Mermaid 图渲染中");
  });

  it("renders external markdown links with blank target and nofollow rel", () => {
    const payload = createPayload("markdown");
    payload.document.contentMd = [
      "[外链](https://example.com/docs)",
      "",
      "[内链](/guide/getting-started)"
    ].join("\n");

    const result = renderSpaceReader(payload);

    expect(result.html).toContain('<a href="https://example.com/docs" rel="noopener noreferrer nofollow" target="_blank">外链</a>');
    expect(result.html).toContain('<a href="/guide/getting-started">内链</a>');
  });

  it("renders reader image viewer shell hooks for async enhancement", () => {
    const result = renderSpaceReader(createPayload("markdown"));

    expect(result.html).toContain('data-reader-hook="image-viewer"');
    expect(result.html).toContain('data-reader-hook="image-viewer-stage"');
    expect(result.html).toContain('data-reader-hook="image-viewer-close"');
    expect(result.html).toContain('data-reader-hook="image-viewer-scale"');
  });
});
