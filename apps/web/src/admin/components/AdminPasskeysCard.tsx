import { KeyRound, LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { showToast } from "../../components/ui/toast";
import type { AuthGateway, PasskeySummary } from "../../data-access";
import { createPasskey, formatPasskeyError, supportsPasskeys } from "../../lib/passkey";
import { useAdminDialogs } from "./AdminDialogs";
import { AdminPageCard } from "./AdminPageLayout";

function formatPasskeyDate(value: string): string {
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

export function AdminPasskeysCard({ auth }: { auth: AuthGateway }) {
  const { confirm, dialogs } = useAdminDialogs();
  const [enabled, setEnabled] = useState(false);
  const [passkeys, setPasskeys] = useState<PasskeySummary[]>([]);
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);
  const [removingId, setRemovingId] = useState<string | null>(null);
  const [loadError, setLoadError] = useState("");
  const [actionError, setActionError] = useState("");
  const browserSupported = supportsPasskeys();
  const busy = adding || removingId !== null;

  const loadPasskeys = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    try {
      const result = await auth.listPasskeys();
      setEnabled(result.enabled);
      setPasskeys(result.passkeys);
    } catch (error) {
      setLoadError(`加载 Passkey 失败：${formatPasskeyError(error)}`);
    } finally {
      setLoading(false);
    }
  }, [auth]);

  useEffect(() => {
    void loadPasskeys();
  }, [loadPasskeys]);

  const handleAdd = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!enabled || !browserSupported || busy) {
      return;
    }
    setActionError("");
    setAdding(true);
    try {
      const { challengeToken, publicKey } = await auth.beginPasskeyRegistration();
      const credential = await createPasskey(publicKey);
      const passkey = await auth.verifyPasskeyRegistration({ challengeToken, credential, name: name.trim() || undefined });
      setPasskeys((previous) => [passkey, ...previous]);
      setName("");
      showToast("Passkey 已添加", "success");
    } catch (error) {
      setActionError(`添加 Passkey 失败：${formatPasskeyError(error)}`);
    } finally {
      setAdding(false);
    }
  };

  const handleRemove = async (passkey: PasskeySummary) => {
    setRemovingId(passkey.id);
    setActionError("");
    try {
      const confirmed = await confirm({
        title: `移除 ${passkey.name}`,
        description: "此 Passkey 将无法再用于登录 bugodoc。",
        confirmText: "确认移除",
        tone: "danger"
      });
      if (!confirmed) {
        return;
      }
      await auth.deletePasskey(passkey.id);
      setPasskeys((previous) => previous.filter((item) => item.id !== passkey.id));
      showToast("Passkey 已移除", "success");
    } catch (error) {
      setActionError(`移除 Passkey 失败：${formatPasskeyError(error)}`);
    } finally {
      setRemovingId(null);
    }
  };

  return (
    <AdminPageCard className="rounded-xl border border-slate-200 bg-white shadow-sm" contentClassName="space-y-4 p-5">
      <header className="space-y-1">
        <h3 className="text-lg font-semibold text-slate-900">通行密钥（Passkey）</h3>
        <p className="text-sm text-slate-500">使用设备的指纹、面容、PIN 或安全密钥登录。</p>
      </header>

      {loading && <p role="status" className="text-sm text-slate-500">正在加载 Passkey…</p>}
      {loadError && (
        <div className="flex flex-wrap items-center gap-3">
          <p role="alert" className="text-sm text-rose-700">{loadError}</p>
          <Button type="button" variant="outline" size="sm" onClick={() => void loadPasskeys()}>重试</Button>
        </div>
      )}
      {!loading && !loadError && (
        <>
          {passkeys.length > 0 ? (
            <ul className="divide-y divide-slate-200 rounded-md border border-slate-200">
              {passkeys.map((passkey) => (
                <li key={passkey.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2.5">
                  <div className="min-w-0 space-y-1">
                    <p className="break-words text-sm font-medium text-slate-900">{passkey.name}</p>
                    <p className="text-xs text-slate-500">
                      添加于 {formatPasskeyDate(passkey.createdAt)}
                      {passkey.lastUsedAt && ` · 上次使用 ${formatPasskeyDate(passkey.lastUsedAt)}`}
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    aria-label={`移除 Passkey ${passkey.name}`}
                    disabled={busy}
                    onClick={() => void handleRemove(passkey)}
                  >
                    移除 Passkey
                  </Button>
                </li>
              ))}
            </ul>
          ) : enabled ? (
            <p className="text-sm text-slate-500">尚未添加 Passkey。</p>
          ) : null}

          {!enabled ? (
            <p className="text-sm text-slate-500">此站点尚未启用 Passkey。</p>
          ) : !browserSupported ? (
            <p className="text-sm text-slate-500">请使用支持 Passkey 的浏览器，通过 HTTPS 访问后添加。</p>
          ) : (
            <form className="flex flex-wrap items-end gap-3" onSubmit={(event) => void handleAdd(event)}>
              <label className="grid w-full max-w-[360px] gap-1.5 text-sm text-slate-700">
                <span className="font-medium">Passkey 名称（可选）</span>
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  maxLength={64}
                  placeholder="例如：我的笔记本"
                  disabled={busy}
                />
              </label>
              <Button type="submit" disabled={busy}>
                {adding ? <LoaderCircle size={14} className="animate-spin" aria-hidden="true" /> : <KeyRound size={14} aria-hidden="true" />}
                添加 Passkey
              </Button>
            </form>
          )}
        </>
      )}
      {actionError && <p role="alert" className="text-sm text-rose-700">{actionError}</p>}
      {dialogs}
    </AdminPageCard>
  );
}
