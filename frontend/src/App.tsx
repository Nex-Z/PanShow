import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  DownloadIcon,
  EyeIcon,
  EyeOffIcon,
  FileIcon,
  FolderIcon,
  RefreshCwIcon
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api, RequestError } from "./api/client";
import type { Announcement, DirectoryPassword, FileEntry, PublicAnnouncement, User } from "./types";

type View = "files" | "admin" | "login";
type R2Route = { kind: "directory"; path: string } | { kind: "file"; path: string };
type RouteHistoryState = { entries: R2Route[]; index: number };

const markdownPlugins = [remarkGfm];

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [view, setView] = useState<View>("files");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .me()
      .then(({ user }) => setUser(user))
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
  }, []);

  const handleLogout = useCallback(async () => {
    await api.logout().catch(() => undefined);
    setUser(null);
    setView("files");
  }, []);

  if (loading) {
    return <ShellState title="PanShow" message="正在连接服务" />;
  }

  return (
    <main className="app-shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">PanShow</p>
          <h1>{view === "admin" ? "管理后台" : view === "login" ? "登录" : "内容"}</h1>
        </div>
        <nav className="topbar-actions" aria-label="主导航">
          <button className={view === "files" ? "button primary" : "button ghost"} onClick={() => setView("files")}>
            内容
          </button>
          {user?.role === "admin" ? (
            <button className={view === "admin" ? "button primary" : "button ghost"} onClick={() => setView("admin")}>
              后台
            </button>
          ) : null}
          {user ? <span className="user-chip">{user.username}</span> : null}
          {user ? (
            <button className="button" onClick={handleLogout}>
              退出
            </button>
          ) : (
            <button className={view === "login" ? "button primary" : "button"} onClick={() => setView("login")}>
              登录
            </button>
          )}
        </nav>
      </header>
      {view === "files" ? <FileBrowser /> : null}
      {view === "login" ? (
        <LoginPage
          onLogin={(user) => {
            setUser(user);
            setView(user.role === "admin" ? "admin" : "files");
          }}
        />
      ) : null}
      {view === "admin" && user?.role === "admin" ? <AdminPanel /> : null}
    </main>
  );
}

