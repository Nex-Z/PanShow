\# 盘秀



\# 核心理念

做一个简化版的 alist ，我们只需要用户体系、目录鉴权（目录密码鉴权）、对 CF R2 的文件/目录浏览、README公告、文件展示、下载、管理员后台（配置、查看）

不要搞很复杂



\# 主要技术选型

Go + PostgreSQL + redis + React + shadcn/ui + Cloudflare R2 S3 API


\# 开发环境准备

后端需要先准备 PostgreSQL、Redis、Cloudflare R2，并复制 `backend/.env.example` 为 `backend/.env` 后填写配置。后端启动时会自动读取 `.env`；系统环境变量优先级更高。

前端默认通过 Vite 代理访问 `http://localhost:8080` 的后端接口。如需分离部署，参考 `frontend/.env.example` 配置 `VITE_API_BASE_URL`。

后台登录鉴权使用 `Authorization: Bearer` token；登录接口会返回 token，前端保存后自动带到后续后台请求里。目录密码通过状态仍使用独立的访问 cookie。

本地开发可以不设置 `VITE_API_BASE_URL`，让前端通过 Vite 的 `/api` 代理访问后端。如果后端端口不是 8080，在 `frontend/.env` 设置 `VITE_DEV_API_PROXY_TARGET`，例如 `http://127.0.0.1:5245`。如果直接设置 `VITE_API_BASE_URL` 访问后端，也需要确保 `PANSHOW_CORS_ORIGINS` 包含前端页面的 origin。

\# 启动命令

后端启动：

```powershell
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

后端会用 Redis 缓存 R2 目录列表、README 和文件详情，默认缓存 60 秒，可通过 `PANSHOW_R2_CACHE_TTL_SECONDS` 调整。列表页的“刷新”按钮会强制清理当前路径及其子级缓存，再重新读取。

\# 构建命令

后端测试：

```powershell
cd backend
go test ./...
```

后端构建：

```powershell
cd backend
go build -o ./bin/panshow-server.exe ./cmd/server
```

前端构建：

```powershell
cd frontend
npm install
npm run build
```

\# 打包命令

Windows 下推荐先构建后端和前端，再把后端可执行文件、前端 `dist`、示例环境变量和文档打包：

```powershell
cd backend
go build -o ./bin/panshow-server.exe ./cmd/server

cd ..\frontend
npm install
npm run build

cd ..
Compress-Archive -Path .\backend\bin\panshow-server.exe,.\backend\.env.example,.\frontend\dist,.\docs,.\README.md -DestinationPath .\panshow-release.zip -Force
```

生产部署时需要把 `/r2/*` 回退到前端 SPA 的 `index.html`，并让 `/api/*` 与 `/healthz` 转发到后端服务。
