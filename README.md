# 盘秀

PanShow 是一个简化版的 alist。当前目标聚焦在用户体系、目录密码鉴权、Cloudflare R2 文件/目录浏览、路径公告、文件展示、下载，以及管理员后台配置和查看，不做过度复杂化。

## 技术栈

- 后端：Go、Gin、GORM、PostgreSQL、Redis、Cloudflare R2 S3 API
- 前端：React、Vite、TypeScript、Tailwind CSS、shadcn/ui
- 生产形态：后端服务可通过 Go `embed` 托管 `frontend/dist`，发布时可以只部署一个服务端二进制和生产 `.env`

## 开发环境准备

后端需要先准备 PostgreSQL、Redis、Cloudflare R2，并复制 `backend/.env.example` 为 `backend/.env` 后填写配置。后端启动时会按顺序尝试读取：二进制同级 `.env`、二进制同级 `config/.env`、当前工作目录 `.env`、当前工作目录 `config/.env`、开发场景的 `backend/.env`；系统环境变量优先级最高，已存在的环境变量不会被 `.env` 覆盖。

前端默认通过 Vite 代理访问 `http://127.0.0.1:8080` 的后端接口。如需修改开发代理地址，在 `frontend/.env` 设置：

```text
VITE_DEV_API_PROXY_TARGET=http://127.0.0.1:5245
```

后台登录鉴权使用 `Authorization: Bearer` token；登录接口会返回 token，前端保存后自动带到后续后台请求里。目录密码通过状态仍使用独立访问 cookie。

## 本地启动

后端启动：

```powershell
chcp 65001
cd backend
Copy-Item .env.example .env
go run ./cmd/server
```

前端启动：

```powershell
cd frontend
npm install
npm run dev
```

前端开发地址默认是：

```text
http://127.0.0.1:5173
```

文件浏览路径规则：

```text
/r2/2025/10/31/          # 直达目录页
/r2/2025/10/31/file.ext  # 直达文件详情页
```

文件浏览不要求登录；受目录密码保护的路径会提示输入密码。管理员登录后可以进入后台，且管理员访问文件时不需要输入目录密码。

后端会用 Redis 缓存 R2 目录列表、文件详情和当前版本的启用公告列表，R2 缓存默认 1800 秒，可通过 `PANSHOW_R2_CACHE_TTL_SECONDS` 调整。列表页的“刷新”按钮会强制清理当前路径及其子级文件缓存，再重新读取；公告在管理员后台发布、修改、停启或手动刷新后会立即切换到新版本缓存。

## 生产构建

生产推荐把前端产物打进后端二进制。关键顺序是：先构建前端，再把 `frontend/dist` 复制到 `backend/internal/web/dist`，最后编译后端。Go 编译时会通过 `embed` 把该目录内容写入二进制；如果漏掉复制步骤，后端 API 仍可编译运行，但前端页面不会可用。

构建工具建议版本：

- Go 1.24.x
- Node.js 满足当前 Vite 要求：`^20.19.0 || >=22.12.0`
- npm 使用仓库里的 `frontend/package-lock.json`，生产构建优先使用 `npm ci`

### 一键构建

从项目根目录执行：

```powershell
chcp 65001
.\scripts\build-production.ps1
```

脚本会依次安装前端依赖、执行 `npm run build`、复制 `frontend/dist` 到 `backend/internal/web/dist`、`go test ./...`、生产 `go build`，最后生成发布目录和 zip 包。默认依赖安装模式是 `auto`：已有 `node_modules` 时用 `npm install`，干净环境用 `npm ci`。默认 API Base 是同域 `/api/*`，脚本会覆盖 `frontend/.env` 里的本地开发地址，避免生产包请求 `127.0.0.1`。

常用参数：

```powershell
# 构建 Linux x64，默认值
.\scripts\build-production.ps1

# 构建 Windows x64，用于本机临时验证
.\scripts\build-production.ps1 -Target windows-amd64

# 前后端分离部署时，把 API 地址写进前端产物
.\scripts\build-production.ps1 -ApiBaseUrl "https://api.example.com"

# 严格使用 npm ci 安装依赖
.\scripts\build-production.ps1 -FrontendInstallMode ci

# 只生成二进制，不打 zip
.\scripts\build-production.ps1 -NoArchive

# 临时跳过 Go 测试
.\scripts\build-production.ps1 -SkipTests
```

