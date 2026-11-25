import {
  ChevronRight,
  FileText,
  FolderKanban,
  History,
  Image,
  LayoutDashboard,
  Link2,
  LoaderCircle,
  LogOut,
  Paperclip,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Table2,
  type LucideIcon,
  Palette,
  Settings2,
  ShieldAlert,
  User,
  Users
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  type AdminGateway,
  type AdminIdentity,
  type AuthCaptchaChallenge,
  type AuthCaptchaRefreshInput,
  type AuthLoginInput,
  type AuthMfaChallenge,
  type AuthMfaVerifyInput,
  type AuthSession,
  type DataGateway
} from "../data-access";
import { formatError } from "../editor/status-utils";
import { AdminAuthPanel } from "../components/AdminAuthPanel";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { ADMIN_BRAND_LOGO_SRC } from "./brand";
import { ADMIN_LOGIN_ROUTE_PATH, ADMIN_ROUTE_BASE_PATH } from "./routes";
import { AdminUsersPage } from "./pages/AdminUsersPage";
import { AdminSpacesPage } from "./pages/AdminSpacesPage";
import { AdminDocumentsPage } from "./pages/AdminDocumentsPage";
import { AdminDocumentAttachmentsPage } from "./pages/AdminDocumentAttachmentsPage";
import { AdminDocumentImagesPage } from "./pages/AdminDocumentImagesPage";
import { AdminDocumentTemplatesPage } from "./pages/AdminDocumentTemplatesPage";
import { AdminThemesPage } from "./pages/AdminThemesPage";
import { AdminSystemConfigsPage } from "./pages/AdminSystemConfigsPage";
import { AdminAuditsPage } from "./pages/AdminAuditsPage";
import { AdminProfilePage } from "./pages/AdminProfilePage";
import { AdminSearchAnalyzersPage } from "./pages/AdminSearchAnalyzersPage";
import { AdminDocumentSharesPage } from "./pages/AdminDocumentSharesPage";
import { AdminSpaceTransferTaskProvider } from "./space-transfer/AdminSpaceTransferTaskProvider";

type AdminMenuKey =
  | "dashboard"
  | "profile"
  | "users"
  | "spaces"
  | "documents"
  | "shares"
  | "attachments"
  | "images"
  | "document-templates"
  | "themes"
  | "system"
  | "office-config"
  | "search-analyzers"
  | "audits";

interface AdminMenuItem {
  key: AdminMenuKey;
  label: string;
  description: string;
  path: string;
  icon: LucideIcon;
}

interface AdminAppProps {
  authSession: AuthSession;
  checking: boolean;
  submitting: boolean;
  errorMessage: string | null;
  authChallenge: AuthCaptchaChallenge | null;
  dataGateway: DataGateway;
  onLogin: (input: AuthLoginInput) => Promise<AuthMfaChallenge | null>;
  passkeyEnabled: boolean;
  onPasskeyLogin: () => Promise<void>;
  onVerifyMfa: (input: AuthMfaVerifyInput) => Promise<void>;
  onRefreshCaptcha: (input: AuthCaptchaRefreshInput) => Promise<void>;
  onLogout: () => Promise<void>;
}