function LoginPage({ onLogin }: { onLogin: (user: User) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      const result = await api.login(username, password);
      onLogin(result.user);
    } catch (err) {
      setError(err instanceof RequestError ? err.error.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="login-section">
      <div className="login-panel">
        <p className="eyebrow">PanShow</p>
        <h1>管理员登录</h1>
        <p className="muted">文件浏览不需要登录，管理后台需要管理员账号。</p>
        <form className="form-stack" onSubmit={handleSubmit}>
          <label>
            用户名
            <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
          </label>
          <label>
            密码
            <input
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              type="password"
              autoComplete="current-password"
            />
          </label>
          {error ? <p className="error-text">{error}</p> : null}
          <button className="button primary" disabled={submitting}>
            {submitting ? "登录中" : "登录"}
          </button>
        </form>
      </div>
    </section>
  );
}

function FileBrowser() {
  const initialPathname = useMemo(() => window.location.pathname, []);
  const initialNotFoundPath = useMemo(() => frontendNotFoundPath(initialPathname), [initialPathname]);
  const initialRoute = useMemo(() => parseR2Route(initialPathname), [initialPathname]);
  const [notFoundPath, setNotFoundPath] = useState(initialNotFoundPath);
  const [route, setRoute] = useState<R2Route>(initialRoute);
  const [routeHistory, setRouteHistory] = useState<RouteHistoryState>(() => ({ entries: [initialRoute], index: 0 }));
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [file, setFile] = useState<FileEntry | null>(null);
  const [announcements, setAnnouncements] = useState<PublicAnnouncement[]>([]);
  const [requiredPaths, setRequiredPaths] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const currentPath = route.path;
  const canGoBack = routeHistory.index > 0;
  const canGoForward = routeHistory.index < routeHistory.entries.length - 1;

  const goHome = useCallback(() => {
    const homeRoute: R2Route = { kind: "directory", path: "/" };
    window.history.replaceState(null, "", toR2BrowserPath(homeRoute));
    setNotFoundPath("");
    setRoute(homeRoute);
    setRouteHistory({ entries: [homeRoute], index: 0 });
  }, []);

  const navigateTo = useCallback(
    (nextRoute: R2Route) => {
      if (routesEqual(route, nextRoute)) {
        return;
      }
      window.history.pushState(null, "", toR2BrowserPath(nextRoute));
      setRoute(nextRoute);
      setRouteHistory((history) => appendRouteHistory(history, nextRoute));
    },
    [route]
  );

  const stepRouteHistory = useCallback(
    (offset: -1 | 1) => {
      const nextIndex = routeHistory.index + offset;
      if (nextIndex < 0 || nextIndex >= routeHistory.entries.length) {
        return;
      }
      window.history.go(offset);
    },
    [routeHistory]
  );

  const load = useCallback(async (nextRoute: R2Route) => {
    setLoading(true);
    setError("");
    setRequiredPaths([]);
    setFile(null);
    try {
      if (nextRoute.kind === "file") {
        const result = await api.fileDetail(nextRoute.path);
        setFile(result.file);
        setEntries([]);
        setAnnouncements([]);
        return;
      }
      const files = await api.listFiles(nextRoute.path);
      setEntries(files.entries);
      setAnnouncements(files.announcements ?? []);
    } catch (err) {
      if (err instanceof RequestError && err.error.code === "directory_password_required") {
        setRequiredPaths(err.error.requiredPaths ?? []);
        setAnnouncements(err.error.announcements ?? []);
        setEntries([]);
        setRoute(nextRoute);
      } else {
        setAnnouncements([]);
        setError(err instanceof RequestError ? err.error.message : "读取路径失败");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (notFoundPath) {
      return;
    }
    void load(route);
  }, [route, load, notFoundPath]);

  useEffect(() => {
    function handlePopState() {
      const nextNotFoundPath = frontendNotFoundPath(window.location.pathname);
      const nextRoute = parseR2Route(window.location.pathname);
      setNotFoundPath(nextNotFoundPath);
      setRoute(nextRoute);
      if (nextNotFoundPath) {
        setRouteHistory({ entries: [nextRoute], index: 0 });
        return;
      }
      setRouteHistory((history) => syncRouteHistory(history, nextRoute));
    }
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  const breadcrumbs = useMemo(() => buildBreadcrumbs(route), [route]);

  async function handleDownload(entry: Pick<FileEntry, "path">) {
    try {
      const result = await api.download(entry.path);
      triggerDownload(result.url);
    } catch (err) {
      setError(err instanceof RequestError ? err.error.message : "生成下载链接失败");
    }
  }

  async function handlePreview(entry: Pick<FileEntry, "path">) {
    try {
      const result = await api.preview(entry.path);
      window.open(result.url, "_blank", "noopener,noreferrer");
    } catch (err) {
      setError(err instanceof RequestError ? err.error.message : "生成预览链接失败");
    }
  }

  async function handleRefresh() {
    try {
      await api.refreshFileCache(currentPath);
      await load(route);
    } catch (err) {
      setError(err instanceof RequestError ? err.error.message : "刷新失败");
    }
  }

  if (notFoundPath) {
    return <NotFoundPage path={notFoundPath} onHome={goHome} />;
  }

  return (
    <section className="workspace">
      {announcements.length > 0 ? <AnnouncementPane announcements={announcements} /> : null}
      <div className="browser-pane">
        <div className="browser-tools">
          <div className="breadcrumbs" aria-label="路径导航">
            <div className="breadcrumb-history" aria-label="浏览历史">
              <button
                className="history-button"
                type="button"
                onClick={() => stepRouteHistory(-1)}
                disabled={!canGoBack}
                aria-label="后退"
              >
                <ChevronLeftIcon aria-hidden />
              </button>
              <button
                className="history-button"
                type="button"
                onClick={() => stepRouteHistory(1)}
                disabled={!canGoForward}
                aria-label="前进"
              >
                <ChevronRightIcon aria-hidden />
              </button>
            </div>
            <div className="breadcrumb-trail">
              {breadcrumbs.map((item) => (
                <button className="link-button" key={item.path} onClick={() => navigateTo({ kind: "directory", path: item.path })}>
                  {item.label}
                </button>
              ))}
            </div>
          </div>
          {route.kind === "directory" && requiredPaths.length === 0 ? (
            <button className="link-button" onClick={() => void handleRefresh()}>
              <RefreshCwIcon aria-hidden />
            </button>
          ) : null}
        </div>
        {requiredPaths.length > 0 ? (
          <DirectoryPasswordPrompt paths={requiredPaths} onPassed={() => void load(route)} />
        ) : null}
        {error ? <p className="error-text">{error}</p> : null}
        {loading ? <p className="muted">正在读取内容</p> : null}
        {file ? (
          <FileDetail
            file={file}
            onDownload={() => void handleDownload(file)}
            onPreview={() => void handlePreview(file)}
          />
        ) : (
          <div className="file-list">
            {entries.map((entry) => (
              <div className="file-row" key={entry.path}>
                <button
                  className="file-name"
                  title={entry.name}
                  onClick={() => navigateTo(entry.isDir ? { kind: "directory", path: entry.path } : { kind: "file", path: entry.path })}
                >
                  <span className="item-icon" aria-hidden>
                    {entry.isDir ? <FolderIcon /> : <FileIcon />}
                  </span>
                  <span className="sr-only">{entry.isDir ? "文件夹" : "文件"}</span>
                  <span className="item-name">{entry.name}</span>
                </button>
                <span className="muted file-meta">{entry.isDir ? "" : formatSize(entry.size)}</span>
              </div>
            ))}
            {!loading && entries.length === 0 && requiredPaths.length === 0 ? <EmptyState /> : null}
          </div>
        )}
      </div>
    </section>
  );
}

function FileDetail({ file, onDownload, onPreview }: { file: FileEntry; onDownload: () => void; onPreview: () => void }) {
  return (
    <section className="file-detail">
      <p className="eyebrow">内容详情</p>
      <h3>{file.name}</h3>
      <dl className="config-list">
        <div>
          <dt>大小</dt>
          <dd>{formatSize(file.size)}</dd>
        </div>
        <div>
          <dt>更新时间</dt>
          <dd>{file.lastModified ? new Date(file.lastModified).toLocaleString() : "未知"}</dd>
        </div>
      </dl>
      <div className="detail-actions">
        {isPreviewable(file) ? (
          <button className="button" onClick={onPreview}>
            <EyeIcon aria-hidden />
            预览
          </button>
        ) : null}
        <button className="button primary" onClick={onDownload}>
          <DownloadIcon aria-hidden />
          下载
        </button>
      </div>
    </section>
  );
}

function AnnouncementPane({ announcements }: { announcements: PublicAnnouncement[] }) {
  return (
    <aside className="announcement-pane">
      <div className="announcement-heading">
        <h2>公告</h2>
      </div>
      <div className="announcement-stack">
        {announcements.map((announcement) => (
          <AnnouncementEntry announcement={announcement} key={announcement.id} />
        ))}
      </div>
    </aside>
  );
}

function AnnouncementEntry({ announcement }: { announcement: PublicAnnouncement }) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [collapsible, setCollapsible] = useState(false);

  const measure = useCallback(() => {
    const body = bodyRef.current;
    if (!body) {
      return;
    }
    const styles = window.getComputedStyle(body);
    const fontSize = Number.parseFloat(styles.fontSize);
    const lineHeight = Number.parseFloat(styles.lineHeight) || fontSize * 1.7;
    setCollapsible(body.scrollHeight > lineHeight * 3 + 2);
  }, []);

  useEffect(() => {
    setExpanded(false);
  }, [announcement.content]);

  useEffect(() => {
    const frame = window.requestAnimationFrame(measure);
    const body = bodyRef.current;
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    if (body) {
      observer?.observe(body);
    }
    window.addEventListener("resize", measure);
    return () => {
      window.cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [announcement.content, measure]);

  return (
    <article className={`announcement-entry ${collapsible ? "collapsible" : ""} ${expanded ? "expanded" : "collapsed"}`}>
      <div className="markdown announcement-body" ref={bodyRef}>
        <ReactMarkdown remarkPlugins={markdownPlugins} skipHtml>
          {announcement.content}
        </ReactMarkdown>
      </div>
      {collapsible ? (
        <button className="link-button announcement-toggle" type="button" onClick={() => setExpanded((value) => !value)}>
          {expanded ? "收起" : "展开"}
        </button>
      ) : null}
    </article>
  );
}

function NotFoundPage({ path, onHome }: { path: string; onHome: () => void }) {
  const [secondsLeft, setSecondsLeft] = useState(3);

  useEffect(() => {
    setSecondsLeft(3);
    const interval = window.setInterval(() => {
      setSecondsLeft((value) => Math.max(0, value - 1));
    }, 1000);
    const timeout = window.setTimeout(onHome, 3000);
    return () => {
      window.clearInterval(interval);
      window.clearTimeout(timeout);
    };
  }, [path, onHome]);

  return (
    <section className="not-found-panel">
      <p className="eyebrow">404</p>
      <h2>这个页面走丢了</h2>
      <p className="muted">{path} 暂时没有可展示的内容，{secondsLeft} 秒后回到首页。</p>
      <button className="button primary" type="button" onClick={onHome}>
        回到首页
      </button>
    </section>
  );
}

function EmptyState() {
  return (
    <div className="empty-state">
      <p className="eyebrow">内容</p>
      <h3>这里还没有内容</h3>
      <p className="muted">换一个路径，或稍后再看。</p>
    </div>
  );
}

function DirectoryPasswordPrompt({ paths, onPassed }: { paths: string[]; onPassed: () => void }) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const path = paths[0] ?? "/";

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      await api.submitDirectoryPassword(path, password);
      onPassed();
    } catch (err) {
      setError(err instanceof RequestError ? err.error.message : "目录密码验证失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="access-panel" onSubmit={handleSubmit}>
      <h3>需要目录密码</h3>
      <p className="muted">这里包含受保护内容，请输入密码。</p>
      <div className="password-input-wrap">
        <input
          type={showPassword ? "text" : "password"}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          autoComplete="current-password"
        />
        <button
          className="password-toggle"
          type="button"
          onClick={() => setShowPassword((value) => !value)}
          aria-label={showPassword ? "隐藏密码" : "显示密码"}
        >
          {showPassword ? <EyeOffIcon aria-hidden /> : <EyeIcon aria-hidden />}
        </button>
      </div>
      {error ? <p className="error-text">{error}</p> : null}
      <button className="button primary" disabled={submitting}>
        {submitting ? "验证中" : "验证"}
      </button>
    </form>
  );
}

function AdminPanel() {
  const [status, setStatus] = useState<{ database: boolean; redis: boolean; r2: boolean } | null>(null);
  const [config, setConfig] = useState<{ r2Bucket: string; r2RootPrefix: string; corsOrigins?: string[] } | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [rules, setRules] = useState<DirectoryPassword[]>([]);
  const [announcements, setAnnouncements] = useState<Announcement[]>([]);
  const [notice, setNotice] = useState<{ type: "success" | "error"; text: string } | null>(null);

  const load = useCallback(async () => {
    const [status, config, users, rules, announcements] = await Promise.all([
      api.adminStatus(),
      api.adminConfig(),
      api.listUsers(),
      api.listDirectoryPasswords(),
      api.listAnnouncements()
    ]);
    setStatus(status);
    setConfig(config);
    setUsers(users.users);
    setRules(rules.directoryPasswords);
    setAnnouncements(announcements.announcements);
  }, []);

  useEffect(() => {
    void load().catch((err) =>
      setNotice({ type: "error", text: err instanceof RequestError ? err.error.message : "读取后台数据失败" })
    );
  }, [load]);

  async function runAdminAction(successText: string, action: () => Promise<void>) {
    setNotice(null);
    try {
      await action();
      setNotice({ type: "success", text: successText });
    } catch (err) {
      setNotice({ type: "error", text: err instanceof RequestError ? err.error.message : "操作失败" });
    }
  }

  async function createUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    await runAdminAction("用户已创建", async () => {
      await api.createUser(String(form.get("username")), String(form.get("password")), String(form.get("role")));
      formElement.reset();
      await load();
    });
  }

  async function createRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    await runAdminAction("目录密码已创建", async () => {
      await api.createDirectoryPassword(String(form.get("path")), String(form.get("password")), true);
      formElement.reset();
      await load();
    });
  }

  async function updateRule(id: number, body: { path?: string; password?: string; enabled?: boolean }) {
    await runAdminAction("目录密码已更新", async () => {
      await api.updateDirectoryPassword(id, body);
      await load();
    });
  }

  async function deleteRule(id: number) {
    await runAdminAction("目录密码已删除", async () => {
      await api.disableDirectoryPassword(id);
      await load();
    });
  }

  async function createAnnouncement(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    await runAdminAction("公告已发布", async () => {
      await api.createAnnouncement({
        title: "",
        pattern: String(form.get("pattern")),
        content: String(form.get("content")),
        enabled: form.get("enabled") === "on",
        sortOrder: Number(form.get("sortOrder") || 100)
      });
      formElement.reset();
      await load();
    });
  }

  async function updateAnnouncement(
    id: number,
    body: Partial<Pick<Announcement, "title" | "pattern" | "content" | "enabled" | "sortOrder">>
  ) {
    await runAdminAction("公告已更新", async () => {
      await api.updateAnnouncement(id, body);
      await load();
    });
  }

  async function deleteAnnouncement(id: number) {
    await runAdminAction("公告已删除", async () => {
      await api.deleteAnnouncement(id);
      await load();
    });
  }

  async function refreshAnnouncementCache() {
    await runAdminAction("公告缓存已刷新", async () => {
      await api.refreshAnnouncementCache();
      await load();
    });
  }

  return (
    <section className="admin-layout">
      <aside className="admin-sidebar">
        <section className="admin-panel">
          <p className="eyebrow">系统状态</p>
          <h2>连接</h2>
          <div className="status-grid">
            <StatusPill label="PostgreSQL" ok={status?.database} />
            <StatusPill label="Redis" ok={status?.redis} />
            <StatusPill label="R2" ok={status?.r2} />
          </div>
          {config ? (
            <dl className="config-list">
              <div>
                <dt>Bucket</dt>
                <dd>{config.r2Bucket || "未配置"}</dd>
              </div>
              <div>
                <dt>Root prefix</dt>
                <dd>{config.r2RootPrefix || "/"}</dd>
              </div>
              <div>
                <dt>CORS</dt>
                <dd>{config.corsOrigins?.join(", ") || "未配置"}</dd>
              </div>
            </dl>
          ) : null}
        </section>

        <section className="admin-panel">
          <p className="eyebrow">用户</p>
          <form className="admin-form compact-form" onSubmit={(event) => void createUser(event)}>
            <input name="username" placeholder="用户名" required />
            <input name="password" placeholder="密码" type="password" required />
            <select name="role" defaultValue="user">
              <option value="user">user</option>
              <option value="admin">admin</option>
            </select>
            <button className="button primary">创建用户</button>
          </form>
          <DataList
            rows={users.map((user) => ({
              key: String(user.id),
              title: user.username,
              meta: `${user.role} · ${user.active ? "启用" : "停用"}`
            }))}
          />
        </section>
      </aside>

      <div className="admin-main">
        <section className="admin-panel">
          <div className="section-heading compact">
            <div>
              <p className="eyebrow">公告</p>
              <h2>发布与缓存</h2>
            </div>
            <button className="button" onClick={() => void refreshAnnouncementCache()}>
              刷新公告缓存
            </button>
          </div>
          <form className="admin-form announcement-form" onSubmit={(event) => void createAnnouncement(event)}>
            <input name="pattern" placeholder="/ 或 /docs/**" defaultValue="/**" required />
            <input name="sortOrder" placeholder="排序" type="number" defaultValue={100} />
            <label className="checkbox-label">
              <input name="enabled" type="checkbox" defaultChecked />
              启用
            </label>
            <textarea name="content" placeholder="支持 Markdown，内容可较长" required />
            <button className="button primary">发布公告</button>
          </form>
          <div className="data-list">
            {announcements.length === 0 ? <p className="muted">暂无公告。</p> : null}
            {announcements.map((announcement) => (
              <AnnouncementRow
                key={announcement.id}
                announcement={announcement}
                onUpdate={updateAnnouncement}
                onDelete={deleteAnnouncement}
              />
            ))}
          </div>
        </section>

        <section className="admin-panel">
          <p className="eyebrow">目录密码</p>
          <form className="admin-form directory-form" onSubmit={(event) => void createRule(event)}>
            <input name="path" placeholder="/protected/path" required />
            <input name="password" placeholder="密码" type="password" required />
            <button className="button primary">创建目录密码</button>
          </form>
          <div className="data-list">
            {rules.length === 0 ? <p className="muted">暂无数据。</p> : null}
            {rules.map((rule) => (
              <DirectoryPasswordRow key={rule.id} rule={rule} onUpdate={updateRule} onDelete={deleteRule} />
            ))}
          </div>
        </section>
      </div>
      {notice ? <p className={`toast ${notice.type}`}>{notice.text}</p> : null}
    </section>
  );
}

function AnnouncementRow({
  announcement,
  onUpdate,
  onDelete
}: {
  announcement: Announcement;
  onUpdate: (
    id: number,
    body: Partial<Pick<Announcement, "title" | "pattern" | "content" | "enabled" | "sortOrder">>
  ) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
}) {
  const [pattern, setPattern] = useState(announcement.pattern);
  const [content, setContent] = useState(announcement.content);
  const [enabled, setEnabled] = useState(announcement.enabled);
  const [sortOrder, setSortOrder] = useState(announcement.sortOrder);

  useEffect(() => {
    setPattern(announcement.pattern);
    setContent(announcement.content);
    setEnabled(announcement.enabled);
    setSortOrder(announcement.sortOrder);
  }, [announcement]);

  async function handleSave() {
    await onUpdate(announcement.id, { pattern, content, enabled, sortOrder });
  }

  return (
    <article className="announcement-row">
      <div className="announcement-edit-grid">
        <label>
          目录规则
          <input value={pattern} onChange={(event) => setPattern(event.target.value)} />
        </label>
        <label>
          排序
          <input
            value={sortOrder}
            onChange={(event) => setSortOrder(Number(event.target.value || 0))}
            type="number"
          />
        </label>
        <label className="checkbox-label">
          <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
          启用
        </label>
      </div>
      <label>
        Markdown 内容
        <textarea value={content} onChange={(event) => setContent(event.target.value)} />
      </label>
      <details className="markdown-preview">
        <summary>预览</summary>
        <div className="markdown">
          <ReactMarkdown remarkPlugins={markdownPlugins} skipHtml>
            {content}
          </ReactMarkdown>
        </div>
      </details>
      <div className="row-actions">
        <span className="muted">#{announcement.id}</span>
        <button className="button primary" onClick={() => void handleSave()}>
          保存
        </button>
        <button className="button danger" onClick={() => void onDelete(announcement.id)}>
          删除
        </button>
      </div>
    </article>
  );
}

function DirectoryPasswordRow({
  rule,
  onUpdate,
  onDelete
}: {
  rule: DirectoryPassword;
  onUpdate: (id: number, body: { path?: string; password?: string; enabled?: boolean }) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
}) {
  const [path, setPath] = useState(rule.path);
  const [password, setPassword] = useState("");
  const [enabled, setEnabled] = useState(rule.enabled);

  useEffect(() => {
    setPath(rule.path);
    setPassword("");
    setEnabled(rule.enabled);
  }, [rule]);

  async function handleSave() {
    await onUpdate(rule.id, { path, password: password || undefined, enabled });
  }

  return (
    <div className="rule-row">
      <div className="rule-fields">
        <label>
          路径
          <input value={path} onChange={(event) => setPath(event.target.value)} />
        </label>
        <label>
          新密码
          <input
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder="留空则不修改"
            type="password"
          />
        </label>
        <label className="checkbox-label">
          <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
          启用
        </label>
      </div>
      <div className="row-actions">
        <span className="muted">v{rule.version}</span>
        <button className="button primary" onClick={() => void handleSave()}>
          保存
        </button>
        <button className="button danger" onClick={() => void onDelete(rule.id)}>
          删除
        </button>
      </div>
    </div>
  );
}

function StatusPill({ label, ok }: { label: string; ok?: boolean }) {
  return <span className={ok ? "status ok" : "status bad"}>{`${label}: ${ok ? "正常" : "异常"}`}</span>;
}

function DataList({ rows }: { rows: { key: string; title: string; meta: string }[] }) {
  if (rows.length === 0) {
    return <p className="muted">暂无数据。</p>;
  }
  return (
    <div className="data-list">
      {rows.map((row) => (
        <div className="data-row" key={row.key}>
          <strong>{row.title}</strong>
          <span className="muted">{row.meta}</span>
        </div>
      ))}
    </div>
  );
}

function ShellState({ title, message }: { title: string; message: string }) {
  return (
    <main className="login-screen">
      <section className="login-panel">
        <p className="eyebrow">{title}</p>
        <h1>{message}</h1>
      </section>
    </main>
  );
}

function parseR2Route(pathname: string): R2Route {
  const prefix = "/r2";
  if (pathname === "" || pathname === "/") {
    window.history.replaceState(null, "", "/r2/");
    return { kind: "directory", path: "/" };
  }
  if (!isR2BrowserPath(pathname)) {
    return { kind: "directory", path: "/" };
  }
  const trailingSlash = pathname.endsWith("/");
  const raw = safeDecodeURI(pathname.slice(prefix.length)) || "/";
  const path = normalizeBrowserPath(raw);
  if (!trailingSlash && hasFileExtension(path)) {
    return { kind: "file", path };
  }
  return { kind: "directory", path };
}

function frontendNotFoundPath(pathname: string) {
  if (pathname === "" || pathname === "/" || isR2BrowserPath(pathname)) {
    return "";
  }
  return safeDecodeURI(pathname);
}

function isR2BrowserPath(pathname: string) {
  return pathname === "/r2" || pathname.startsWith("/r2/");
}

function safeDecodeURI(value: string) {
  try {
    return decodeURI(value);
  } catch {
    return "/";
  }
}

function toR2BrowserPath(route: R2Route) {
  const encoded = route.path
    .split("/")
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join("/");
  const base = encoded ? `/r2/${encoded}` : "/r2";
  return route.kind === "directory" ? `${base}/` : base;
}

function normalizeBrowserPath(raw: string) {
  const parts = raw.split("/").filter(Boolean);
  return parts.length === 0 ? "/" : `/${parts.join("/")}`;
}

function hasFileExtension(path: string) {
  const name = path.split("/").filter(Boolean).at(-1) ?? "";
  return /\.[^/.]+$/.test(name);
}

function routesEqual(left: R2Route, right: R2Route) {
  return left.kind === right.kind && left.path === right.path;
}

function appendRouteHistory(history: RouteHistoryState, nextRoute: R2Route): RouteHistoryState {
  const currentRoute = history.entries[history.index];
  if (currentRoute && routesEqual(currentRoute, nextRoute)) {
    return history;
  }
  const entries = history.entries.slice(0, history.index + 1);
  entries.push(nextRoute);
  return { entries, index: entries.length - 1 };
}

function syncRouteHistory(history: RouteHistoryState, nextRoute: R2Route): RouteHistoryState {
  const currentRoute = history.entries[history.index];
  if (currentRoute && routesEqual(currentRoute, nextRoute)) {
    return history;
  }
  const previousIndex = history.index - 1;
  const nextIndex = history.index + 1;
  if (previousIndex >= 0 && routesEqual(history.entries[previousIndex], nextRoute)) {
    return { ...history, index: previousIndex };
  }
  if (nextIndex < history.entries.length && routesEqual(history.entries[nextIndex], nextRoute)) {
    return { ...history, index: nextIndex };
  }
  return { entries: [nextRoute], index: 0 };
}

function buildBreadcrumbs(route: R2Route) {
  const path = route.kind === "file" ? route.path.split("/").slice(0, -1).join("/") || "/" : route.path;
  const parts = path.split("/").filter(Boolean);
  const items = [{ label: "根目录", path: "/" }];
  let cursor = "";
  for (const part of parts) {
    cursor += `/${part}`;
    items.push({ label: part, path: cursor });
  }
  return items;
}

function formatSize(size: number) {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function isPreviewable(entry: Pick<FileEntry, "name" | "contentType">) {
  const contentType = entry.contentType?.toLowerCase() ?? "";
  if (
    contentType.startsWith("text/") ||
    contentType.startsWith("image/") ||
    contentType.startsWith("audio/") ||
    contentType.startsWith("video/") ||
    contentType === "application/pdf" ||
    contentType === "application/json"
  ) {
    return true;
  }
  return /\.(txt|md|json|csv|log|xml|html|css|js|ts|tsx|jsx|svg|png|jpe?g|gif|webp|avif|pdf|mp3|mp4|webm|ogg|wav)$/i.test(
    entry.name
  );
}

function triggerDownload(url: string) {
  const link = document.createElement("a");
  link.href = url;
  link.rel = "noopener noreferrer";
  link.download = "";
  document.body.appendChild(link);
  link.click();
  link.remove();
}
