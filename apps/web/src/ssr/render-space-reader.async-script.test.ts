import { waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { READER_ASYNC_ENHANCEMENT_SCRIPT } from "./render-space-reader.async-script";

describe("READER_ASYNC_ENHANCEMENT_SCRIPT", () => {
  afterEach(() => {
    document.head.innerHTML = "";
    document.body.innerHTML = "";
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("updates only the directory after creating a nested folder and retries a failed refresh", async () => {
    document.body.innerHTML = `
      <button type="button" data-reader-create-document="/editor/space-test?from=reader">添加到目录</button>
      <div class="reader-sidebar__tree-scroll">
        <details data-reader-hook="tree-details" data-reader-node-id="parent"><summary data-reader-hook="tree-summary">父目录</summary></details>
        <details data-reader-hook="tree-details" data-reader-node-id="open" open><summary>已展开</summary></details>
      </div>
      <main data-reader-hook="main"><article data-reader-hook="article-shell">原来的文档</article></main>
      <script id="plaindoc-reader-state" type="application/json">{"space":{"id":"space-test"}}</script>
    `;
    const fetchReader = vi.fn()
      .mockResolvedValueOnce({ ok: false })
      .mockResolvedValueOnce({
        ok: true,
        text: async () => `
          <div class="reader-sidebar__tree-scroll">
            <details data-reader-hook="tree-details" data-reader-node-id="parent">
              <summary data-reader-hook="tree-summary">父目录</summary>
              <details data-reader-hook="tree-details" data-reader-node-id="new-folder"><summary data-reader-hook="tree-summary">新目录</summary></details>
            </details>
            <details data-reader-hook="tree-details" data-reader-node-id="open"><summary>已展开</summary></details>
          </div>
          <main data-reader-hook="main"><article data-reader-hook="article-shell">服务端返回的另一个正文节点</article></main>
        `
      });
    vi.stubGlobal("fetch", fetchReader);
    const defineDialogMethod = (name: "showModal" | "close", method: (this: HTMLDialogElement) => void) => {
      const prototype = HTMLDialogElement.prototype;
      const previousMethod = Object.getOwnPropertyDescriptor(prototype, name);
      Object.defineProperty(prototype, name, { configurable: true, value: method });
      return () => {
        if (previousMethod) Object.defineProperty(prototype, name, previousMethod);
        else Reflect.deleteProperty(prototype, name);
      };
    };
    const restoreShowModal = defineDialogMethod("showModal", function () { this.setAttribute("open", ""); });
    const restoreClose = defineDialogMethod("close", function () { this.removeAttribute("open"); });
    try {
      window.eval(READER_ASYNC_ENHANCEMENT_SCRIPT);
      document.querySelector<HTMLButtonElement>("[data-reader-create-document]")?.click();
      const dialog = document.querySelector<HTMLDialogElement>(".reader-create-dialog")!;
      const frame = dialog.querySelector("iframe")!;
      expect(dialog).toHaveAccessibleName("添加到目录");
      const created = { type: "plaindoc:reader-folder-created", spaceId: "space-test", nodeId: "new-folder" };
      const send = (data: object, origin = window.location.origin) => {
        window.dispatchEvent(new MessageEvent("message", { source: frame.contentWindow, origin, data }));
      };
      send(created, "https://other-origin.test");
      send({ ...created, spaceId: "other-space" });
      expect(fetchReader).not.toHaveBeenCalled();
      send({ type: "plaindoc:reader-create-resize", height: 284 });
      expect(dialog.style.getPropertyValue("--reader-create-form-height")).toBe("284px");

      document.querySelector("[data-reader-node-id='open']")?.setAttribute("open", "");
      const originalURL = window.location.href;
      const article = document.querySelector("[data-reader-hook='article-shell']");
      send(created);
      await waitFor(() => expect(dialog).toHaveTextContent("文件夹已创建，暂时无法更新目录。"));
      dialog.querySelector<HTMLButtonElement>(".reader-article-action")?.click();

      await waitFor(() => expect(dialog).not.toBeInTheDocument());
      expect(fetchReader).toHaveBeenCalledTimes(2);
      expect(window.location.href).toBe(originalURL);
      expect(document.querySelector("[data-reader-hook='article-shell']")).toBe(article);
      expect(document.querySelector("[data-reader-node-id='parent']")).toHaveAttribute("open");
      expect(document.querySelector("[data-reader-node-id='open']")).toHaveAttribute("open");
      expect(document.querySelector("[data-reader-node-id='new-folder']")).toHaveTextContent("新目录");
    } finally {
      restoreShowModal();
      restoreClose();
    }
  });

  it("opens the mobile sidebar and focuses the search input", () => {
    vi.stubGlobal("innerWidth", 390);
    document.body.innerHTML = `
      <button type="button" data-reader-hook="mobile-sidebar-open" data-reader-focus-search="1" aria-expanded="false">搜索</button>
      <aside data-reader-hook="sidebar"><input type="search" data-reader-search-input="1"></aside>
      <main data-reader-hook="main"></main>
      <script id="plaindoc-reader-state" type="application/json">{}</script>
    `;

    window.eval(READER_ASYNC_ENHANCEMENT_SCRIPT);
    const trigger = document.querySelector<HTMLButtonElement>("[data-reader-focus-search]");
    trigger?.click();

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(document.activeElement).toBe(document.querySelector("[data-reader-search-input]"));
    document.body.classList.remove("reader-mobile-sidebar-open");
  });

  it("opens an imported Office file in the reader at its canonical link", async () => {
    const initialURL = window.location.href;
    const readerURL = new URL("/r/space-test/office-report", initialURL).href;
    document.body.innerHTML = `
      <button type="button" data-reader-create-document="/editor/space-test?from=reader">添加到目录</button>
      <div class="reader-sidebar__tree-scroll"></div>
      <main data-reader-hook="main"><article data-reader-hook="article-shell">旧文档</article></main>
      <script id="plaindoc-reader-state" type="application/json">{"space":{"id":"space-test"}}</script>
    `;
    const fetchReader = vi.fn().mockResolvedValue({
      ok: true,
      redirected: true,
      url: readerURL,
      text: async () => `
        <div class="reader-sidebar__tree-scroll"></div>
        <main data-reader-hook="main">
          <article data-reader-hook="article-shell">
            <button data-reader-inline-edit="/editor/space-test/office-id?from=reader">编辑文档</button>
            新的 Office 文档
          </article>
        </main>
      `
    });
    vi.stubGlobal("fetch", fetchReader);
    const dialog = HTMLDialogElement.prototype;
    const originalShowModal = Object.getOwnPropertyDescriptor(dialog, "showModal");
    const originalClose = Object.getOwnPropertyDescriptor(dialog, "close");
    Object.defineProperty(dialog, "showModal", { configurable: true, value(this: HTMLDialogElement) { this.open = true; } });
    Object.defineProperty(dialog, "close", { configurable: true, value(this: HTMLDialogElement) { this.open = false; } });
    try {
      window.eval(READER_ASYNC_ENHANCEMENT_SCRIPT);
      document.querySelector<HTMLButtonElement>("[data-reader-create-document]")?.click();
      const frame = document.querySelector<HTMLIFrameElement>(".reader-create-dialog iframe")!;
      const data = { type: "plaindoc:reader-document-imported", spaceId: "space-test", documentId: "office-id" };
      window.dispatchEvent(new MessageEvent("message", {
        source: frame.contentWindow, origin: window.location.origin, data: { ...data, spaceId: "other-space" }
      }));
      expect(fetchReader).not.toHaveBeenCalled();
      window.dispatchEvent(new MessageEvent("message", { source: frame.contentWindow, origin: window.location.origin, data }));

      await waitFor(() => expect(frame).not.toBeInTheDocument());
      expect(fetchReader).toHaveBeenCalledWith(new URL("/r/space-test/office-id", initialURL).href, expect.anything());
      expect(document.querySelector("[data-reader-hook='article-shell']")).toHaveTextContent("新的 Office 文档");
      expect(window.location.href).toBe(readerURL);
      expect(document.querySelector(".reader-inline-editor")).toBeNull();
    } finally {
      if (originalShowModal) Object.defineProperty(dialog, "showModal", originalShowModal);
      else Reflect.deleteProperty(dialog, "showModal");
      if (originalClose) Object.defineProperty(dialog, "close", originalClose);
      else Reflect.deleteProperty(dialog, "close");
      window.history.replaceState(null, "", initialURL);
    }
  });

  it("keeps code copy working for article content inserted after initialization", async () => {
    document.body.innerHTML = `
      <main data-reader-hook="main">
        <article class="reader-article-shell" data-reader-hook="article-shell"></article>
      </main>
      <script id="plaindoc-reader-state" type="application/json">{}</script>
    `;

    const clipboardWriteText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window.navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: clipboardWriteText
      }
    });

    window.eval(READER_ASYNC_ENHANCEMENT_SCRIPT);

    const currentArticleShell = document.querySelector("[data-reader-hook='article-shell']");
    const nextArticleShell = document.createElement("article");
    nextArticleShell.className = "reader-article-shell";
    nextArticleShell.setAttribute("data-reader-hook", "article-shell");
    nextArticleShell.innerHTML = `
      <div class="code-block-copy-shell">
        <button
          type="button"
          class="code-block-copy-button"
          data-code-copy-button="1"
          data-copy-state="idle"
          aria-label="复制代码"
        >
          <span class="code-block-copy-button__label code-block-copy-button__label--idle">复制</span>
          <span class="code-block-copy-button__label code-block-copy-button__label--success">复制成功</span>
        </button>
        <pre class="code-block-copy-shell__surface"><code data-code-copy-source="1">const answer = 42;</code></pre>
      </div>
    `;
    currentArticleShell?.replaceWith(nextArticleShell);

    const copyButton = document.querySelector("button[data-code-copy-button='1']");
    expect(copyButton).toBeInstanceOf(HTMLButtonElement);

    (copyButton as HTMLButtonElement).click();

    await waitFor(() => {
      expect(clipboardWriteText).toHaveBeenCalledWith("const answer = 42;");
    });
    expect(copyButton).toHaveAttribute("data-copy-state", "success");
    expect(copyButton).toHaveAttribute("aria-label", "复制成功");
  });

  it("initializes reader image viewer runtime for existing reader images", async () => {
    document.body.innerHTML = `
      <main data-reader-hook="main">
        <article class="reader-article-shell" data-reader-hook="article-shell">
          <article id="plaindoc-preview-body">
            <p><img src="https://example.com/demo.png" alt="示例图片" width="400" height="240" /></p>
          </article>
        </article>
      </main>
      <div data-reader-hook="image-viewer" aria-hidden="true" hidden>
        <button data-reader-hook="image-viewer-backdrop" aria-label="关闭图片浏览器"></button>
        <div data-reader-hook="image-viewer-stage">
          <div data-reader-hook="image-viewer-content"></div>
        </div>
        <button data-reader-hook="image-viewer-close" aria-label="关闭图片浏览器"></button>
        <button data-reader-hook="image-viewer-prev" aria-label="上一张"></button>
        <button data-reader-hook="image-viewer-next" aria-label="下一张"></button>
        <button data-reader-hook="image-viewer-zoom-out" aria-label="缩小"></button>
        <button data-reader-hook="image-viewer-zoom-in" aria-label="放大"></button>
        <button data-reader-hook="image-viewer-original" aria-label="原始尺寸"></button>
        <button data-reader-hook="image-viewer-rotate" aria-label="旋转"></button>
        <span data-reader-hook="image-viewer-index">0/0</span>
        <span data-reader-hook="image-viewer-scale">100%</span>
      </div>
      <script id="plaindoc-reader-state" type="application/json">{}</script>
    `;

    const ensureReaderImageViewer = vi.fn();
    (window as Window & { __plaindocReaderImageViewerRuntime__?: unknown }).__plaindocReaderImageViewerRuntime__ =
      {
        ensureReaderImageViewer,
        refreshReaderImageViewer: vi.fn()
      };

    window.eval(READER_ASYNC_ENHANCEMENT_SCRIPT);

    await waitFor(() => {
      expect(ensureReaderImageViewer).toHaveBeenCalledWith(document);
    });
  });
});
