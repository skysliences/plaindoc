import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminRole, DataGateway } from "../data-access";
import { AdminApp } from "./AdminApp";

vi.mock("./pages/AdminProfilePage", () => {
  return {
    AdminProfilePage: () => <div data-testid="admin-profile-page">个人信息页</div>
  };
});

vi.mock("./pages/AdminSpacesPage", () => {
  return {
    AdminSpacesPage: () => <div data-testid="admin-spaces-page">空间管理页</div>
  };
});

vi.mock("./pages/AdminUsersPage", () => ({ AdminUsersPage: () => <div /> }));
vi.mock("./pages/AdminDocumentsPage", () => ({ AdminDocumentsPage: () => <div /> }));
vi.mock("./pages/AdminDocumentSharesPage", () => ({ AdminDocumentSharesPage: () => <div /> }));
vi.mock("./pages/AdminDocumentAttachmentsPage", () => ({ AdminDocumentAttachmentsPage: () => <div /> }));
vi.mock("./pages/AdminDocumentImagesPage", () => ({ AdminDocumentImagesPage: () => <div /> }));
vi.mock("./pages/AdminDocumentTemplatesPage", () => ({ AdminDocumentTemplatesPage: () => <div /> }));
vi.mock("./pages/AdminThemesPage", () => ({ AdminThemesPage: () => <div /> }));
vi.mock("./pages/AdminSystemConfigsPage", () => ({ AdminSystemConfigsPage: () => <div /> }));
vi.mock("./pages/AdminAuditsPage", () => ({ AdminAuditsPage: () => <div /> }));
vi.mock("./pages/AdminSearchAnalyzersPage", () => ({ AdminSearchAnalyzersPage: () => <div /> }));

function renderDashboard(roles: AdminRole[], listDocuments = vi.fn().mockResolvedValue({ pagination: { total: 362 } })) {
  const user = { id: "dashboard-admin", email: "dashboard@example.com", name: "后台管理员" };
  const admin = {
    getMe: vi.fn().mockResolvedValue({
      userId: user.id,
      email: user.email,
      name: user.name,
      roles,
      capabilities: { canViewSpaceManagement: true, canManageSpace: true }
    }),
    listUsers: vi.fn().mockResolvedValue({ pagination: { total: 2301 } }),
    listSpaces: vi.fn().mockResolvedValue({ pagination: { total: 35 } }),
    listDocuments,
    listAudits: vi.fn().mockResolvedValue({ pagination: { total: 0 } })
  };
  const dataGateway = { admin } as unknown as DataGateway;

  render(
    <MemoryRouter initialEntries={["/admin"]}>
      <AdminApp
        authSession={{ user }}
        checking={false}
        submitting={false}
        errorMessage={null}
        authChallenge={null}
        dataGateway={dataGateway}
        onLogin={vi.fn()}
        onVerifyMfa={vi.fn()}
        onRefreshCaptcha={vi.fn()}
        onLogout={vi.fn()}
      />
    </MemoryRouter>
  );

  return admin;
}