function buildAdminMenu(identity: AdminIdentity | null): AdminMenuItem[] {
  const roles = identity?.roles ?? [];
  const hasPlatformAdminRole = roles.includes("platform_admin");
  const hasSpaceAdminRole = roles.includes("space_admin");
  const hasAnyAdminRole = hasPlatformAdminRole || hasSpaceAdminRole;
  const canViewSpaceManagement = hasAnyAdminRole || identity?.capabilities?.canViewSpaceManagement === true;

  // 菜单结构按“个人信息 -> 分享中心 -> 成员态空间管理 -> 管理员全量后台”逐级展开。
  // 分享中心对所有登录用户开放，但普通用户只会停留在“我的分享”视图。
  const items: AdminMenuItem[] = [
    {
      key: "profile",
      label: "个人信息",
      description: "维护昵称、头像和登录方式",
      path: "/admin/profile",
      icon: User
    },
    {
      key: "shares",
      label: "分享中心",
      description: "查看我的分享记录",
      path: "/admin/document-shares",
      icon: Link2
    }
  ];

  if (hasAnyAdminRole) {
    items.unshift({
      key: "dashboard",
      label: "概览",
      description: "查看管理范围内的统计数据",
      path: ADMIN_ROUTE_BASE_PATH,
      icon: LayoutDashboard
    });

    if (hasPlatformAdminRole) {
      items.push({
        key: "users",
        label: "用户管理",
        description: "管理用户状态、封禁与删除",
        path: "/admin/users",
        icon: Users
      });
    }

    items.push(
      {
        key: "spaces",
        label: "空间管理",
        description: "查看空间状态并执行封禁/删除",
        path: "/admin/spaces",
        icon: FolderKanban
      },
      {
        key: "documents",
        label: "文档管理",
        description: "筛选文档并处理违规内容",
        path: "/admin/documents",
        icon: FileText
      },
      {
        key: "attachments",
        label: "附件管理",
        description: "检索与删除文档附件",
        path: "/admin/attachments",
        icon: Paperclip
      },
      {
        key: "images",
        label: "图片管理",
        description: "检索与删除文档图片资源",
        path: "/admin/images",
        icon: Image
      }
    );

    if (hasPlatformAdminRole) {
      items.push({
        key: "document-templates",
        label: "模板管理",
        description: "治理全站文档场景模板",
        path: "/admin/document-templates",
        icon: FileText
      });
    }

    if (hasPlatformAdminRole) {
      items.push({
        key: "themes",
        label: "主题管理",
        description: "维护全站主题模板与 CSS 变量",
        path: "/admin/themes",
        icon: Palette
      });
    }

    if (hasPlatformAdminRole) {
      items.push({
        key: "system",
        label: "系统配置",
        description: "维护系统级配置参数",
        path: "/admin/system-configs",
        icon: Settings2
      });
    }

    if (hasPlatformAdminRole) {
      items.push({
        key: "office-config",
        label: "Office配置",
        description: "维护 ONLYOFFICE 接入与阅读渲染",
        path: "/admin/office-configs",
        icon: Table2
      });
    }

    if (hasPlatformAdminRole) {
      items.push({
        key: "search-analyzers",
        label: "分词治理",
        description: "维护全文检索分词器与词典词条",
        path: "/admin/search-analyzers",
        icon: Search
      });
    }

    if (hasPlatformAdminRole) {
      items.push({
        key: "audits",
        label: "审计日志",
        description: "检索关键管理操作轨迹",
        path: "/admin/audits",
        icon: History
      });
    }

    return items;
  }

  if (canViewSpaceManagement) {
    items.push({
      key: "spaces",
      label: "空间管理",
      description: "查看你参与的空间并打开编辑文档",
      path: "/admin/spaces",
      icon: FolderKanban
    });
  }

  return items;
}

// 左下角用户徽章按当前登录用户角色动态展示，避免把不同身份都写死成同一个 Admin。
function resolveAdminIdentityBadgeLabel(identity: AdminIdentity | null): string {
  const roles = identity?.roles ?? [];
  if (roles.includes("platform_admin")) {
    return "平台管理员";
  }
  if (roles.includes("space_admin")) {
    return "空间管理员";
  }
  return "普通用户";
}

