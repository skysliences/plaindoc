# bugodoc

<img src="./apps/server/internal/server/view/static/favicon.png" width="160" alt="BUGO">

bugodoc 是面向中小团队的开源知识库和文档协作平台。团队可以在同一个空间里编写 Markdown、管理 Office 文件、检索有权访问的文档和分享内容。

本项目基于 Minho 的 PlainDoc 二次开发，保留原项目的版权声明，并沿用 MIT 许可证。 [上游 PlainDoc](https://github.com/lifei6671/plaindoc) · [许可证](./LICENSE)

## 功能

| 功能 | 说明 |
| --- | --- |
| 空间与目录 | 按空间、分类和多级文件夹组织文档。目录栏支持拖动调整宽度。 |
| Markdown 编辑 | 在阅读页进入编辑，支持实时分栏预览、自动保存、历史版本与恢复。支持 Mermaid、公式和代码高亮。 |
| Office 文件 | 配置 ONLYOFFICE Docs 后可在线编辑 Word（DOCX）和 Excel（XLSX），以及阅读、批注 PDF。支持将 CSV 导入为可编辑的 Excel 工作簿。 |
| 文件导入与导出 | 支持文件夹、ZIP 导入和目录解压。无法预览的文件保留原件，可按权限下载。Markdown 文档可导出 Markdown 或 PDF，空间可导出 EPUB 或 `.plaindoc` 交换包。 |
| 权限与分享 | 空间按角色协作，可为成员单独配置文档的查看、编辑、复制和导出权限。独立文档分享链接支持密码与有效期。 |
| 单篇文档查看密码 | 文档创建者可在编辑器的「文档锁设置」中设置、更换或移除查看密码。文档上锁后，有查看权限的用户也须输入密码，分享链接同样受保护。 |
| 全文搜索 | 跨空间检索标题和正文，结果按当前用户的文档权限过滤。可配置内置 Bleve 索引。 |
| 登录与安全 | 支持本地账号和 LDAP。密码登录需要验证器动态码，本地账号可绑定 Passkey 后直接登录。管理员可配置登录 IP 白名单及重置二次验证。 |
| 管理与审计 | 后台管理用户、空间、文档、资源和分享，支持审计日志及每篇文档的打开、查看、编辑、下载统计。 |

## 快速启动

### Docker Compose

从本仓库构建，需安装 Docker Engine 和 Docker Compose。在仓库根目录执行以下命令。

```bash
git clone https://github.com/skysliences/Bugodoc.git
cd Bugodoc
docker compose up -d --build
```

服务运行在 `8080` 端口，管理后台入口为 `/admin`。新数据库会创建初始管理员，首次密码登录需绑定动态验证码验证器。 [打开首页](http://localhost:8080/) · [初始管理员配置](./apps/server/internal/storage/migrations/sqlite/0008_seed_default_admin_account.up.sql)

```bash
docker compose logs -f plaindoc
curl -fsS http://localhost:8080/api/healthz
```

健康检查成功时，响应中的 `code` 为 `0`。默认使用 SQLite，数据卷 `plaindoc_data` 和 `plaindoc_uploads` 分别保存数据库和上传文件。

新部署时，在 Compose 服务配置中设置独立且长期保管的 `JWT_SECRET`，并将 `WEB_ORIGIN` 设为浏览器访问的站点来源。升级时保留原有密钥和数据卷，可继续验证已绑定的动态验证码及 Passkey。需要启用 Passkey 时，把 `AUTH_PASSKEY_ORIGIN` 设为站点的完整 HTTPS 来源。 [部署配置](./docs/BACKEND_DEVELOPER_GUIDE.md#5-配置系统来源默认值消费点)

### 本地开发

需要 Go 1.26 或更高版本。Node.js 推荐使用 24，与项目容器和持续集成一致。

```bash
make install
```

分别在两个终端运行 `make server-dev` 和 `make web-dev`。开发前端运行在 `3001`，后端运行在 `8080`。 [打开开发前端](http://localhost:3001/)

需要完整的阅读页服务端渲染时，构建前端和 SSR Worker 后，以 `make server-dev-ssr` 替换后端启动命令。

```bash
make web-build
make server-dev-ssr
```

## 项目架构

- `apps/server` 使用 Go、Gin 和 GORM，支持 SQLite、MySQL 与 PostgreSQL。首页和分类页使用 Go 模板渲染。
- `apps/web` 使用 React、TypeScript、Vite 和 CodeMirror。阅读页由 Go 服务管理的 Node Worker 执行 React SSR。
- ONLYOFFICE Docs 是可选的独立服务。部署并接入后，可在浏览器中编辑 Office 文档。 [ONLYOFFICE 接入说明](./docs/ONLYOFFICE_INTEGRATION_TECHNICAL_PLAN.md)

## 文档

- [文档导航](./docs/README.md)
- [前端开发指南](./docs/FRONTEND_DEVELOPER_GUIDE.md)
- [后端开发指南](./docs/BACKEND_DEVELOPER_GUIDE.md)