默认 Linux x64 产物：

```text
backend/bin/panshow-server
release/panshow-linux-amd64/
release/panshow-linux-amd64.zip
```

Windows x64 产物：

```text
backend/bin/panshow-server.exe
release/panshow-windows-amd64/
release/panshow-windows-amd64.zip
```

发布目录会包含二进制、`config/.env.example`、`logs/` 和 `README.md`。生产机上只需要二进制、生产 `.env`（放在二进制同级或 `config/.env`）、PostgreSQL、Redis 和可访问的 R2 bucket。

## 生产配置

建议生产环境把 `.env` 放在二进制同级目录，或放在二进制同级的 `config/.env`。服务也会读取当前工作目录下的 `.env` / `config/.env`，但生产部署更推荐跟随二进制目录，避免启动目录变化导致读错配置。示例：

```dotenv
PANSHOW_HTTP_ADDR=127.0.0.1:5245
PANSHOW_GIN_MODE=release
PANSHOW_LOG_DIR=logs
PANSHOW_LOG_MAX_SIZE_MB=50
PANSHOW_LOG_MAX_BACKUPS=14
PANSHOW_LOG_MAX_AGE_DAYS=30
PANSHOW_DATABASE_URL=postgres://panshow:strong-password@127.0.0.1:5432/panshow?sslmode=disable
PANSHOW_REDIS_ADDR=127.0.0.1:6379
PANSHOW_REDIS_PASSWORD=
PANSHOW_REDIS_DB=0
PANSHOW_SESSION_TTL_HOURS=24
PANSHOW_COOKIE_SECURE=true
PANSHOW_COOKIE_SAME_SITE=lax
PANSHOW_CORS_ORIGINS=https://panshow.example.com
PANSHOW_R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
PANSHOW_R2_ACCESS_KEY=<access-key>
PANSHOW_R2_SECRET_KEY=<secret-key>
PANSHOW_R2_BUCKET=<bucket>
PANSHOW_R2_REGION=auto
PANSHOW_R2_ROOT_PREFIX=
PANSHOW_R2_CACHE_TTL_SECONDS=1800
PANSHOW_ADMIN_USERNAME=admin
PANSHOW_ADMIN_PASSWORD=<initial-admin-password>
```

配置说明：

- `PANSHOW_HTTP_ADDR`：后端监听地址。放在 Nginx 后面时建议绑定 `127.0.0.1:5245`。
- `PANSHOW_GIN_MODE`：生产使用 `release`。
- `PANSHOW_LOG_DIR`：日志目录。相对路径会按二进制同级目录解析；默认写入 `logs/panshow.log`。
- `PANSHOW_LOG_MAX_SIZE_MB` / `PANSHOW_LOG_MAX_BACKUPS` / `PANSHOW_LOG_MAX_AGE_DAYS`：日志切割和归档策略。默认单文件 50 MB 切割，归档到 `logs/archive/`，保留 14 个归档且最多保留 30 天。
- `PANSHOW_DATABASE_URL`：PostgreSQL 连接串。服务启动时会执行 GORM AutoMigrate。
- `PANSHOW_REDIS_*`：Redis 连接、会话、目录密码通过状态、登录失败限流和缓存都依赖 Redis。
- `PANSHOW_COOKIE_SECURE`：HTTPS 生产环境建议 `true`。
- `PANSHOW_COOKIE_SAME_SITE`：同站部署用 `lax`；如果前端和 API 是跨站域名并依赖 cookie，则使用 `none`，同时必须启用 HTTPS 和 `PANSHOW_COOKIE_SECURE=true`。
- `PANSHOW_CORS_ORIGINS`：允许访问 API 的前端 origin，多个值用英文逗号分隔。单进程同域部署时仍建议填正式站点 origin。
- `PANSHOW_R2_ROOT_PREFIX`：可选，用于把站点根目录限制在 R2 bucket 的某个前缀下。
- `PANSHOW_ADMIN_USERNAME` / `PANSHOW_ADMIN_PASSWORD`：仅在数据库里没有管理员时创建首个管理员；创建后应改用后台管理账号，不要把真实密码写进发布包。
- `VITE_API_BASE_URL`：前端构建期变量。单进程同域部署保持同域默认值即可；一键构建脚本默认会强制同域 `/api/*`，不会使用 `frontend/.env` 里的 `127.0.0.1`。如果前端和 API 分开部署，构建时通过 `-ApiBaseUrl https://api.example.com` 设置，并同步配置 `PANSHOW_CORS_ORIGINS`。