function resolveActiveMenu(menuItems: AdminMenuItem[], pathname: string): AdminMenuItem | null {
  const normalizedPathname = pathname.endsWith("/") && pathname !== "/" ? pathname.slice(0, -1) : pathname;

  // 优先精确匹配，避免 `/admin` 抢占 `/admin/users` 等子路由。
  const exactMatchedItem = menuItems.find((item) => normalizedPathname === item.path);
  if (exactMatchedItem) {
    return exactMatchedItem;
  }

  // 再按路径深度匹配前缀，保证 `/admin/users/:id` 命中 `users` 菜单。
  const prefixCandidates = menuItems
    .filter((item) => normalizedPathname.startsWith(`${item.path}/`))
    .sort((left, right) => right.path.length - left.path.length);
  return prefixCandidates[0] ?? null;
}

function renderPlaceholderContent(activeMenuKey: AdminMenuKey): { title: string; description: string; todo: string[] } {
  switch (activeMenuKey) {
    case "dashboard":
      return {
        title: "后台概览",
        description: "查看管理范围内的统计数据。",
        todo: []
      };
    case "users":
      return {
        title: "用户管理",
        description: "用户列表、搜索、封禁/解封、软删除。",
        todo: ["实现列表与筛选", "支持批量封禁", "写入审计日志"]
      };
    case "profile":
      return {
        title: "个人信息",
        description: "维护昵称、头像与密码。",
        todo: ["昵称修改", "头像上传", "密码更新"]
      };
    case "spaces":
      return {
        title: "空间管理",
        description: "空间状态治理与权限范围内操作。",
        todo: ["实现空间列表", "支持状态变更", "支持删除确认流程"]
      };
    case "documents":
      return {
        title: "文档管理",
        description: "文档检索、状态变更和内容治理。",
        todo: ["实现多条件搜索", "支持封禁/删除", "支持按空间过滤"]
      };
    case "shares":
      return {
        title: "分享中心",
        description: "管理分享配置并支持取消/延期/修改密码。",
        todo: ["支持 all/mine 视图", "支持模式切换", "支持取消分享"]
      };
    case "attachments":
      return {
        title: "附件管理",
        description: "附件检索、删除与审计追踪。",
        todo: ["支持多条件搜索", "支持逻辑删除/物理删除", "记录附件治理审计日志"]
      };
    case "images":
      return {
        title: "图片管理",
        description: "图片资源检索、删除与审计追踪。",
        todo: ["支持多条件搜索", "支持逻辑删除/物理删除", "记录图片治理审计日志"]
      };
    case "document-templates":
      return {
        title: "模板管理",
        description: "文档场景模板维护与启停管理。",
        todo: ["实现模板 CRUD", "支持场景筛选", "支持模板变更审计"]
      };
    case "themes":
      return {
        title: "主题管理",
        description: "主题模板维护与生效范围管理。",
        todo: ["实现主题 CRUD", "区分内置与自定义主题", "支持变更审计"]
      };
    case "system":
      return {
        title: "系统配置",
        description: "系统级参数配置与校验。",
        todo: ["配置项列表", "JSON schema 校验", "配置变更审计"]
      };
    case "office-config":
      return {
        title: "Office配置",
        description: "ONLYOFFICE 接入与 Office 阅读渲染治理。",
        todo: ["维护接入参数", "维护阅读渲染策略", "记录配置变更审计"]
      };
    case "search-analyzers":
      return {
        title: "分词治理",
        description: "全文检索分词器状态、词典与预览治理。",
        todo: ["查看分词器状态", "维护词典词条", "执行分词预览与重载"]
      };
    case "audits":
      return {
        title: "审计日志",
        description: "检索操作轨迹并支持筛选导出。",
        todo: ["按操作者/目标筛选", "按时间区间查询", "导出审计报表"]
      };
    default:
      return {
        title: "待开发",
        description: "该模块将在下一阶段实现。",
        todo: ["补齐模块接口", "完成页面联调", "补充测试用例"]
      };
  }
}

interface DashboardMetric {
  key: AdminMenuKey;
  label: string;
  detail: string;
  load: (admin: AdminGateway) => Promise<{ pagination: { total: number } }>;
}

type DashboardMetricState =
  | { status: "loading" }
  | { status: "loaded"; total: number }
  | { status: "error" };

