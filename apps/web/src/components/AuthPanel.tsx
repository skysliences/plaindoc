import { Eye, EyeOff, LoaderCircle } from "lucide-react";
import type { FormEvent } from "react";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import type {
  AuthCaptchaChallenge,
  AuthCaptchaRefreshInput,
  AuthLoginInput,
  AuthLoginMode,
  AuthLoginProviderOption,
  AuthMfaChallenge,
  AuthMfaVerifyInput,
  AuthRegisterInput
} from "../data-access";
import { AuthMfaPanel } from "./AuthMfaPanel";
import { SiteAuthLayout } from "./SiteAuthLayout";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

type AuthMode = "login" | "register";
const CAPTCHA_REFRESH_DEBOUNCE_MS = 800;

interface AuthPanelProps {
  mode: AuthMode;
  switchPath: string;
  forgotPasswordPath: string;
  redirectTarget: string | null;
  checking: boolean;
  submitting: boolean;
  errorMessage: string | null;
  loginMode: AuthLoginMode;
  allowUserRegister: boolean;
  allowPasswordReset: boolean;
  providerOptions: AuthLoginProviderOption[];
  authChallenge: AuthCaptchaChallenge | null;
  onLogin: (input: AuthLoginInput) => Promise<AuthMfaChallenge | null>;
  onRegister: (input: AuthRegisterInput) => Promise<AuthMfaChallenge | null>;
  onVerifyMfa: (input: AuthMfaVerifyInput) => Promise<void>;
  onRefreshCaptcha: (input: AuthCaptchaRefreshInput) => Promise<void>;
}