## 部署

### 直接运行

Linux 服务器示例：

```bash
unzip panshow-linux-amd64.zip -d /opt/panshow
cd /opt/panshow
cp config/.env.example config/.env
chmod +x ./panshow-server
./panshow-server
```

启动后验证：

```bash
curl http://127.0.0.1:5245/healthz
curl -I http://127.0.0.1:5245/
curl -I http://127.0.0.1:5245/r2/
```

日志会写入 `logs/panshow.log`；切割后的归档日志会放在 `logs/archive/`。

`/api/*` 和 `/healthz` 仍然由后端接口处理；其他 GET/HEAD 路由会优先读取嵌入静态文件，找不到真实文件时回退到前端 SPA 的 `index.html`，因此 `/r2/*` 可以直接刷新或分享。

### Nginx 反向代理

因为后端已经托管前端静态资源，Nginx 只需要把站点整体转发到后端服务：

```nginx
server {
    listen 80;
    server_name panshow.example.com;

    location / {
        proxy_pass http://127.0.0.1:5245;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

生产环境建议在 Nginx 或上游网关终止 HTTPS，并保持 `.env` 里的 `PANSHOW_COOKIE_SECURE=true`。如果不使用内嵌前端，也可以单独托管 `frontend/dist`，再只把 `/api/*` 和 `/healthz` 反代到后端；但当前推荐单进程内嵌部署。

### systemd 示例
```
sudo vim /etc/systemd/system/panshow.service
```

```ini
[Unit]
Description=PanShow
After=network.target

[Service]
Type=simple
User=panshow
WorkingDirectory=/opt/panshow
EnvironmentFile=/opt/panshow/config/.env
ExecStart=/opt/panshow/panshow-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

部署或更新后：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now panshow
sudo systemctl status panshow
```

## 发布检查清单

- 单进程同域部署直接运行 `.\scripts\build-production.ps1`；只有前后端分离部署才传 `-ApiBaseUrl https://api.example.com`。
- 脚本默认输出 `release/panshow-linux-amd64.zip`，并包含 Linux 二进制、`config/.env.example`、`logs/` 和 `README.md`。
- 生产 `.env` 不要使用示例密码，建议放在二进制同级 `.env` 或 `config/.env`，且不要打进公开发布包。
- PostgreSQL、Redis、R2 bucket 和 R2 API key 已准备好，生产机能访问这些服务。
- `PANSHOW_GIN_MODE=release`，HTTPS 环境下 `PANSHOW_COOKIE_SECURE=true`，日志策略按需调整 `PANSHOW_LOG_*`。
- 访问 `/healthz` 返回 `{ "ok": true }`。
- 访问 `/`、`/r2/`、某个实际文件详情路径，刷新页面不应 404。
- 登录管理员后台后检查 `/api/admin/status` 或后台状态页。

## 外部接口：修改目录密码

外部程序可以复用后台登录接口获取管理员 token，再调用专用接口按目录密码 ID 修改明文密码。登录失败会按用户名和客户端 IP 写入 Redis 限流，10 分钟内任一维度失败 8 次后返回 `429 too_many_login_attempts`。

登录获取 token：

```powershell
$login = Invoke-RestMethod -Method Post -Uri http://127.0.0.1:5245/api/auth/login -ContentType 'application/json' -Body '{"username":"admin","password":"admin-password"}'
$token = $login.token
```

按目录密码 ID 修改密码：

```powershell
$body = '{"password":"new-directory-password"}'
Invoke-RestMethod -Method Patch -Uri http://127.0.0.1:5245/api/admin/directory-passwords/1/password -Headers @{ Authorization = "Bearer $token" } -ContentType 'application/json' -Body $body
```

接口要求调用者是管理员；服务端只保存 bcrypt 哈希，成功后会递增该目录密码的版本并刷新目录鉴权缓存，让旧目录密码通过状态失效。生产环境务必走 HTTPS，避免明文密码在链路或日志里泄漏。

## 编码约束

所有文本文件保持 UTF-8 无 BOM。Windows 下写入前先确认 `chcp` 为 `65001`，不要使用默认编码、GBK 或 ANSI；如果发现中文乱码，先从正确源文件、历史版本或上下文确认原文，再替换。
