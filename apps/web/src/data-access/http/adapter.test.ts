import { afterEach, describe, expect, it, vi } from "vitest";
import type { AdminSpaceTransferEvent } from "../types";
import { createHttpAdapter } from "./adapter";

describe("createHttpAdapter two-step authentication", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    window.localStorage.removeItem("plaindoc.auth.access-token");
    window.localStorage.removeItem("plaindoc.auth.refresh-token");
  });

  it("only saves session tokens after the authenticator code is accepted", async () => {
    const user = { id: "user-id", email: "reader@example.test", name: "文档用户" };
    const initial = {
      code: 0,
      data: {
        mfa: { mode: "setup", challengeToken: "challenge-token", secret: "JBSWY3DPEHPK3PXP" },
        token: "ignored-unverified-token",
        refreshToken: "ignored-unverified-refresh"
      }
    };
    const verified = { code: 0, data: { user, token: "verified-token", refreshToken: "verified-refresh" } };
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify(initial), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(verified), { status: 200 }));
    const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });

    await expect(gateway.auth.register({ email: user.email, name: user.name, password: "example-password" }))
      .resolves.toMatchObject({ mode: "setup", challengeToken: "challenge-token", secret: "JBSWY3DPEHPK3PXP" });
    expect(window.localStorage.getItem("plaindoc.auth.access-token")).toBeNull();
    expect(window.localStorage.getItem("plaindoc.auth.refresh-token")).toBeNull();

    const verification = { challengeToken: "challenge-token", code: "012345" };
    await expect(gateway.auth.verifyMfa(verification)).resolves.toEqual({ user, token: "verified-token" });
    expect(fetchMock).toHaveBeenLastCalledWith(
      "https://api.example.test/api/auth/mfa/verify",
      expect.objectContaining({ body: JSON.stringify(verification), method: "POST" })
    );
    expect(window.localStorage.getItem("plaindoc.auth.access-token")).toBe("verified-token");
    expect(window.localStorage.getItem("plaindoc.auth.refresh-token")).toBe("verified-refresh");
  });
});

describe("createHttpAdapter administrator authenticator reset", () => {
  afterEach(() => vi.restoreAllMocks());

  it("gets a user-scoped operation token before resetting the authenticator", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 0, data: { token: "reset-token" } })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 0, data: {} })));
    const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });

    await gateway.admin.resetUserMfa({ userId: " target/user " });

    expect(fetchMock).toHaveBeenNthCalledWith(1, "https://api.example.test/api/admin/operation-tokens", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ operation: "user.reset_mfa", targetType: "user", targetId: "target/user" })
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, "https://api.example.test/api/admin/users/target%2Fuser/reset-mfa", expect.objectContaining({
      method: "POST"
    }));
    expect(new Headers(fetchMock.mock.calls[1]?.[1]?.headers).get("X-Admin-Operation-Token")).toBe("reset-token");
  });
});

describe("createHttpAdapter workspace file download", () => {
  afterEach(() => vi.restoreAllMocks());

  it("reads original bytes and the upload filename and rejects a denied request", async () => {
    const contents = new Uint8Array([0, 255, 2, 13]);
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(contents, { headers: {
        "Content-Type": "application/octet-stream",
        "Content-Disposition": "attachment; filename*=UTF-8''%E5%8E%9F%E6%96%87.bin"
      } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 4003, message: "没有下载权限" }), { status: 403 }));
    const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });

    const original = await gateway.workspace.downloadImportedOriginal("original-doc");
    expect(original.fileName).toBe("原文.bin");
    expect(new Uint8Array(await original.blob.arrayBuffer())).toEqual(contents);
    expect(fetchMock).toHaveBeenCalledWith("https://api.example.test/api/docs/original-doc/imported-original",
      expect.objectContaining({ credentials: "include" }));
    await expect(gateway.workspace.downloadImportedOriginal("original-doc")).rejects.toThrow("没有下载权限");
  });
});

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  readonly listeners = new Map<string, EventListener[]>();
  readonly url: string;
  readonly withCredentials: boolean;
  readyState = 0;

  constructor(url: string | URL, init?: EventSourceInit) {
    this.url = String(url);
    this.withCredentials = init?.withCredentials === true;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: EventListener): void {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  close(): void {
    this.readyState = 2;
  }

  open(): void {
    this.readyState = 1;
    this.onopen?.(new Event("open"));
  }

  fail(): void {
    this.readyState = 0;
    this.onerror?.(new Event("error"));
  }

  emit(type: string, event: AdminSpaceTransferEvent): void {
    const message = new MessageEvent(type, { data: JSON.stringify(event) });
    if (type === "message") {
      this.onmessage?.(message);
    }
    for (const listener of this.listeners.get(type) ?? []) {
      listener(message);
    }
  }
}