// 登录注册入口：独立于编辑器主视图，避免未登录状态触发业务加载链路。
export function AuthPanel({
  mode,
  switchPath,
  forgotPasswordPath,
  redirectTarget,
  checking,
  submitting,
  errorMessage,
  loginMode,
  allowUserRegister,
  allowPasswordReset,
  providerOptions,
  authChallenge,
  onLogin,
  onRegister,
  onVerifyMfa,
  onRefreshCaptcha
}: AuthPanelProps) {
  const [name, setName] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordVisible, setPasswordVisible] = useState(false);
  const passwordId = useId();
  const [confirmPassword, setConfirmPassword] = useState("");
  const [provider, setProvider] = useState("__auto__");
  const [captchaAnswer, setCaptchaAnswer] = useState("");
  const [refreshingCaptcha, setRefreshingCaptcha] = useState(false);
  const [mfaChallenge, setMfaChallenge] = useState<AuthMfaChallenge | null>(null);
  const refreshDelayTimerRef = useRef<number | null>(null);
  const refreshCancelledRef = useRef(false);

  useEffect(() => {
    refreshCancelledRef.current = false;
    return () => {
      refreshCancelledRef.current = true;
      if (refreshDelayTimerRef.current !== null) {
        window.clearTimeout(refreshDelayTimerRef.current);
        refreshDelayTimerRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    setCaptchaAnswer("");
  }, [authChallenge?.captchaId]);

  const validationErrorMessage = useMemo(() => {
    if (mode !== "register") {
      return null;
    }
    if (!confirmPassword) {
      return null;
    }
    if (password !== confirmPassword) {
      return "两次输入的密码不一致";
    }
    return null;
  }, [confirmPassword, mode, password]);

  const canSubmit = useMemo(() => {
    if (checking || submitting || refreshingCaptcha) {
      return false;
    }
    const loginIdentifier = mode === "login" ? identifier : email;
    if (!loginIdentifier.trim() || !password.trim()) {
      return false;
    }
    if (mode === "register" && !name.trim()) {
      return false;
    }
    if (mode === "register") {
      if (!confirmPassword) {
        return false;
      }
      if (password !== confirmPassword) {
        return false;
      }
    }
    if (authChallenge && !captchaAnswer.trim()) {
      return false;
    }
    return true;
  }, [authChallenge, captchaAnswer, checking, confirmPassword, email, identifier, mode, name, password, refreshingCaptcha, submitting]);

  const captchaIdentifier = useMemo(() => {
    if (mode === "login") {
      return identifier.trim();
    }
    return email.trim();
  }, [email, identifier, mode]);

  const canRefreshCaptcha = useMemo(() => {
    if (!authChallenge) {
      return false;
    }
    if (checking || submitting || refreshingCaptcha) {
      return false;
    }
    return captchaIdentifier.length > 0;
  }, [authChallenge, captchaIdentifier, checking, refreshingCaptcha, submitting]);

  const submitText = useMemo(() => {
    if (checking) {
      return "检查会话中...";
    }
    if (submitting) {
      return mode === "login" ? "登录中..." : "注册中...";
    }
    return mode === "login" ? "登录" : "注册";
  }, [checking, mode, submitting]);

  const switchActionText = mode === "login" ? "注册新账号" : "去登录";
  const switchPromptText = mode === "login" ? "还没有账号？" : "已有账号？";
  const headingText = mode === "login" ? "登录" : "注册";
  const introText = mode === "login" ? "访问你的团队文档空间。" : "注册时需绑定 Google Authenticator。";
  const showProviderSelector =
    mode === "login" && loginMode !== "local_only" && providerOptions.length > 0;
  const canSwitchToRegister = mode === "register" || allowUserRegister;
  const switchHref = useMemo(() => {
    if (!redirectTarget) {
      return switchPath;
    }
    return `${switchPath}?redirect=${encodeURIComponent(redirectTarget)}`;
  }, [redirectTarget, switchPath]);
  const forgotPasswordHref = useMemo(() => {
    if (!redirectTarget) {
      return forgotPasswordPath;
    }
    return `${forgotPasswordPath}?redirect=${encodeURIComponent(redirectTarget)}`;
  }, [forgotPasswordPath, redirectTarget]);

  const handleSubmit = useCallback(async (event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    if (!canSubmit) {
      return;
    }
    if (mode === "login") {
      const selectedProvider = provider !== "__auto__" ? provider : undefined;
      const challenge = await onLogin({
        identifier: identifier.trim(),
        password,
        provider: selectedProvider,
        captchaId: authChallenge?.captchaId,
        captchaAnswer: captchaAnswer.trim()
      });
      setPassword("");
      setPasswordVisible(false);
      setMfaChallenge(challenge);
      return;
    }
    const challenge = await onRegister({
      name: name.trim(),
      email: email.trim(),
      password,
      captchaId: authChallenge?.captchaId,
      captchaAnswer: captchaAnswer.trim()
    });
    setPassword("");
    setConfirmPassword("");
    setMfaChallenge(challenge);
  }, [authChallenge?.captchaId, canSubmit, captchaAnswer, email, identifier, mode, name, onLogin, onRegister, password, provider]);

  const handleRefreshCaptcha = useCallback(async () => {
    if (!authChallenge || !canRefreshCaptcha) {
      return;
    }
    setRefreshingCaptcha(true);
    try {
      await new Promise<void>((resolve) => {
        refreshDelayTimerRef.current = window.setTimeout(() => {
          refreshDelayTimerRef.current = null;
          resolve();
        }, CAPTCHA_REFRESH_DEBOUNCE_MS);
      });
      if (refreshCancelledRef.current) {
        return;
      }
      await onRefreshCaptcha({
        scene: mode,
        identifier: captchaIdentifier,
        captchaId: authChallenge.captchaId
      });
      setCaptchaAnswer("");
    } finally {
      if (!refreshCancelledRef.current) {
        setRefreshingCaptcha(false);
      }
    }
  }, [authChallenge, canRefreshCaptcha, captchaIdentifier, mode, onRefreshCaptcha]);

  if (mfaChallenge) {
    return (
      <AuthMfaPanel
        challenge={mfaChallenge}
        publicEntry
        registration={mode === "register"}
        onVerify={onVerifyMfa}
        onBack={() => setMfaChallenge(null)}
      />
    );
  }

  return (
    <SiteAuthLayout title={headingText} description={introText}>
      <form className="site-auth-form" onSubmit={(event) => void handleSubmit(event)}>
        {mode === "register" ? (
          <label className="site-auth-field">
            <span>昵称</span>
            <Input
              type="text"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="输入昵称"
              autoComplete="name"
              disabled={checking || submitting}
            />
          </label>
        ) : null}

        {mode === "login" ? (
          <label className="site-auth-field">
            <span>账号（邮箱或用户名）</span>
            <Input
              type="text"
              value={identifier}
              onChange={(event) => setIdentifier(event.target.value)}
              placeholder="输入邮箱或用户名"
              autoComplete="username"
              autoCapitalize="none"
              spellCheck={false}
              disabled={checking || submitting}
            />
          </label>
        ) : (
          <label className="site-auth-field">
            <span>邮箱</span>
            <Input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              placeholder="name@example.com"
              autoComplete="email"
              disabled={checking || submitting}
            />
          </label>
        )}

        {showProviderSelector ? (
          <label className="site-auth-field">
            <span>认证源（可选）</span>
            <Select
              value={provider}
              onValueChange={(value) => setProvider(value)}
              disabled={checking || submitting}
            >
              <SelectTrigger aria-label="认证源（可选）">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__auto__">自动选择（推荐）</SelectItem>
                {loginMode === "mixed" ? <SelectItem value="local">本地账号（local）</SelectItem> : null}
                {providerOptions.map((providerOption) => (
                  <SelectItem key={providerOption.id} value={providerOption.id}>
                    {providerOption.name}（{providerOption.id}）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
        ) : null}

        <div className="site-auth-field">
          <div className="site-auth-field__heading">
            <label htmlFor={passwordId}>密码</label>
            {mode === "login" && allowPasswordReset && (
              <a href={forgotPasswordHref} className="site-auth-link">忘记密码？</a>
            )}
          </div>
          <div className="site-auth-password">
            <Input
              id={passwordId}
              type={mode === "login" && passwordVisible ? "text" : "password"}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder={mode === "login" ? "输入密码" : "至少 6 位"}
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              spellCheck={false}
              autoCapitalize="none"
              disabled={checking || submitting}
            />
            {mode === "login" && (
              <button
                type="button"
                aria-label={passwordVisible ? "隐藏密码" : "显示密码"}
                aria-controls={passwordId}
                disabled={checking || submitting}
                onClick={() => setPasswordVisible((visible) => !visible)}
              >
                {passwordVisible ? <EyeOff size={18} aria-hidden="true" /> : <Eye size={18} aria-hidden="true" />}
              </button>
            )}
          </div>
        </div>

        {mode === "register" ? (
          <label className="site-auth-field">
            <span>确认密码</span>
            <Input
              type="password"
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
              placeholder="再次输入密码"
              autoComplete="new-password"
              disabled={checking || submitting}
            />
          </label>
        ) : null}

        {authChallenge ? (
          <div className="space-y-2 rounded-md border border-slate-200 bg-slate-50 p-3">
            <p className="text-xs text-slate-600">
              已触发验证码校验（{authChallenge.level} 位数字）
            </p>
            <button
              type="button"
              onClick={() => void handleRefreshCaptcha()}
              disabled={!canRefreshCaptcha}
              className="inline-block max-w-full disabled:cursor-not-allowed disabled:opacity-70"
              title="看不清？点击刷新验证码"
            >
              <span className="flex min-h-14 items-center justify-center">
                <img
                  src={authChallenge.captchaImageDataUrl}
                  alt="验证码（点击刷新）"
                  className="block h-auto w-auto max-w-full rounded border border-slate-200"
                />
              </span>
            </button>
            <p className="text-[11px] text-slate-500">
              看不清图片可点击刷新{refreshingCaptcha ? "（刷新中...）" : ""}
            </p>
            <label className="site-auth-field">
              <span>验证码</span>
              <Input
                type="text"
                value={captchaAnswer}
                onChange={(event) => setCaptchaAnswer(event.target.value)}
                placeholder="输入图片中的验证码"
                inputMode="numeric"
                autoComplete="off"
                disabled={checking || submitting}
              />
            </label>
          </div>
        ) : null}

        {(validationErrorMessage || errorMessage) && (
          <p className="site-auth-error" role="alert">{validationErrorMessage ?? errorMessage}</p>
        )}

        <Button type="submit" className="site-auth-submit" disabled={!canSubmit}>
          {checking || submitting ? <LoaderCircle className="admin-auth-form__submit-loader" size={14} aria-hidden="true" /> : null}
          <span>{submitText}</span>
        </Button>

        {canSwitchToRegister ? (
          <p className="site-auth-switch">
            <span>{switchPromptText}</span>
            <a href={switchHref} className="site-auth-link">
              {switchActionText}
            </a>
          </p>
        ) : (
          <p className="site-auth-switch">当前站点已关闭注册入口。</p>
        )}
      </form>
    </SiteAuthLayout>
  );
}