interface AdminMenuGroup {
  label: string;
  keys: AdminMenuKey[];
}

const DASHBOARD_COUNT_PAGE = { page: 1, pageSize: 1 };
const DASHBOARD_COUNT_FORMAT = new Intl.NumberFormat("zh-CN");

const DASHBOARD_METRICS: readonly DashboardMetric[] = [
  {
    key: "users",
    label: "平台用户",
    detail: "正常与封禁用户",
    load: (admin) => admin.listUsers(DASHBOARD_COUNT_PAGE)
  },
  {
    key: "spaces",
    label: "空间数量",
    detail: "管理范围内的正常与封禁空间",
    load: (admin) => admin.listSpaces(DASHBOARD_COUNT_PAGE)
  },
  {
    key: "documents",
    label: "文档总量",
    detail: "管理范围内的正常与封禁文档",
    load: (admin) => admin.listDocuments(DASHBOARD_COUNT_PAGE)
  },
  {
    key: "audits",
    label: "审计记录",
    detail: "当前保留的审计日志",
    load: (admin) => admin.listAudits(DASHBOARD_COUNT_PAGE)
  }
];

const ADMIN_MENU_GROUPS: readonly AdminMenuGroup[] = [
  { label: "总览", keys: ["dashboard"] },
  { label: "账号", keys: ["profile"] },
  { label: "内容管理", keys: ["users", "spaces", "documents", "shares", "attachments", "images"] },
  { label: "系统治理", keys: ["system", "office-config", "document-templates", "themes", "search-analyzers", "audits"] }
];
const ADMIN_PAGE_BACKGROUND = "lab(98.26% 0 0)";
const ADMIN_TITLE_EXTRA_METADATA = "bugodoc - 一个适合中小团队文档在线管理系统";
const ADMIN_DEFAULT_PAGE_TITLE = "管理后台";
const ADMIN_BUILD_VERSION = (import.meta.env.VITE_BUILD_VERSION ?? "").trim() || "dev";

function composeAdminBrowserTitle(pageTitle: string): string {
  const normalizedTitle = pageTitle.trim() || ADMIN_DEFAULT_PAGE_TITLE;
  return `${normalizedTitle} - ${ADMIN_TITLE_EXTRA_METADATA}`;
}

function buildMenuGroups(menuItems: AdminMenuItem[]): Array<{ label: string; items: AdminMenuItem[] }> {
  return ADMIN_MENU_GROUPS
    .map((group) => ({
      label: group.label,
      items: group.keys
        .map((key) => menuItems.find((item) => item.key === key))
        .filter((item): item is AdminMenuItem => item !== undefined)
    }))
    .filter((group) => group.items.length > 0);
}

function DashboardMetricCard({ admin, metric }: { admin: AdminGateway; metric: DashboardMetric }) {
  const [state, setState] = useState<DashboardMetricState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    async function loadCount() {
      setState({ status: "loading" });
      try {
        const { pagination } = await metric.load(admin);
        if (!cancelled) {
          setState({ status: "loaded", total: pagination.total });
        }
      } catch {
        if (!cancelled) {
          setState({ status: "error" });
        }
      }
    }
    void loadCount();
    return () => {
      cancelled = true;
    };
  }, [admin, attempt, metric]);

  return (
    <Card role="region" aria-label={metric.label} className="border-slate-200 bg-white shadow-sm">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium tracking-normal text-slate-600">{metric.label}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 pt-0" aria-busy={state.status === "loading"}>
        {state.status === "loaded" ? (
          <p className="text-3xl font-semibold tracking-tight tabular-nums">{DASHBOARD_COUNT_FORMAT.format(state.total)}</p>
        ) : state.status === "loading" ? (
          <p className="flex items-center gap-2 text-sm text-slate-500" role="status">
            <LoaderCircle size={15} className="animate-spin" aria-hidden="true" />
            加载中
          </p>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm text-red-700" role="status">加载失败</p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-label={`重试${metric.label}`}
              onClick={() => setAttempt((previousAttempt) => previousAttempt + 1)}
            >
              重试
            </Button>
          </div>
        )}
        <p className="text-xs text-slate-500">{metric.detail}</p>
      </CardContent>
    </Card>
  );
}