describe("createHttpAdapter transfer subscriptions", () => {
  const originalEventSource = globalThis.EventSource;

  afterEach(() => {
    vi.useRealTimers();
    FakeEventSource.instances = [];
    globalThis.EventSource = originalEventSource;
    vi.restoreAllMocks();
  });

  it("normalizes transfer download URLs to the API origin", () => {
    globalThis.EventSource = FakeEventSource as unknown as typeof EventSource;
    const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });
    const onEvent = vi.fn();

    gateway.admin.subscribeSpaceExport({
      streamUrl: "/api/admin/spaces/space-a/exports/job-a/events?token=stream",
      onEvent
    });

    expect(FakeEventSource.instances[0]?.url).toBe("https://api.example.test/api/admin/spaces/space-a/exports/job-a/events?token=stream");

    FakeEventSource.instances[0]?.emit("completed", {
      type: "completed",
      progress: 100,
      downloadUrl: "/api/admin/space-exports/job-a/download?token=download"
    });

    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
      downloadUrl: "https://api.example.test/api/admin/space-exports/job-a/download?token=download"
    }));
  });

  it("calls the global transfer task endpoints and normalizes returned URLs", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/admin/space-transfer-tasks?status=active")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: {
            tasks: [
              {
                jobId: "job-a",
                kind: "space_export",
                status: "running",
                progress: 45,
                updatedAt: "2026-05-17T00:00:00Z"
              }
            ]
          }
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.endsWith("/api/admin/space-transfer-tasks/space_export/job-a/stream-token")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: { streamUrl: "/api/admin/spaces/space-a/exports/job-a/events?token=stream" }
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.endsWith("/api/admin/space-transfer-tasks/space_export/job-a/download-token")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: { downloadUrl: "/api/admin/space-exports/job-a/download?token=download" }
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected request: ${url}`);
    });
    const originalFetch = globalThis.fetch;
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    try {
      const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });

      await expect(gateway.admin.listSpaceTransferTasks({ status: "active" })).resolves.toEqual({
        tasks: [
          expect.objectContaining({
            jobId: "job-a",
            kind: "space_export",
            status: "running"
          })
        ]
      });
      await expect(gateway.admin.issueSpaceTransferStreamToken({
        kind: "space_export",
        jobId: "job-a"
      })).resolves.toEqual({
        streamUrl: "https://api.example.test/api/admin/spaces/space-a/exports/job-a/events?token=stream"
      });
      await expect(gateway.admin.issueSpaceTransferDownloadToken({
        kind: "space_export",
        jobId: "job-a"
      })).resolves.toEqual({
        downloadUrl: "https://api.example.test/api/admin/space-exports/job-a/download?token=download"
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("lets EventSource recover transient transfer stream errors before refreshing tokens", () => {
    vi.useFakeTimers();
    globalThis.EventSource = FakeEventSource as unknown as typeof EventSource;
    const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });
    const onError = vi.fn();

    gateway.admin.subscribeSpaceExport({
      streamUrl: "/api/admin/spaces/space-a/exports/job-a/events?token=stream",
      onEvent: vi.fn(),
      onError
    });

    const source = FakeEventSource.instances[0];
    source?.fail();
    expect(onError).not.toHaveBeenCalled();

    vi.advanceTimersByTime(10_000);
    source?.open();
    vi.advanceTimersByTime(30_000);

    expect(onError).not.toHaveBeenCalled();
  });

  it("reports transfer stream errors after EventSource cannot reconnect", () => {
    vi.useFakeTimers();
    globalThis.EventSource = FakeEventSource as unknown as typeof EventSource;
    const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });
    const onError = vi.fn();

    gateway.admin.subscribeSpaceExport({
      streamUrl: "/api/admin/spaces/space-a/exports/job-a/events?token=stream",
      onEvent: vi.fn(),
      onError
    });

    FakeEventSource.instances[0]?.fail();
    vi.advanceTimersByTime(30_000);

    expect(onError).toHaveBeenCalledTimes(1);
  });
});

describe("createHttpAdapter document revision APIs", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("requests revision summaries and details separately", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/docs/doc-a/revisions")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: [
            {
              id: "revision-a",
              documentId: "doc-a",
              version: 2,
              baseVersion: 1,
              createdAt: "2026-05-17T00:00:00Z",
              source: "remote",
              format: "markdown",
              editorUser: { userId: "user-a", displayName: "作者甲" }
            }
          ]
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.endsWith("/api/docs/doc-a/revisions?page=2&pageSize=30")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: []
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.endsWith("/api/docs/doc-a/revisions/revision-a")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: {
            id: "revision-a",
            documentId: "doc-a",
            version: 2,
            baseVersion: 1,
            createdAt: "2026-05-17T00:00:00Z",
            source: "remote",
            format: "markdown",
            editorUser: { userId: "user-a", displayName: "作者甲" },
            contentMd: "# 历史正文"
          }
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.endsWith("/api/docs/doc-a/revisions/revision-a/restore")) {
        return new Response(JSON.stringify({
          code: 0,
          message: "success",
          data: {
            document: {
              id: "doc-a",
              nodeId: "node-a",
              themeId: "default",
              format: "markdown",
              title: "文档 A",
              contentMd: "# 历史正文",
              version: 3,
              updatedAt: "2026-05-17T00:01:00Z"
            },
            restoredFromRevision: {
              id: "revision-a",
              documentId: "doc-a",
              version: 2,
              baseVersion: 1,
              createdAt: "2026-05-17T00:00:00Z",
              source: "remote",
              format: "markdown"
            }
          }
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected request: ${url}`);
    });
    const originalFetch = globalThis.fetch;
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    try {
      const gateway = createHttpAdapter({ baseUrl: "https://api.example.test/api" });

      await expect(gateway.document.listRevisions("doc-a")).resolves.toEqual([
        expect.objectContaining({
          id: "revision-a",
          format: "markdown",
          editorUser: { userId: "user-a", displayName: "作者甲" }
        })
      ]);
      await expect(gateway.document.getRevisionDetail("doc-a", "revision-a")).resolves.toEqual(
        expect.objectContaining({
          id: "revision-a",
          contentMd: "# 历史正文"
        })
      );
      await expect(gateway.document.restoreRevision({
        docId: "doc-a",
        revisionId: "revision-a",
        baseVersion: 2
      })).resolves.toEqual(expect.objectContaining({
        document: expect.objectContaining({
          id: "doc-a",
          version: 3
        }),
        restoredFromRevision: expect.objectContaining({
          id: "revision-a"
        })
      }));
      expect(fetchMock).toHaveBeenCalledWith(
        "https://api.example.test/api/docs/doc-a/revisions",
        expect.anything()
      );
      await gateway.document.listRevisions("doc-a", { page: 2, pageSize: 30 });
      expect(fetchMock).toHaveBeenCalledWith(
        "https://api.example.test/api/docs/doc-a/revisions?page=2&pageSize=30",
        expect.anything()
      );
      expect(fetchMock).toHaveBeenCalledWith(
        "https://api.example.test/api/docs/doc-a/revisions/revision-a",
        expect.anything()
      );
      expect(fetchMock).toHaveBeenCalledWith(
        "https://api.example.test/api/docs/doc-a/revisions/revision-a/restore",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ baseVersion: 2 })
        })
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
