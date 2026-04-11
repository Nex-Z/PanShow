import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { DownloadIcon, EyeIcon, FileIcon, FolderIcon, RefreshCwIcon } from "lucide-react";
import ReactMarkdown from "react-markdown";
import { api, RequestError } from "./api/client";
import type { DirectoryPassword, FileEntry, User } from "./types";

type View = "files" | "admin" | "login";
type R2Route = { kind: "directory"; path: string } | { kind: "file"; path: string };

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
  const [route, setRoute] = useState<R2Route>(() => parseR2Route(window.location.pathname));
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [file, setFile] = useState<FileEntry | null>(null);
  const [readme, setReadme] = useState("");
  const [requiredPaths, setRequiredPaths] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const currentPath = route.path;

  const navigateTo = useCallback((nextRoute: R2Route) => {
    window.history.pushState(null, "", toR2BrowserPath(nextRoute));
    setRoute(nextRoute);
  }, []);

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
        setReadme("");
        return;
      }
      const files = await api.listFiles(nextRoute.path, true);
      setEntries(files.entries);
      setReadme(files.readme ?? "");
    } catch (err) {
      if (err instanceof RequestError && err.error.code === "directory_password_required") {
        setRequiredPaths(err.error.requiredPaths ?? []);
        setRoute(nextRoute);
      } else {
        setError(err instanceof RequestError ? err.error.message : "读取路径失败");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(route);
  }, [route, load]);

  useEffect(() => {
    function handlePopState() {
      setRoute(parseR2Route(window.location.pathname));
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

  return (
    <section className="workspace">
      {readme ? (
        <aside className="readme-pane">
          <p className="eyebrow">公告</p>
          <article className="markdown">
            <ReactMarkdown>{readme}</ReactMarkdown>
          </article>
        </aside>
      ) : null}
      <div className="browser-pane">
        <div className="browser-tools">
          <div className="breadcrumbs" aria-label="路径导航">
            {breadcrumbs.map((item) => (
              <button className="link-button" key={item.path} onClick={() => navigateTo({ kind: "directory", path: item.path })}>
                {item.label}
              </button>
            ))}
          </div>
          {route.kind === "directory" ? (
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
          <dt>路径</dt>
          <dd>{file.path}</dd>
        </div>
        <div>
          <dt>大小</dt>
          <dd>{formatSize(file.size)}</dd>
        </div>
        <div>
          <dt>类型</dt>
          <dd>{file.contentType || "未知"}</dd>
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
      <input type="password" value={password} onChange={(event) => setPassword(event.target.value)} />
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
  const [notice, setNotice] = useState<{ type: "success" | "error"; text: string } | null>(null);

  const load = useCallback(async () => {
    const [status, config, users, rules] = await Promise.all([
      api.adminStatus(),
      api.adminConfig(),
      api.listUsers(),
      api.listDirectoryPasswords()
    ]);
    setStatus(status);
    setConfig(config);
    setUsers(users.users);
    setRules(rules.directoryPasswords);
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

  return (
    <section className="admin-grid">
      <div className="admin-panel">
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
      </div>

      <div className="admin-panel">
        <p className="eyebrow">用户</p>
        <form className="inline-form" onSubmit={(event) => void createUser(event)}>
          <input name="username" placeholder="用户名" required />
          <input name="password" placeholder="密码" type="password" required />
          <select name="role" defaultValue="user">
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
          <button className="button primary">创建</button>
        </form>
        <DataList
          rows={users.map((user) => ({
            key: String(user.id),
            title: user.username,
            meta: `${user.role} · ${user.active ? "启用" : "停用"}`
          }))}
        />
      </div>

      <div className="admin-panel wide">
        <p className="eyebrow">目录密码</p>
        <form className="inline-form" onSubmit={(event) => void createRule(event)}>
          <input name="path" placeholder="/protected/path" required />
          <input name="password" placeholder="密码" type="password" required />
          <button className="button primary">创建</button>
        </form>
        <div className="data-list">
          {rules.length === 0 ? <p className="muted">暂无数据。</p> : null}
          {rules.map((rule) => (
            <DirectoryPasswordRow key={rule.id} rule={rule} onUpdate={updateRule} onDelete={deleteRule} />
          ))}
        </div>
      </div>
      {notice ? <p className={`toast ${notice.type}`}>{notice.text}</p> : null}
    </section>
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
  if (!pathname.startsWith(prefix)) {
    window.history.replaceState(null, "", "/r2/");
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