function AdminDashboard({
  admin,
  menuItems,
  onNavigate
}: {
  admin: AdminGateway;
  menuItems: AdminMenuItem[];
  onNavigate: (path: string) => void;
}) {
  const metrics = DASHBOARD_METRICS.filter((metric) => menuItems.some((item) => item.key === metric.key));
  const quickEntries = menuItems.filter((item) => item.key !== "dashboard").slice(0, 6);

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 2xl:grid-cols-4">
        {metrics.map((metric) => (
          <DashboardMetricCard key={metric.key} admin={admin} metric={metric} />
        ))}
      </div>

      <Card className="border-slate-200 bg-white shadow-sm">
        <CardHeader>
          <CardTitle className="text-xl">快捷入口</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-2 pt-0 sm:grid-cols-2 lg:grid-cols-3">
          {quickEntries.map((entry) => {
            const EntryIcon = entry.icon;
            return (
              <Button
                key={entry.key}
                type="button"
                variant="ghost"
                className="h-auto w-full items-center justify-between rounded-sm border border-slate-200 bg-slate-50 px-3 py-2.5 text-left font-normal transition-colors hover:bg-white"
                onClick={() => onNavigate(entry.path)}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <EntryIcon size={15} className="text-slate-600" />
                  <span className="truncate text-sm text-slate-800">{entry.label}</span>
                </span>
                <ChevronRight size={15} className="text-slate-400" />
              </Button>
            );
          })}
        </CardContent>
      </Card>
    </div>
  );
}