describe("AdminApp", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("only shows profile menu for a logged-in user without space membership", async () => {
    const getMe = vi.fn().mockResolvedValue({
      userId: "user-1",
      email: "user@example.com",
      name: "用户A",
      avatarUrl: "",
      roles: [],
      capabilities: {
        canViewSpaceManagement: false,
        canManageSpace: false
      }
    });

    const dataGateway = {
      admin: {
        getMe
      }
    } as unknown as DataGateway;

    render(
      <MemoryRouter initialEntries={["/admin/profile"]}>
        <AdminApp
          authSession={{ user: { id: "user-1", email: "user@example.com", name: "用户A" } }}
          checking={false}
          submitting={false}
          errorMessage={null}
          authChallenge={null}
          dataGateway={dataGateway}
          onLogin={vi.fn()}
          onVerifyMfa={vi.fn()}
          onRefreshCaptcha={vi.fn()}
          onLogout={vi.fn()}
        />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId("admin-profile-page")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "个人信息" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "空间管理" })).toBeNull();
    expect(screen.queryByRole("button", { name: "概览" })).toBeNull();
    expect(screen.getByText("普通用户")).toBeInTheDocument();
  });

  it("shows space management menu for a logged-in space member", async () => {
    const getMe = vi.fn().mockResolvedValue({
      userId: "member-1",
      email: "member@example.com",
      name: "成员A",
      avatarUrl: "",
      roles: ["space_admin"],
      capabilities: {
        canViewSpaceManagement: true,
        canManageSpace: true
      }
    });

    const dataGateway = {
      admin: {
        getMe
      }
    } as unknown as DataGateway;

    render(
      <MemoryRouter initialEntries={["/admin/profile"]}>
        <AdminApp
          authSession={{ user: { id: "member-1", email: "member@example.com", name: "成员A" } }}
          checking={false}
          submitting={false}
          errorMessage={null}
          authChallenge={null}
          dataGateway={dataGateway}
          onLogin={vi.fn()}
          onVerifyMfa={vi.fn()}
          onRefreshCaptcha={vi.fn()}
          onLogout={vi.fn()}
        />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId("admin-profile-page")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "个人信息" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "空间管理" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "概览" })).toBeInTheDocument();
    expect(screen.getByText("空间管理员")).toBeInTheDocument();
  });

  it("shows platform admin badge for privileged admin users", async () => {
    const getMe = vi.fn().mockResolvedValue({
      userId: "platform-1",
      email: "platform@example.com",
      name: "平台A",
      avatarUrl: "",
      roles: ["platform_admin"],
      capabilities: {
        canViewSpaceManagement: true,
        canManageSpace: true
      }
    });

    const dataGateway = {
      admin: {
        getMe
      }
    } as unknown as DataGateway;

    render(
      <MemoryRouter initialEntries={["/admin/profile"]}>
        <AdminApp
          authSession={{ user: { id: "platform-1", email: "platform@example.com", name: "平台A" } }}
          checking={false}
          submitting={false}
          errorMessage={null}
          authChallenge={null}
          dataGateway={dataGateway}
          onLogin={vi.fn()}
          onVerifyMfa={vi.fn()}
          onRefreshCaptcha={vi.fn()}
          onLogout={vi.fn()}
        />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId("admin-profile-page")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "概览" })).toBeInTheDocument();
    expect(screen.getByText("平台管理员")).toBeInTheDocument();
  });

  it("shows the real pagination totals for all platform metrics", async () => {
    const admin = renderDashboard(["platform_admin"]);

    expect(await within(await screen.findByRole("region", { name: "平台用户" })).findByText("2,301")).toBeVisible();
    expect(await within(screen.getByRole("region", { name: "空间数量" })).findByText("35")).toBeVisible();
    expect(await within(screen.getByRole("region", { name: "文档总量" })).findByText("362")).toBeVisible();
    expect(await within(screen.getByRole("region", { name: "审计记录" })).findByText("0")).toBeVisible();

    for (const list of [admin.listUsers, admin.listSpaces, admin.listDocuments, admin.listAudits]) {
      expect(list).toHaveBeenCalledWith({ page: 1, pageSize: 1 });
    }
    expect(screen.queryByText("最近 7 天内容趋势")).not.toBeInTheDocument();
    expect(screen.queryByText("待处理事项")).not.toBeInTheDocument();
  });

  it("loads only scoped metrics for a space admin and retries a failed metric independently", async () => {
    const listDocuments = vi.fn()
      .mockRejectedValueOnce(new Error("Network unavailable"))
      .mockResolvedValueOnce({ pagination: { total: 4 } });
    const admin = renderDashboard(["space_admin"], listDocuments);

    const documents = within(await screen.findByRole("region", { name: "文档总量" }));
    expect(await documents.findByText("加载失败")).toBeVisible();
    expect(await within(screen.getByRole("region", { name: "空间数量" })).findByText("35")).toBeVisible();
    expect(screen.queryByRole("region", { name: "平台用户" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "审计记录" })).not.toBeInTheDocument();
    expect(admin.listUsers).not.toHaveBeenCalled();
    expect(admin.listAudits).not.toHaveBeenCalled();

    fireEvent.click(documents.getByRole("button", { name: "重试文档总量" }));
    expect(await documents.findByText("4")).toBeVisible();
    expect(admin.listDocuments).toHaveBeenCalledTimes(2);
    expect(admin.listSpaces).toHaveBeenCalledTimes(1);
  });
});
