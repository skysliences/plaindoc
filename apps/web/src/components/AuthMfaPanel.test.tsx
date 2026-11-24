import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AdminAuthPanel } from "./AdminAuthPanel";
import { AuthPanel } from "./AuthPanel";

describe("authentication with Google Authenticator", () => {
  it("shows the BUGO wordmark and toggles password visibility without submitting login", async () => {
    const onLogin = vi.fn().mockResolvedValue(null);
    render(<AuthPanel
      mode="login"
      switchPath="/register"
      forgotPasswordPath="/forgot-password"
      redirectTarget="/r/space/document"
      checking={false}
      submitting={false}
      errorMessage={null}
      loginMode="local_only"
      allowUserRegister
      allowPasswordReset
      providerOptions={[]}
      authChallenge={null}
      onLogin={onLogin}
      onRegister={vi.fn()}
      onVerifyMfa={vi.fn()}
      onRefreshCaptcha={vi.fn()}
    />);
    expect(screen.getByRole("img", { name: "BUGO" })).toHaveAttribute("src", "/assets/favicon.png?v=bugo");
    expect(screen.getByRole("link", { name: "BUGO 首页" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "忘记密码？" })).toHaveAttribute("href", "/forgot-password?redirect=%2Fr%2Fspace%2Fdocument");
    const password = screen.getByLabelText("密码", { exact: true });
    fireEvent.change(screen.getByLabelText("账号（邮箱或用户名）"), { target: { value: "reader@example.test" } });
    fireEvent.change(password, { target: { value: "example-password" } });
    fireEvent.click(screen.getByRole("button", { name: "显示密码" }));
    expect(password).toHaveAttribute("type", "text");
    expect(onLogin).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "隐藏密码" }));
    expect(password).toHaveAttribute("type", "password");
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    await waitFor(() => expect(onLogin).toHaveBeenCalledWith(expect.objectContaining({
      identifier: "reader@example.test", password: "example-password"
    })));
  });

  it("requires setup verification before completing registration", async () => {
    const onRegister = vi.fn().mockResolvedValue({
      mode: "setup",
      challengeToken: "registration-challenge",
      secret: "JBSWY3DPEHPK3PXP",
      qrCodeDataUrl: "data:image/png;base64,QUJD"
    });
    const onVerifyMfa = vi.fn().mockRejectedValueOnce(new Error("验证码无效")).mockResolvedValueOnce(undefined);
    render(<AuthPanel
      mode="register"
      switchPath="/login"
      forgotPasswordPath="/forgot-password"
      redirectTarget={null}
      checking={false}
      submitting={false}
      errorMessage={null}
      loginMode="local_only"
      allowUserRegister
      allowPasswordReset={false}
      providerOptions={[]}
      authChallenge={null}
      onLogin={vi.fn()}
      onRegister={onRegister}
      onVerifyMfa={onVerifyMfa}
      onRefreshCaptcha={vi.fn()}
    />);

    fireEvent.change(screen.getByLabelText("昵称"), { target: { value: "文档用户" } });
    fireEvent.change(screen.getByLabelText("邮箱"), { target: { value: "reader@example.test" } });
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "example-password" } });
    fireEvent.change(screen.getByLabelText("确认密码"), { target: { value: "example-password" } });
    fireEvent.click(screen.getByRole("button", { name: "注册" }));

    expect(await screen.findByRole("heading", { name: "绑定 Google Authenticator" })).toBeVisible();
    expect(screen.getByRole("img", { name: "用于绑定 Google Authenticator 的二维码" })).toBeVisible();
    expect(screen.getByText("JBSWY3DPEHPK3PXP")).toBeVisible();
    expect(screen.queryByLabelText("密码")).not.toBeInTheDocument();
    expect(onVerifyMfa).not.toHaveBeenCalled();

    const input = screen.getByRole("textbox", { name: "6 位动态验证码" });
    const submit = screen.getByRole("button", { name: "验证并注册" });
    expect(submit).toBeDisabled();
    fireEvent.change(input, { target: { value: "012 345" } });
    fireEvent.click(submit);
    expect(await screen.findByRole("alert")).toHaveTextContent("验证码无效");
    expect(onVerifyMfa).toHaveBeenLastCalledWith({ challengeToken: "registration-challenge", code: "012345" });
    expect(input).toHaveValue("");

    fireEvent.change(input, { target: { value: "654321" } });
    fireEvent.click(submit);
    await waitFor(() => expect(onVerifyMfa).toHaveBeenLastCalledWith({ challengeToken: "registration-challenge", code: "654321" }));
  });

  it("returns to admin credentials after a challenge expires", async () => {
    const onLogin = vi.fn().mockResolvedValue({ mode: "verify", challengeToken: "admin-challenge" });
    const onVerifyMfa = vi.fn().mockRejectedValue(Object.assign(new Error("验证请求已过期"), { code: 1018 }));
    render(<AdminAuthPanel
      checking={false}
      submitting={false}
      errorMessage={null}
      authChallenge={null}
      onLogin={onLogin}
      onVerifyMfa={onVerifyMfa}
      onRefreshCaptcha={vi.fn()}
    />);

    fireEvent.change(screen.getByLabelText("邮箱"), { target: { value: "admin@example.test" } });
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "example-password" } });
    fireEvent.click(screen.getByRole("button", { name: "登录管理后台" }));

    expect(await screen.findByRole("heading", { name: "输入动态验证码" })).toBeVisible();
    expect(screen.queryByText("手动设置密钥")).not.toBeInTheDocument();
    expect(onVerifyMfa).not.toHaveBeenCalled();
    const input = screen.getByRole("textbox", { name: "6 位动态验证码" });
    fireEvent.change(input, { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "验证并登录" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("验证请求已过期");
    expect(input).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "返回登录" }));
    expect(screen.queryByRole("textbox", { name: "6 位动态验证码" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("密码")).toHaveValue("");
  });
});