export function AdminApp({
  authSession,
  checking,
  submitting,
  errorMessage,
  authChallenge,
  dataGateway,
  onLogin,
  passkeyEnabled,
  onPasskeyLogin,
  onVerifyMfa,
  onRefreshCaptcha,
  onLogout
}: AdminAppProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const activeUser = authSession.user;

  const [adminIdentity, setAdminIdentity] = useState<AdminIdentity | null>(null);
  const [isAdminIdentityLoading, setIsAdminIdentityLoading] = useState(false);
  const [adminIdentityError, setAdminIdentityError] = useState<string | null>(null);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
  const isResolvingAdminIdentity = isAdminIdentityLoading ||
    Boolean(activeUser && adminIdentity && adminIdentity.userId !== activeUser.id);

  useEffect(() => {
    if (checking || !activeUser) {
      setAdminIdentity(null);
      setAdminIdentityError(null);
      return;
    }

    let cancelled = false;
    const loadAdminIdentity = async () => {
      setIsAdminIdentityLoading(true);
      setAdminIdentityError(null);
      try {
        const identity = await dataGateway.admin.getMe();
        if (cancelled) {
          return;
        }
        setAdminIdentity(identity);
      } catch (error) {
        if (cancelled) {
          return;
        }
        setAdminIdentity(null);
        setAdminIdentityError(`管理员权限校验失败：${formatError(error)}`);
      } finally {
        if (!cancelled) {
          setIsAdminIdentityLoading(false);
        }
      }
    };

    void loadAdminIdentity();
    return () => {
      cancelled = true;
    };
  }, [activeUser, checking, dataGateway]);

  const adminMenuItems = useMemo(
    () => buildAdminMenu(adminIdentity),
    [adminIdentity]
  );
  const activeMenuItem = useMemo(
    () => resolveActiveMenu(adminMenuItems, location.pathname),
    [adminMenuItems, location.pathname]
  );
  const currentAdminPageTitle = useMemo(() => {
    if (!activeUser) {
      return "管理后台登录";
    }
    if (checking || isResolvingAdminIdentity) {
      return ADMIN_DEFAULT_PAGE_TITLE;
    }
    if (adminIdentityError || !adminIdentity) {
      return "无管理后台权限";
    }
    const activeMenuKey = activeMenuItem?.key ?? adminMenuItems[0]?.key ?? "dashboard";
    return renderPlaceholderContent(activeMenuKey).title;
  }, [
    activeMenuItem,
    activeUser,
    adminIdentity,
    adminIdentityError,
    adminMenuItems,
    checking,
    isResolvingAdminIdentity
  ]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }
    window.document.title = composeAdminBrowserTitle(currentAdminPageTitle);
  }, [currentAdminPageTitle]);

  useEffect(() => {
    if (!adminIdentity || !adminMenuItems.length || isResolvingAdminIdentity) {
      return;
    }
    if (location.pathname === ADMIN_LOGIN_ROUTE_PATH) {
      navigate(ADMIN_ROUTE_BASE_PATH, { replace: true });
      return;
    }
    if (!activeMenuItem) {
      navigate(adminMenuItems[0].path, { replace: true });
    }
  }, [activeMenuItem, adminIdentity, adminMenuItems, isResolvingAdminIdentity, location.pathname, navigate]);

  const handleNavigateMenu = useCallback(
    (path: string) => {
      if (location.pathname !== path) {
        navigate(path);
      }
    },
    [location.pathname, navigate]
  );
  const handleBackHome = useCallback(() => {
    // 返回首页需触发整页导航，交由后端 SSR 渲染首页，而不是停留在前端 SPA 路由。
    window.location.assign("/");
  }, []);
  const handleProfileUpdated = useCallback(
    (payload: { name: string; email: string; avatarUrl: string }) => {
      setAdminIdentity((previousState) => {
        if (!previousState) {
          return previousState;
        }
        return {
          ...previousState,
          name: payload.name,
          email: payload.email,
          avatarUrl: payload.avatarUrl
        };
      });
    },
    []
  );

  if (checking || isResolvingAdminIdentity) {
    return (
      <div className="flex min-h-dvh items-center justify-center p-6" style={{ backgroundColor: ADMIN_PAGE_BACKGROUND }}>
        <Card className="w-full max-w-md border-slate-200 shadow-sm">
          <CardContent className="flex min-h-[88px] items-center justify-center gap-2 p-6 text-slate-600">
            <LoaderCircle className="animate-spin" size={18} />
            <span className="text-sm font-medium">正在加载管理后台...</span>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!activeUser) {
    return (
      <AdminAuthPanel
        key={location.key}
        checking={checking}
        submitting={submitting}
        errorMessage={errorMessage}
        authChallenge={authChallenge}
        onLogin={onLogin}
        passkeyEnabled={passkeyEnabled}
        onPasskeyLogin={onPasskeyLogin}
        onVerifyMfa={onVerifyMfa}
        onRefreshCaptcha={onRefreshCaptcha}
      />
    );
  }

  if (adminIdentityError || !adminIdentity) {
    return (
      <div className="flex min-h-dvh items-center justify-center p-6" style={{ backgroundColor: ADMIN_PAGE_BACKGROUND }}>
        <Card className="w-full max-w-lg border-rose-200 shadow-sm">
          <CardHeader className="pb-2">
            <div className="inline-flex h-10 w-10 items-center justify-center rounded-md bg-rose-50 text-rose-600">
              <ShieldAlert size={18} />
            </div>
            <CardTitle className="mt-3 text-2xl">无管理后台权限</CardTitle>
            <CardDescription className="mt-2 text-sm text-slate-600">
              {adminIdentityError ?? "当前账号未配置管理员角色，请联系平台管理员授权。"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" onClick={handleBackHome}>
                返回首页
              </Button>
              <Button type="button" variant="destructive" onClick={() => void onLogout()}>
                切换账号
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  const activeContent = renderPlaceholderContent(activeMenuItem?.key ?? adminMenuItems[0].key);
  const currentMenuPath = activeMenuItem?.path ?? adminMenuItems[0].path;
  const groupedMenuItems = buildMenuGroups(adminMenuItems);
  const hasPrivilegedAdminRole = adminIdentity.roles.includes("platform_admin") || adminIdentity.roles.includes("space_admin");
  const spaceManagementMode: "admin" | "member" = hasPrivilegedAdminRole ? "admin" : "member";

  return (
    <div className="h-dvh overflow-hidden text-slate-900" style={{ backgroundColor: ADMIN_PAGE_BACKGROUND }}>
      <AdminSpaceTransferTaskProvider dataGateway={dataGateway}>
      <div className="flex h-full">
        <aside
          className={`hidden shrink-0 flex-col bg-transparent transition-[width,padding,opacity] duration-200 lg:flex ${
            isSidebarCollapsed ? "w-0 overflow-hidden p-0 opacity-0 pointer-events-none" : "w-62 p-2"
          }`}
        >
          <div className="flex items-center gap-2 px-2 py-1">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center overflow-hidden rounded-md border border-slate-200/80 bg-white shadow-sm">
              <img src={ADMIN_BRAND_LOGO_SRC} alt="bugodoc logo" className="h-full w-full object-cover" />
            </span>
            <div className="flex min-w-0 items-center gap-1.5">
              <p className="truncate text-sm font-semibold tracking-tight">bugodoc 管理后台</p>
              <Badge variant="outline" className="h-5 shrink-0 border-slate-200 bg-slate-50 px-1.5 text-[10px] font-medium text-slate-600">
                {ADMIN_BUILD_VERSION}
              </Badge>
            </div>
          </div>

          <div className="mt-6 flex-1 space-y-5 overflow-auto px-1">
            {groupedMenuItems.map((group) => (
              <section key={group.label}>
                <p className="px-2 text-[11px] font-semibold uppercase tracking-[0.1em] text-slate-500">
                  {group.label}
                </p>
                <nav className="mt-2 space-y-0.5" aria-label={`${group.label}菜单`}>
                  {group.items.map((item) => {
                    const ItemIcon = item.icon;
                    const isActive = item.key === (activeMenuItem?.key ?? adminMenuItems[0].key);
                    return (
                      <Button
                        key={item.key}
                        type="button"
                        variant="ghost"
                        className={
                          isActive
                            ? "admin-nav-item-button admin-nav-item-button--active h-auto w-full items-center justify-start gap-3 rounded-sm px-3 py-1.5 text-left text-sm font-medium text-slate-50"
                            : "admin-nav-item-button h-auto w-full items-center justify-start gap-3 rounded-sm px-3 py-1.5 text-left text-sm text-slate-700"
                        }
                        onClick={() => handleNavigateMenu(item.path)}
                      >
                        <ItemIcon size={15} />
                        <span className="truncate">{item.label}</span>
                      </Button>
                    );
                  })}
                </nav>
              </section>
            ))}
          </div>

          <div className="mt-4 border-t border-slate-200/80 px-2 pt-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 space-y-1">
                <p className="truncate text-sm font-semibold text-slate-900">
                  {adminIdentity.name || activeUser.name || "管理员"}
                </p>
                <p className="truncate text-xs text-slate-500">{adminIdentity.email || activeUser.email}</p>
              </div>
              <Badge variant="outline" className="border-slate-200 bg-slate-50 text-[10px] text-slate-600">
                {resolveAdminIdentityBadgeLabel(adminIdentity)}
              </Badge>
            </div>
            <div className="mt-3 flex items-center gap-2">
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="admin-sidebar-footer-button flex-1 justify-start text-slate-700 hover:bg-slate-50"
                onClick={handleBackHome}
              >
                返回首页
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="admin-sidebar-footer-button admin-sidebar-footer-button--icon text-slate-700 hover:bg-slate-50"
                onClick={() => void onLogout()}
              >
                <LogOut size={14} />
                <span className="sr-only">退出</span>
              </Button>
            </div>
          </div>
        </aside>

        <section className="min-w-0 flex-1 p-2 lg:p-2">
          <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-2xl border border-slate-200/80 bg-white shadow-sm">
            <header className="flex h-14 items-center px-4">
              <div className="flex min-w-0 items-center gap-3">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="admin-header-sidebar-toggle h-8 w-8 rounded-sm p-0 text-slate-500 hover:bg-transparent hover:text-slate-700"
                  onClick={() => setIsSidebarCollapsed((previousState) => !previousState)}
                  aria-label={isSidebarCollapsed ? "展开侧边栏" : "收起侧边栏"}
                >
                  {isSidebarCollapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
                </Button>
                <div className="h-5 w-px bg-slate-200" />
                <h2 className="truncate text-xl font-semibold tracking-tight">{activeContent.title}</h2>
                <div className="lg:hidden">
                  <Select
                    value={currentMenuPath}
                    onValueChange={handleNavigateMenu}
                  >
                    <SelectTrigger className="h-8 w-[148px] text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {adminMenuItems.map((item) => (
                        <SelectItem key={item.key} value={item.path}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </header>

            <div className="border-t border-slate-200/80" />

            <main className="min-h-0 flex-1 overflow-auto overscroll-none px-4 pb-4 pt-2 lg:px-6 lg:pb-6 lg:pt-3">
              <div className="mx-auto max-w-[1500px]">
              {activeMenuItem?.key === "users" ? (
                <AdminUsersPage currentUserID={adminIdentity.userId} dataGateway={dataGateway} onLogout={onLogout} />
              ) : activeMenuItem?.key === "profile" ? (
                <AdminProfilePage dataGateway={dataGateway} onProfileUpdated={handleProfileUpdated} />
              ) : activeMenuItem?.key === "spaces" ? (
                <AdminSpacesPage dataGateway={dataGateway} mode={spaceManagementMode} />
              ) : activeMenuItem?.key === "documents" ? (
                <AdminDocumentsPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "shares" ? (
                <AdminDocumentSharesPage
                  dataGateway={dataGateway}
                  currentUserRoles={adminIdentity.roles}
                  currentUserID={adminIdentity.userId}
                />
              ) : activeMenuItem?.key === "attachments" ? (
                <AdminDocumentAttachmentsPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "images" ? (
                <AdminDocumentImagesPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "document-templates" ? (
                <AdminDocumentTemplatesPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "themes" ? (
                <AdminThemesPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "system" ? (
                <AdminSystemConfigsPage key="system-configs" dataGateway={dataGateway} scope="system" />
              ) : activeMenuItem?.key === "office-config" ? (
                <AdminSystemConfigsPage key="office-configs" dataGateway={dataGateway} scope="office" />
              ) : activeMenuItem?.key === "search-analyzers" ? (
                <AdminSearchAnalyzersPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "audits" ? (
                <AdminAuditsPage dataGateway={dataGateway} />
              ) : activeMenuItem?.key === "dashboard" ? (
                <AdminDashboard
                  key={adminIdentity.userId}
                  admin={dataGateway.admin}
                  menuItems={adminMenuItems}
                  onNavigate={handleNavigateMenu}
                />
              ) : (
                <Card className="border-slate-200 bg-white shadow-sm">
                  <CardHeader className="pb-3">
                    <CardTitle>{activeContent.title}</CardTitle>
                    <CardDescription>{activeContent.description}</CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-2 text-sm text-slate-600">
                    {activeContent.todo.map((todo) => (
                      <p key={todo}>- {todo}</p>
                    ))}
                  </CardContent>
                </Card>
              )}
              </div>
            </main>
          </div>
        </section>
      </div>
      </AdminSpaceTransferTaskProvider>
    </div>
  );
}
