import { KeyRound, LockKeyhole, UnlockKeyhole } from "lucide-react";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { formatError } from "../editor/status-utils";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

interface DocumentUnlockPanelProps {
  title?: string;
  onUnlock: (password: string) => Promise<void>;
  onBack: () => void;
  backLabel: string;
  onManage?: () => void;
}

export function DocumentUnlockPanel({ title, onUnlock, onBack, backLabel, onManage }: DocumentUnlockPanelProps) {
  const titleId = useId();
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!password || pending) return;
    setPending(true);
    setError("");
    try {
      await onUnlock(password);
      setPassword("");
    } catch (cause) {
      setError(formatError(cause));
      setPassword("");
    } finally {
      setPending(false);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }

  return (
    <main className="grid min-h-[100dvh] place-items-center bg-slate-50 p-4" aria-labelledby={titleId}>
      <section className="w-full max-w-[420px] rounded-xl border border-slate-200 bg-white p-6 text-slate-900 shadow-sm">
        <span className="inline-flex size-10 items-center justify-center rounded-lg bg-slate-100 text-slate-600" aria-hidden="true">
          <LockKeyhole size={19} />
        </span>
        <h1 id={titleId} className="mb-0 mt-3 text-xl font-semibold">文档已锁定</h1>
        {title && <p className="mb-0 mt-2 break-words text-sm text-slate-600">{title}</p>}
        <form className="mt-5 grid gap-3" onSubmit={(event) => { void submit(event); }}>
          <label className="text-sm font-medium" htmlFor={inputId}>文档密码</label>
          <Input ref={inputRef} id={inputId} type="password" autoComplete="off" required autoFocus
            maxLength={72} value={password} disabled={pending}
            onChange={(event) => { setPassword(event.target.value); setError(""); }} />
          {error && <p role="alert" className="m-0 break-words text-sm text-red-700">{error}</p>}
          <Button type="submit" disabled={!password || pending}>
            <UnlockKeyhole size={16} aria-hidden="true" />
            {pending ? "验证中…" : "解锁文档"}
          </Button>
          <div className="flex flex-wrap justify-between gap-2">
            <Button type="button" variant="ghost" disabled={pending} onClick={onBack}>{backLabel}</Button>
            {onManage && (
              <Button type="button" variant="ghost" disabled={pending} onClick={onManage}>文档锁设置</Button>
            )}
          </div>
        </form>
      </section>
    </main>
  );
}

interface DocumentLockSettingsProps {
  title?: string;
  locked: boolean;
  onClose: () => void;
  onUpdate: (password: string | null) => Promise<void>;
}

export function DocumentLockSettings({ title, locked, onClose, onUpdate }: DocumentLockSettingsProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const passwordId = useId();
  const confirmationId = useId();
  const hintId = useId();
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [isConfirmingRemoval, setIsConfirmingRemoval] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) dialog.showModal();
  }, []);

  async function update(value: string | null) {
    if (pending) return;
    if (value !== null) {
      const passwordLength = new TextEncoder().encode(value).length;
      if (passwordLength < 6 || passwordLength > 72) {
        setError("密码长度需为 6–72 字节。");
        return;
      }
      if (value !== confirmation) {
        setError("两次输入的密码不一致。");
        return;
      }
    }
    setPending(true);
    setError("");
    try {
      await onUpdate(value);
      onClose();
    } catch (cause) {
      setError(formatError(cause));
      setPending(false);
    }
  }

  return (
    <dialog ref={dialogRef} onClose={onClose} aria-labelledby={titleId} onCancel={(event) => {
      if (pending) event.preventDefault();
    }}
      className="m-auto w-[calc(100%_-_2rem)] max-w-[440px] rounded-xl border border-solid border-slate-200 bg-white p-5 text-slate-900 shadow-xl backdrop:bg-slate-900/45">
      <h2 id={titleId} className="m-0 flex items-center gap-2 text-lg font-semibold">
        <KeyRound size={17} aria-hidden="true" />
        文档锁设置
      </h2>
      {title && <p className="mb-0 mt-2 break-words text-sm text-slate-600">{title}</p>}
      {isConfirmingRemoval ? (
        <section className="mt-4 grid gap-4">
          <p className="m-0 text-sm">移除文档密码后，有权限的用户可直接打开该文档。</p>
          {error && <p role="alert" className="m-0 text-sm text-red-700">{error}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={pending} onClick={() => { setIsConfirmingRemoval(false); setError(""); }}>返回</Button>
            <Button type="button" disabled={pending} onClick={() => { void update(null); }}>{pending ? "处理中…" : "确认移除"}</Button>
          </div>
        </section>
      ) : (
        <form className="mt-4 grid gap-3" onSubmit={(event) => { event.preventDefault(); void update(password); }}>
          <label className="text-sm font-medium" htmlFor={passwordId}>{locked ? "新密码" : "文档密码"}</label>
          <Input id={passwordId} type="password" autoComplete="new-password" autoFocus required maxLength={72}
            aria-describedby={hintId} value={password} disabled={pending}
            onChange={(event) => { setPassword(event.target.value); setError(""); }} />
          <p id={hintId} className="m-0 text-xs text-slate-600">6–72 字节。设置后，有权限的用户需输入此密码才能查看。</p>
          <label className="text-sm font-medium" htmlFor={confirmationId}>确认密码</label>
          <Input id={confirmationId} type="password" autoComplete="new-password" required maxLength={72}
            value={confirmation} disabled={pending}
            onChange={(event) => { setConfirmation(event.target.value); setError(""); }} />
          {error && <p role="alert" className="m-0 text-sm text-red-700">{error}</p>}
          <div className="mt-1 flex flex-wrap justify-end gap-2">
            {locked && (
              <Button type="button" variant="ghost" className="mr-auto text-red-700" disabled={pending}
                onClick={() => { setIsConfirmingRemoval(true); setError(""); }}>移除密码</Button>
            )}
            <Button type="button" variant="outline" disabled={pending} onClick={onClose}>取消</Button>
            <Button type="submit" disabled={!password || !confirmation || pending}>
              {pending ? "保存中…" : locked ? "修改密码" : "锁定文档"}
            </Button>
          </div>
        </form>
      )}
    </dialog>
  );
}
