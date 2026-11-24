import { ArrowLeft, LoaderCircle, ShieldCheck } from "lucide-react";
import { useRef, useState, type FormEvent } from "react";
import type { AuthMfaChallenge, AuthMfaVerifyInput } from "../data-access";
import { formatError } from "../editor/status-utils";
import { SiteAuthLayout } from "./SiteAuthLayout";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";

interface AuthMfaPanelProps {
  challenge: AuthMfaChallenge;
  registration?: boolean;
  publicEntry?: boolean;
  onVerify: (input: AuthMfaVerifyInput) => Promise<void>;
  onBack: () => void;
}

const AUTH_MFA_CHALLENGE_EXPIRED_CODE = 1018;

export function AuthMfaPanel({ challenge, registration = false, publicEntry = false, onVerify, onBack }: AuthMfaPanelProps) {
  const [code, setCode] = useState("");
  const [error, setError] = useState<{ message: string; expired: boolean } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const requestInFlight = useRef(false);
  const isSetup = challenge.mode === "setup";
  const expired = error?.expired === true;
  let instructions = "打开 Google Authenticator，输入此账号当前的 6 位验证码。";
  if (expired) {
    instructions = registration ? "请返回注册页重新设置。" : "请返回登录页重新验证账号。";
  } else if (isSetup) {
    instructions = challenge.qrCodeDataUrl
      ? "在 Google Authenticator 扫描二维码或手动添加密钥，然后输入应用生成的验证码。"
      : "在 Google Authenticator 手动添加时间型密钥，然后输入应用生成的验证码。";
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (requestInFlight.current || expired || !/^\d{6}$/.test(code)) {
      return;
    }
    requestInFlight.current = true;
    setSubmitting(true);
    setError(null);
    try {
      await onVerify({ challengeToken: challenge.challengeToken, code });
    } catch (cause) {
      setCode("");
      setError({
        message: formatError(cause),
        expired: cause instanceof Error && "code" in cause && cause.code === AUTH_MFA_CHALLENGE_EXPIRED_CODE
      });
    } finally {
      requestInFlight.current = false;
      setSubmitting(false);
    }
  }

  const title = isSetup ? "绑定 Google Authenticator" : "输入动态验证码";
  const content = (
    <>
      {challenge.mode === "setup" && !expired && (
        <div className="space-y-3 rounded-md border border-slate-200 bg-slate-50 p-3">
          {challenge.qrCodeDataUrl && (
            <img
              src={challenge.qrCodeDataUrl}
              alt="用于绑定 Google Authenticator 的二维码"
              width={160}
              height={160}
              className="mx-auto block max-w-full rounded bg-white"
            />
          )}
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-slate-600">手动设置密钥</p>
            <code className="block select-all break-all rounded border border-slate-200 bg-white px-2 py-1.5 text-sm text-slate-900">
              {challenge.secret}
            </code>
          </div>
          {challenge.otpauthUrl && (
            <a className="inline-block text-sm text-blue-700 underline underline-offset-2" href={challenge.otpauthUrl}>
              在验证器中打开
            </a>
          )}
        </div>
      )}
      <form className={publicEntry ? "site-auth-form" : "space-y-4"} onSubmit={(event) => void handleSubmit(event)}>
        <label className={publicEntry ? "site-auth-field" : "admin-auth-form__field"}>
          <span>6 位动态验证码</span>
          <Input
            value={code}
            onChange={(event) => setCode(event.target.value.replace(/\D/g, "").slice(0, 6))}
            inputMode="numeric"
            autoComplete="one-time-code"
            pattern="[0-9]{6}"
            required
            readOnly={submitting}
            disabled={expired}
            aria-invalid={Boolean(error) && !expired}
            autoFocus
          />
        </label>
        {error && <p className={publicEntry ? "site-auth-error" : "admin-auth-form__error"} role="alert">{error.message}</p>}
        <Button type="submit" className={publicEntry ? "site-auth-submit" : "w-full"} disabled={submitting || expired || code.length !== 6}>
          {submitting && <LoaderCircle size={14} className="admin-auth-form__submit-loader" aria-hidden="true" />}
          {registration ? "验证并注册" : "验证并登录"}
        </Button>
        <Button type="button" className="w-full" variant="ghost" onClick={onBack} disabled={submitting}>
          <ArrowLeft size={14} aria-hidden="true" />
          {registration ? "返回注册" : "返回登录"}
        </Button>
      </form>
    </>
  );

  if (publicEntry) {
    return (
      <SiteAuthLayout title={title} description={instructions}>
        <div className="site-auth-mfa">{content}</div>
      </SiteAuthLayout>
    );
  }

  return (
    <div className="admin-auth-page">
      <Card className="mx-auto w-full max-w-md border-slate-200 shadow-xl">
        <CardHeader className="space-y-4 pb-4">
          <Badge variant="outline" className="w-fit gap-1 border-cyan-200 bg-cyan-50 text-cyan-700">
            <ShieldCheck size={13} aria-hidden="true" />
            两步验证
          </Badge>
          <div className="space-y-2">
            <CardTitle className="text-2xl tracking-tight">
              {title}
            </CardTitle>
            <CardDescription>{instructions}</CardDescription>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {content}
        </CardContent>
      </Card>
    </div>
  );
}
