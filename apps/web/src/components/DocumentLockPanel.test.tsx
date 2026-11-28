import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { DocumentLockSettings, DocumentUnlockPanel } from "./DocumentLockPanel";

const originalShowModal = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, "showModal");

beforeAll(() => {
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", {
    configurable: true,
    value(this: HTMLDialogElement) { this.open = true; }
  });
});

afterAll(() => {
  if (originalShowModal) {
    Object.defineProperty(HTMLDialogElement.prototype, "showModal", originalShowModal);
  } else {
    Reflect.deleteProperty(HTMLDialogElement.prototype, "showModal");
  }
});

describe("document locks", () => {
  it("prompts to unlock and allows retry after an invalid password", async () => {
    const onUnlock = vi.fn().mockRejectedValueOnce(new Error("文档密码不正确")).mockResolvedValueOnce(undefined);
    render(<DocumentUnlockPanel title="团队文档" onUnlock={onUnlock} onBack={vi.fn()} backLabel="返回阅读" />);

    expect(screen.getByRole("heading", { name: "文档已锁定" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "文档锁设置" })).not.toBeInTheDocument();
    const password = screen.getByLabelText("文档密码");
    const submit = screen.getByRole("button", { name: "解锁文档" });
    expect(submit).toBeDisabled();
    fireEvent.change(password, { target: { value: "invalid-password" } });
    fireEvent.click(submit);
    expect(await screen.findByRole("alert")).toHaveTextContent("文档密码不正确");
    expect(password).toHaveValue("");
    fireEvent.change(password, { target: { value: "valid-password" } });
    fireEvent.click(submit);
    await waitFor(() => expect(onUnlock).toHaveBeenLastCalledWith("valid-password"));
  });

  it("checks the password confirmation before enabling the lock", async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<DocumentLockSettings title="团队文档" locked={false} onClose={onClose} onUpdate={onUpdate} />);

    fireEvent.change(screen.getByLabelText("文档密码"), { target: { value: "文档密码" } });
    fireEvent.change(screen.getByLabelText("确认密码"), { target: { value: "不一样的密码" } });
    fireEvent.click(screen.getByRole("button", { name: "锁定文档" }));
    expect(screen.getByRole("alert")).toHaveTextContent("两次输入的密码不一致");
    expect(onUpdate).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("确认密码"), { target: { value: "文档密码" } });
    fireEvent.click(screen.getByRole("button", { name: "锁定文档" }));
    expect(onUpdate).toHaveBeenCalledWith("文档密码");
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  });

  it("requires confirmation before removing a password", async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<DocumentLockSettings locked onClose={onClose} onUpdate={onUpdate} />);

    fireEvent.click(screen.getByRole("button", { name: "移除密码" }));
    expect(onUpdate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认移除" }));
    expect(onUpdate).toHaveBeenCalledWith(null);
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  });
});
