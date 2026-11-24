import { ChevronRight, FileQuestionMark, FileText, Folder, Plus, Settings2, UserRoundPlus, Users, X } from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import type {
  ParticipantSpace,
  TreeNode,
  WorkspaceDocumentAccess,
  WorkspaceDocumentPermissionUpdate,
  WorkspaceGateway,
  WorkspaceMember,
  WorkspaceMemberRole
} from "../data-access";
import { Button } from "../components/ui/button";
import { Checkbox } from "../components/ui/checkbox";
import { Input } from "../components/ui/input";
import { formatError } from "../editor/status-utils";

export const spaceManagementFrameName = "plaindoc-space-management";

export function isSpaceManagementFrame() {
  return window.parent !== window && window.name === spaceManagementFrameName;
}

export function getSpaceManagementParentOrigin() {
  if (isSpaceManagementFrame() && document.referrer) {
    return new URL(document.referrer).origin;
  }
  return undefined;
}

const dialogClass = "fixed m-auto w-[calc(100%_-_2rem)] rounded-2xl border border-solid border-slate-200 bg-white text-slate-900 shadow-xl backdrop:bg-slate-900/45";
const inputClass = "h-9 min-w-0 max-w-full rounded-md border border-solid border-slate-300 bg-white px-3 text-sm text-slate-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-500";
const settingsCard = "rounded-xl border border-solid border-slate-200 bg-white p-4 sm:p-5";
const settingsHeading = "m-0 flex items-center gap-2 text-sm font-semibold text-slate-900";
const settingsLabel = "grid min-w-0 flex-1 gap-1.5 text-sm font-medium text-slate-700";
const buttonClass = "rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50";
const linkClass = "text-sm font-medium text-blue-700 hover:underline";

function flattenDocuments(nodes: TreeNode[], parentPath = ""): { id: string; title: string }[] {
  return nodes.flatMap((node) => {
    const title = parentPath ? `${parentPath} / ${node.title}` : node.title;
    return [
      ...(node.documentId ? [{ id: node.documentId, title }] : []),
      ...flattenDocuments(node.children, title)
    ];
  });
}

function WorkspaceSettingsDirectoryRow({ node, editorPath }: { node: TreeNode; editorPath: string }) {
  const [expanded, setExpanded] = useState(false);
  const childrenId = useId();
  const Icon = node.type === "folder" ? Folder : FileText;
  return (
    <>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          className="h-auto min-h-9 min-w-0 flex-1 justify-start whitespace-normal px-2 py-2 text-left"
          aria-expanded={expanded}
          aria-controls={childrenId}
          onClick={() => setExpanded((open) => !open)}
        >
          <ChevronRight size={14} className={`shrink-0 text-slate-500 ${expanded ? "rotate-90" : ""}`} aria-hidden="true" />
          <Icon size={16} className="shrink-0 text-slate-600" aria-hidden="true" />
          <span className="min-w-0 break-words">{node.title}</span>
        </Button>
        {node.type === "doc" && node.documentId && node.importedFileKind !== "unsupported" && (
          <Button asChild variant="ghost" size="sm" className="shrink-0 text-xs text-slate-600 no-underline">
            <a href={`${editorPath}/${encodeURIComponent(node.documentId)}`} aria-label={`编辑 ${node.title}`}>
              编辑 <ChevronRight size={14} aria-hidden="true" />
            </a>
          </Button>
        )}
      </div>
      <div id={childrenId} hidden={!expanded}>
        {expanded && (node.children.length > 0 ? (
          <WorkspaceSettingsDocumentTree nodes={node.children} editorPath={editorPath} nested />
        ) : (
          <p className="m-0 px-8 py-2 text-xs text-slate-500">文件夹为空</p>
        ))}
      </div>
    </>
  );
}

