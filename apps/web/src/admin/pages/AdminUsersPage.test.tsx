import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminUser, DataGateway } from "../../data-access";
import { AdminUsersPage } from "./AdminUsersPage";

const { showToast } = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock("../../components/ui/toast", () => ({ showToast }));

const listedUser: AdminUser = {
  userId: "target-user",
  email: "reader@example.com",
  name: "阅读者",
  role: "user",
  canEditRole: true,
  loginIpAddresses: ["192.0.2.1"],
  status: "active",
  bannedReason: "",
  bannedAt: null,
  deletedAt: null,
  createdAt: "",
  updatedAt: ""
};

describe("AdminUsersPage", () => {
  beforeEach(() => {
    showToast.mockReset();
  });

  it("retains failed IP edits, saves multiple addresses and can clear the list", async () => {
    const listUsers = vi.fn().mockResolvedValue({
      items: [listedUser],
      pagination: { page: 1, pageSize: 20, total: 1 }
    });
    const updateUserLoginIPs = vi.fn()
      .mockRejectedValueOnce(new Error("IP 地址格式无效"))
      .mockResolvedValueOnce({ loginIpAddresses: ["192.0.2.2", "2001:db8::2"] })
      .mockResolvedValueOnce({ loginIpAddresses: [] });
    const dataGateway = { admin: { listUsers, updateUserLoginIPs } } as unknown as DataGateway;

    render(<AdminUsersPage currentUserID="admin-user" dataGateway={dataGateway} onLogout={vi.fn()} />);
    fireEvent.click(await screen.findByRole("button", { name: "登录 IP" }));
    let dialog = within(screen.getByRole("dialog", { name: "登录 IP 白名单" }));
    expect(dialog.getByRole("textbox", { name: "允许的 IP 地址" })).toHaveValue("192.0.2.1");

    fireEvent.change(dialog.getByRole("textbox", { name: "允许的 IP 地址" }), { target: { value: "无效地址" } });
    fireEvent.click(dialog.getByRole("button", { name: "保存白名单" }));
    expect(await dialog.findByRole("alert")).toHaveTextContent("IP 地址格式无效");
    expect(dialog.getByRole("textbox", { name: "允许的 IP 地址" })).toHaveValue("无效地址");

    fireEvent.change(dialog.getByRole("textbox", { name: "允许的 IP 地址" }), {
      target: { value: "192.0.2.2\n2001:db8::2，192.0.2.2" }
    });
    fireEvent.click(dialog.getByRole("button", { name: "保存白名单" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(updateUserLoginIPs).toHaveBeenLastCalledWith({
      userId: listedUser.userId,
      ipAddresses: ["192.0.2.2", "2001:db8::2"]
    });

    fireEvent.click(screen.getByRole("button", { name: "登录 IP" }));
    dialog = within(screen.getByRole("dialog", { name: "登录 IP 白名单" }));
    expect(dialog.getByRole("textbox", { name: "允许的 IP 地址" })).toHaveValue("192.0.2.2\n2001:db8::2");
    fireEvent.change(dialog.getByRole("textbox", { name: "允许的 IP 地址" }), { target: { value: "" } });
    fireEvent.click(dialog.getByRole("button", { name: "保存白名单" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(updateUserLoginIPs).toHaveBeenLastCalledWith({ userId: listedUser.userId, ipAddresses: [] });
  });

  it("confirms an authenticator reset for the selected user and lets the administrator retry a failed reset", async () => {
    const listUsers = vi.fn().mockResolvedValue({
      items: [listedUser],
      pagination: { page: 1, pageSize: 20, total: 1 }
    });
    const resetUserMfa = vi.fn().mockRejectedValueOnce(new Error("服务器暂不可用")).mockResolvedValueOnce(undefined);
    const onLogout = vi.fn();
    const dataGateway = { admin: { listUsers, resetUserMfa } } as unknown as DataGateway;

    render(<AdminUsersPage currentUserID="admin-user" dataGateway={dataGateway} onLogout={onLogout} />);
    const resetButton = await screen.findByRole("button", { name: "重置二次验证" });
    fireEvent.click(resetButton);
    const dialog = within(screen.getByRole("dialog", { name: `重置二次验证（${listedUser.email}）` }));
    expect(dialog.getByText("该用户将退出所有设备，已绑定的 Passkey 将被移除。下次登录时需要重新绑定 Google Authenticator。")).toBeInTheDocument();
    fireEvent.click(dialog.getByRole("button", { name: "取消" }));
    expect(resetUserMfa).not.toHaveBeenCalled();

    fireEvent.click(resetButton);
    fireEvent.click(screen.getByRole("button", { name: "确认重置" }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith("执行操作失败：服务器暂不可用", "error"));

    fireEvent.click(resetButton);
    fireEvent.click(screen.getByRole("button", { name: "确认重置" }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith("二次验证已重置", "success"));
    expect(resetUserMfa).toHaveBeenCalledTimes(2);
    expect(resetUserMfa).toHaveBeenLastCalledWith({ userId: listedUser.userId });
    expect(onLogout).not.toHaveBeenCalled();
  });

  it("returns the current administrator to sign-in after resetting their authenticator", async () => {
    const listUsers = vi.fn().mockResolvedValue({
      items: [{ ...listedUser, role: "platform_admin" }],
      pagination: { page: 1, pageSize: 20, total: 1 }
    });
    const resetUserMfa = vi.fn().mockResolvedValue(undefined);
    const onLogout = vi.fn().mockResolvedValue(undefined);
    const dataGateway = { admin: { listUsers, resetUserMfa } } as unknown as DataGateway;

    render(<AdminUsersPage currentUserID={listedUser.userId} dataGateway={dataGateway} onLogout={onLogout} />);
    fireEvent.click(await screen.findByRole("button", { name: "重置二次验证" }));
    expect(screen.getByText("你将退出所有设备，已绑定的 Passkey 将被移除。下次登录时需要重新绑定 Google Authenticator。")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认重置" }));
    await waitFor(() => expect(onLogout).toHaveBeenCalledOnce());
    expect(resetUserMfa).toHaveBeenCalledWith({ userId: listedUser.userId });
    expect(listUsers).toHaveBeenCalledOnce();
  });
});