function WorkspaceSettingsDocumentTree({ nodes, editorPath, nested = false }: {
  nodes: TreeNode[];
  editorPath: string;
  nested?: boolean;
}) {
  return (
    <ul className={`m-0 grid list-none gap-1 p-0 ${nested ? "ml-3 border-0 border-l border-solid border-slate-200 pl-2" : "mt-2"}`}>
      {nodes.map((node) => (
        <li key={node.id}>
          {node.type === "folder" || node.children.length > 0 ? (
            <WorkspaceSettingsDirectoryRow node={node} editorPath={editorPath} />
          ) : node.documentId && node.importedFileKind !== "unsupported" ? (
            <Button asChild variant="ghost" className="h-auto min-h-9 w-full justify-between gap-3 px-2 py-2 text-left no-underline">
              <a href={`${editorPath}/${encodeURIComponent(node.documentId)}`}>
                <span className="flex min-w-0 items-center gap-2">
                  <FileText size={16} className="shrink-0 text-slate-500" aria-hidden="true" />
                  <span className="min-w-0 whitespace-normal break-words">{node.title}</span>
                </span>
                <span className="flex shrink-0 items-center gap-1 text-xs text-slate-600">编辑 <ChevronRight size={14} aria-hidden="true" /></span>
              </a>
            </Button>
          ) : (
            <div className="flex min-h-9 items-center gap-2 px-2 py-2 text-sm text-slate-600">
              <FileQuestionMark size={16} className="shrink-0" aria-hidden="true" />
              <span className="min-w-0 flex-1 break-words">{node.title}</span>
              <span className="shrink-0 text-xs">无法编辑</span>
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

function WorkspaceSpaceSettings({ space, gateway, onRename }: {
  space: ParticipantSpace;
  gateway: WorkspaceGateway;
  onRename: (name: string) => void;
}) {
  const [name, setName] = useState(space.name);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<WorkspaceMemberRole>("reader");
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [documentId, setDocumentId] = useState("");
  const [members, setMembers] = useState<WorkspaceMember[] | null>(null);
  const [pending, setPending] = useState(false);
  const [memberError, setMemberError] = useState("");
  const [documentError, setDocumentError] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [memberRevision, setMemberRevision] = useState(0);
  const permissionDocuments = useMemo(() => flattenDocuments(tree), [tree]);

  useEffect(() => {
    let active = true;
    void gateway.getTree(space.id).then(
      (nodes) => { if (active) setTree(nodes); },
      (cause) => { if (active) setDocumentError(formatError(cause)); }
    );
    return () => { active = false; };
  }, [gateway, space.id]);

  useEffect(() => {
    let active = true;
    setMembers(null);
    setMemberError("");
    void gateway.listSpaceMembers(space.id, documentId || undefined).then(
      (items) => { if (active) setMembers(items); },
      (cause) => { if (active) setMemberError(formatError(cause)); }
    );
    return () => { active = false; };
  }, [documentId, gateway, memberRevision, space.id]);

  async function mutate(action: () => Promise<void>, message: string) {
    setPending(true);
    setError("");
    setNotice("");
    try {
      await action();
      setNotice(message);
    } catch (cause) {
      setError(formatError(cause));
    } finally {
      setPending(false);
    }
  }

  function rename(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    const nextName = name.trim();
    void mutate(async () => {
      await gateway.renameSpace(space.id, nextName);
      onRename(nextName);
      setName(nextName);
    }, "空间名称已保存");
  }

  function invite(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    void mutate(async () => {
      await gateway.inviteSpaceMember(space.id, email.trim(), role);
      setEmail("");
      setMemberRevision((value) => value + 1);
    }, "成员已加入空间");
  }

  function createDocument() {
    void mutate(async () => {
      const created = await gateway.createNode({
        spaceId: space.id, parentId: null, type: "doc", title: "未命名文档"
      });
      if (created.docId) {
        window.location.assign(`/editor/${encodeURIComponent(space.id)}/${encodeURIComponent(created.docId)}?compose=1`);
      }
    }, "文档已创建");
  }

  function updateDocumentMemberPermission(userId: string, changes: WorkspaceDocumentPermissionUpdate) {
    void mutate(async () => {
      await gateway.updateDocumentMemberPermission(documentId, userId, changes);
      setMembers((items) => items?.map((item) => item.userId === userId ? { ...item, ...changes } : item) ?? null);
    }, "文档权限已保存");
  }

  return (
    <section className="h-full overflow-y-auto bg-slate-50 p-4 text-slate-900 sm:p-5" aria-label={`管理空间 ${space.name}`}>
      {error && <p role="alert" className="mt-3 text-sm text-red-700">{error}</p>}
      {notice && <p role="status" className="mt-3 text-sm text-emerald-700">{notice}</p>}
      <fieldset disabled={pending} className="m-0 grid min-w-0 gap-3 border-0 p-0">
        <div className={settingsCard}>
          <h3 className={settingsHeading}><Settings2 size={16} aria-hidden="true" />基本信息</h3>
          <form onSubmit={rename} className="mt-3 flex flex-wrap items-end gap-2">
            <label className={settingsLabel}>
              空间名称
              <Input className={inputClass} required maxLength={64} value={name} onChange={(event) => setName(event.target.value)} />
            </label>
            <Button type="submit" disabled={pending || name.trim() === space.name}>保存名称</Button>
          </form>
        </div>

        <div className={settingsCard}>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className={settingsHeading}><FileText size={16} aria-hidden="true" />文档</h3>
            <Button variant="secondary" type="button" onClick={createDocument}><Plus size={16} aria-hidden="true" />新建文档</Button>
          </div>
          {tree.length > 0 && (
            <WorkspaceSettingsDocumentTree nodes={tree} editorPath={`/editor/${encodeURIComponent(space.id)}`} />
          )}
        </div>

        <div className={settingsCard}>
          <h3 className={settingsHeading}><Users size={16} aria-hidden="true" />成员与权限</h3>
          <form onSubmit={invite} className="mt-3 flex flex-wrap items-end gap-2">
            <label className={settingsLabel}>
              成员的注册邮箱
              <Input className={inputClass} type="email" required maxLength={254} value={email} onChange={(event) => setEmail(event.target.value)} />
            </label>
            <label className="grid gap-1.5 text-sm font-medium text-slate-700">
              空间角色
              <select className={inputClass} value={role} onChange={(event) => setRole(event.target.value as WorkspaceMemberRole)}>
                <option value="reader">阅读者</option>
                <option value="collaborator">协作者</option>
              </select>
            </label>
            <Button type="submit"><UserRoundPlus size={16} aria-hidden="true" />邀请成员</Button>
          </form>

          {documentError && <p role="alert" className="mt-3 text-sm text-red-700">{documentError}</p>}
          <label className="mt-4 grid w-fit max-w-full gap-1.5 text-sm font-medium text-slate-700">
            权限范围
            <select className={inputClass} value={documentId} onChange={(event) => {
              setMembers(null);
              setDocumentId(event.target.value);
              setNotice("");
              setError("");
            }}>
              <option value="">空间成员</option>
              {permissionDocuments.map((document) => <option key={document.id} value={document.id}>{document.title}</option>)}
            </select>
          </label>

          {memberError && <p role="alert" className="mt-3 text-sm text-red-700">{memberError}</p>}
          {!members && !memberError && <p className="mt-3 text-sm text-slate-500">正在加载成员…</p>}
          {members?.length === 0 && <p className="mt-3 text-sm text-slate-500">暂无其他成员</p>}
          {members && members.length > 0 && (
            <ul className="m-0 mt-2 list-none divide-y divide-slate-200 p-0">
              {members.map((member) => (
                <li key={member.userId} className="flex flex-wrap items-center justify-between gap-2 py-3">
                  <div className="min-w-0 break-words text-sm">
                    <p className="font-medium">{member.name || member.email}</p>
                    {member.name && <p className="text-slate-500">{member.email}</p>}
                  </div>
                  {documentId ? (
                    <div className="flex min-w-0 flex-wrap items-center gap-3">
                      <select
                        aria-label={`${member.email} 的文档权限`}
                        className={inputClass}
                        value={member.access ?? "inherit"}
                        onChange={(event) => {
                          const access = event.target.value as WorkspaceDocumentAccess;
                          updateDocumentMemberPermission(member.userId, { access });
                        }}
                      >
                        <option value="inherit">继承空间角色</option>
                        <option value="read">可读、可复制</option>
                        <option value="read_no_copy">可读、禁止复制</option>
                        <option value="edit">可编辑、可复制</option>
                        <option value="edit_no_copy">可编辑、禁止复制</option>
                        <option value="denied">禁止访问</option>
                      </select>
                      <label className="inline-flex cursor-pointer items-center gap-2 whitespace-nowrap text-sm">
                        <Checkbox
                          aria-label={`${member.email} 可导出文档`}
                          checked={!member.exportDenied}
                          disabled={pending}
                          onCheckedChange={(checked) => {
                            updateDocumentMemberPermission(member.userId, { exportDenied: checked !== true });
                          }}
                        />
                        可导出
                      </label>
                    </div>
                  ) : (
                    <div className="flex items-center gap-3">
                      {member.role === "owner" ? <span className="text-sm">所有者</span> : (
                        <select
                          aria-label={`${member.email} 的空间角色`}
                          className={inputClass}
                          value={member.role}
                          onChange={(event) => {
                            const nextRole = event.target.value as WorkspaceMemberRole;
                            void mutate(async () => {
                              await gateway.inviteSpaceMember(space.id, member.email, nextRole);
                              setMemberRevision((value) => value + 1);
                            }, "成员角色已保存");
                          }}
                        >
                          <option value="reader">阅读者</option>
                          <option value="collaborator">协作者</option>
                        </select>
                      )}
                      <button
                        type="button"
                        className="text-sm text-red-700 hover:underline"
                        onClick={() => {
                          if (!window.confirm(`将 ${member.email} 移出此空间？`)) return;
                          void mutate(async () => {
                            await gateway.removeSpaceMember(space.id, member.userId);
                            setMemberRevision((value) => value + 1);
                          }, "成员已移出空间");
                        }}
                      >移除</button>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </fieldset>
    </section>
  );
}

export function WorkspaceHome({ gateway }: { gateway: WorkspaceGateway }) {
  const [params, setParams] = useSearchParams();
  const spaceDialog = useRef<HTMLDialogElement>(null);
  const managementDialog = useRef<HTMLDialogElement>(null);
  const managementFrame = useRef<HTMLIFrameElement>(null);
  const [spaces, setSpaces] = useState<ParticipantSpace[] | null>(null);
  const [spaceName, setSpaceName] = useState("");
  const [creating, setCreating] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [createError, setCreateError] = useState("");
  const embedded = params.get("manage") === "dialog" && isSpaceManagementFrame();
  const selectedId = params.get("space");
  const selectedSpace = spaces?.find((space) => space.id === selectedId && space.canManage);

  useEffect(() => {
    let active = true;
    void gateway.listParticipantSpaces().then(
      (items) => { if (active) setSpaces(items); },
      (cause) => { if (active) setLoadError(formatError(cause)); }
    );
    return () => { active = false; };
  }, [gateway]);

  useEffect(() => {
    if (!params.has("create") || !spaceDialog.current) {
      return;
    }
    if (!spaceDialog.current.open) {
      spaceDialog.current.showModal();
    }
    const nextParams = new URLSearchParams(params);
    nextParams.delete("create");
    setParams(nextParams, { replace: true });
  }, [params, setParams]);

  useEffect(() => {
    const dialog = managementDialog.current;
    if (selectedSpace && dialog && !dialog.open) {
      dialog.showModal();
    }
  }, [selectedSpace]);

  function updateSpaceName(spaceId: string, name: string) {
    setSpaces((items) => items?.map((space) => space.id === spaceId ? { ...space, name } : space) ?? null);
  }

  useEffect(() => {
    if (embedded || !selectedId) return;
    const handleMessage = (event: MessageEvent) => {
      if (!managementDialog.current?.open || event.origin !== window.location.origin ||
          event.source !== managementFrame.current?.contentWindow) return;
      if (event.data?.type === "plaindoc:close-space-management") {
        managementDialog.current.close();
      }
      if (event.data?.type === "plaindoc:space-renamed" &&
          event.data.spaceId === selectedId && typeof event.data.name === "string") {
        updateSpaceName(selectedId, event.data.name);
      }
    };
    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, [embedded, selectedId]);

  function resetCreateSpaceDialog() {
    setSpaceName("");
    setCreateError("");
  }

  async function createSpace(event: FormEvent) {
    event.preventDefault();
    const name = spaceName.trim();
    if (!name || Array.from(name).length > 64) {
      setCreateError("请输入 1 至 64 个字符的空间名称。");
      return;
    }
    setCreating(true);
    setCreateError("");
    try {
      const space = await gateway.createSpace({ name });
      setSpaces((items) => [{ id: space.id, name: space.name, visibility: "member", canManage: true, canEdit: true }, ...(items ?? [])]);
      setParams({ space: space.id });
      spaceDialog.current?.close();
    } catch (cause) {
      setCreateError(formatError(cause));
    } finally {
      setCreating(false);
    }
  }

  if (embedded) {
    if (!selectedSpace) {
      return (
        <p className="p-5 text-sm text-slate-700" role={loadError || spaces ? "alert" : "status"}>
          {loadError || (spaces ? "此空间不可管理。" : "正在加载空间…")}
        </p>
      );
    }
    return (
      <WorkspaceSpaceSettings
        key={selectedSpace.id}
        space={selectedSpace}
        gateway={gateway}
        onRename={(name) => {
          updateSpaceName(selectedSpace.id, name);
          const parentOrigin = getSpaceManagementParentOrigin();
          if (parentOrigin) {
            window.parent.postMessage({ type: "plaindoc:space-renamed", spaceId: selectedSpace.id, name }, parentOrigin);
          }
        }}
      />
    );
  }

  return (
    <main className="h-full overflow-y-auto bg-slate-50 text-slate-900">
      <div className="mx-auto max-w-5xl px-4 py-8 sm:px-8">
        <a href="/" className={linkClass}>返回首页</a>
        <h1 className="mb-5 mt-3 text-2xl font-semibold">我的空间</h1>
        <div className="max-w-2xl">
          <section className="rounded-xl border border-slate-200 bg-white p-5">
            <button
              className={`${buttonClass} mb-4`}
              type="button"
              aria-haspopup="dialog"
              onClick={() => spaceDialog.current?.showModal()}
            >新建空间</button>
            {loadError && <p role="alert" className="text-sm text-red-700">{loadError}</p>}
            {!spaces && !loadError && <p className="text-sm text-slate-500">正在加载空间…</p>}
            {spaces?.length === 0 && <p className="text-sm text-slate-500">你还没有加入任何空间</p>}
            <ul className="divide-y divide-slate-200">
              {spaces?.map((space) => {
                const spaceId = encodeURIComponent(space.id);
                const landingPath = `/r/${spaceId}`;
                return (
                  <li key={space.id} className="py-2">
                    <a
                      href={landingPath}
                      className="flex min-h-11 items-center gap-2 rounded-lg px-2 text-slate-900 no-underline hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-blue-700"
                    >
                      <Folder size={18} className="shrink-0 text-blue-700" aria-hidden="true" />
                      <span className="min-w-0 flex-1 break-words font-medium">{space.name}</span>
                      <ChevronRight size={16} className="shrink-0 text-slate-500" aria-hidden="true" />
                    </a>
                    <div className="flex flex-wrap gap-x-3 gap-y-1 pb-1 pl-9">
                      {space.canManage && (
                        <button
                          type="button"
                          className={linkClass}
                          aria-haspopup="dialog"
                          onClick={() => setParams({ space: space.id }, { replace: true })}
                        >管理空间</button>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          </section>
        </div>
        <dialog
          ref={managementDialog}
          aria-labelledby="workspace-settings-title"
          className={`${dialogClass} max-w-[52rem] overflow-hidden p-0 outline-none`}
          onClose={() => setParams({}, { replace: true })}
        >
          <header className="flex items-center justify-between gap-3 border-0 border-b border-solid border-slate-200 px-4 py-2.5 sm:px-5">
            <h2 id="workspace-settings-title" className="m-0 text-base font-semibold">管理空间</h2>
            <Button type="button" variant="ghost" size="icon" aria-label="关闭" autoFocus onClick={() => managementDialog.current?.close()}>
              <X size={18} aria-hidden="true" />
            </Button>
          </header>
          {selectedSpace && (
            <iframe
              ref={managementFrame}
              name={spaceManagementFrameName}
              title="空间管理"
              className="block h-[min(72dvh,720px)] w-full border-0"
              src={`/editor?space=${encodeURIComponent(selectedSpace.id)}&manage=dialog`}
            />
          )}
        </dialog>
        <dialog
          ref={spaceDialog}
          aria-labelledby="workspace-create-space-title"
          className={`${dialogClass} max-w-[26rem] p-6`}
          onClose={resetCreateSpaceDialog}
          onCancel={(event) => { if (creating) event.preventDefault(); }}
        >
          <form onSubmit={createSpace} className="space-y-4">
            <h2 id="workspace-create-space-title" className="text-lg font-semibold">新建空间</h2>
            <label className="grid gap-1.5 text-sm">
              空间名称
              <input className={inputClass} autoFocus required value={spaceName} onChange={(event) => setSpaceName(event.target.value)} />
            </label>
            {createError && <p role="alert" className="text-sm text-red-700">{createError}</p>}
            <div className="flex justify-end gap-2">
              <button type="button" className={inputClass} disabled={creating} onClick={() => spaceDialog.current?.close()}>取消</button>
              <button type="submit" className={buttonClass} disabled={creating}>{creating ? "创建中…" : "创建空间"}</button>
            </div>
          </form>
        </dialog>

      </div>
    </main>
  );
}
