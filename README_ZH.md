# OzDDNS

[English](README.md) | 简体中文

OzDDNS 是一个以 DNS 服务商为中心的动态 DNS 管理器，用于将 DNS 记录持续同步到
HTTP 服务、本机网卡、命令或静态配置所提供的地址。

项目以单个 Go 二进制交付，内置 Vue 管理面板、REST API、管理 CLI、调度器和
SQLite 存储。共享协调引擎会读取当前 DNS 记录、计算差异，并且仅在目标状态不一致时
执行写入。

> OzDDNS 仍在持续开发中。对外开放或重新分发前，请先阅读
> [安全说明](#安全说明)与[许可证状态](#许可证)。

## 主要特性

- **以协调为核心**：写入前同时读取并展示当前值与目标值。

- **安全预演**：可通过管理面板、CLI 或 REST API 对任务执行 Dry Run。

- **服务商实例化管理**：可独立配置多个服务商账号，并在不同任务间复用。

- **灵活的地址来源**：支持 HTTP 查询、本机网卡、Shell 命令和静态 IPv4/IPv6。

- **统一业务逻辑**：管理面板、CLI 和 API 调用同一应用层。

- **内置自动化能力**：任务独立周期、可选抖动、并发执行、暂停/恢复、全部运行和
  滚动执行历史。

- **运维能力完整**：Webhook 通知、配置备份恢复、可选密码 + TOTP 授权、健康检查
  和 CLI JSON 输出。

- **单二进制交付**：生产前端通过 `go:embed` 嵌入 Go 二进制。

- **多语言与主题**：管理面板支持 English、简体中文、日本語，以及亮色、暗色和
  跟随系统主题。

## 支持的服务商

| 服务商         | 类型           |
| ----------- | ------------ |
| Cloudflare  | `cloudflare` |
| 阿里云 DNS     | `alidns`     |
| DNSPod（腾讯云） | `dnspod`     |
| DuckDNS     | `duckdns`    |
| Namecheap   | `namecheap`  |
| GoDaddy     | `godaddy`    |
| deSEC       | `desec`      |
| Vercel DNS  | `vercel`     |
| 自定义 HTTP 端点 | `custom`     |

OzDDNS 可管理 IPv4 `A` 和 IPv6 `AAAA` 目标记录，具体能力取决于所选服务商。
Cloudflare 记录支持代理状态。

## 快速开始

### 从源码构建

环境要求：

- Go 1.26 或更高版本

- Node.js 22 或更高版本及 npm

- GNU Make

```bash
git clone https://github.com/echoface/ozddns.git
cd ozddns
make build

./bin/ozddns --db ./ozddns.db serve
```

打开 <http://127.0.0.1:9876>。

如需使用示例服务商、任务和执行历史体验界面，可用全新数据库启动 Demo：

```bash
./bin/ozddns --db ./demo.db serve --demo
```

`make build` 会安装锁定版本的前端依赖、构建 Vue 面板、将其嵌入并生成
`./bin/ozddns`。

### 运行已发布镜像

发布镜像存放在 GitHub Container Registry，同时支持 `linux/amd64` 和
`linux/arm64`：

```bash
docker volume create ozddns-data

docker run -d \
  --name ozddns \
  --restart unless-stopped \
  -p 127.0.0.1:9876:9876 \
  -v ozddns-data:/data \
  ghcr.io/echoface/ozddns:latest \
  --db /data/ozddns.db serve --listen 0.0.0.0:9876
```

需要可复现部署时，请使用 `ghcr.io/echoface/ozddns:1.1.0` 这样的版本标签。

### Docker Compose

```bash
docker compose up -d          # 拉取 ghcr.io/echoface/ozddns:latest
docker compose up -d --build  # 或在本地构建同一镜像
```

仓库内置的 Compose 配置会：

- 运行已发布镜像 `ghcr.io/echoface/ozddns:latest`，也可用 `OZDDNS_IMAGE`
  指定其他标签（例如 `OZDDNS_IMAGE=ghcr.io/echoface/ozddns:1.1.0`）；

- 默认使用**host 网络模式**，让 `interface` 类型的 IP 来源能读取**宿主机**网卡
  （桥接容器只能看到自己的网卡）。该模式没有端口映射，面板由 `OZDDNS_LISTEN`
  绑定，默认 `127.0.0.1:9876`；设为 `0.0.0.0:9876` 才会对外暴露；

- 在 Docker Desktop（macOS/Windows）上请去掉 `network_mode`、恢复 `ports`
  映射并把 `OZDDNS_LISTEN` 设为 `0.0.0.0:9876`，此时网卡来源只能看到容器自身网卡；

- 将 SQLite 数据库存放在 `./data`；

- 首次启动时向数据卷写入默认 `config.yaml`（`./data/config.yaml`），
  可在下次重启前编辑；

- 在容器内使用非 root 用户运行；

- 提供 HTTP 健康检查。

查看日志：

```bash
docker compose logs -f
```

### 部署配置文件

进程如何运行——监听地址、数据库路径、日志级别——属于**部署配置**：启动时解析一次，
不会存入数据库，也不在面板中修改（从面板改监听地址会把你自己正在使用的连接切断）。

在二进制文件旁边放置可选的 `config.yaml`：

```yaml
# config.yaml
listen: 127.0.0.1:9876    # 默认保持回环；只有可信反向代理前置时才考虑对外监听
db: /data/ozddns.db
logLevel: info
basePath: /                # 对外访问前缀，子路径反代时例如 /ddns
trustProxy: false          # 仅当可信反向代理会覆盖转发头时才设为 true
trustedNetwork: false      # 仅当监听地址只在自己控制的网络内可达时才设为 true
```

该文件是可选的：`serve` 启动时若不存在，OzDDNS 会写入这份带注释的默认配置并继续运行
（目录只读不算错误）。用 `--config` 或 `OZDDNS_CONFIG` 指定其他路径；`--config` 也可
放在子命令之后，例如 `ozddns serve --config /etc/ozddns/config.yaml`。

所有部署项遵循同一优先级，越具体的来源优先级越高：
**命令行参数 > 环境变量 > `config.yaml` > 内置默认值**。每种设置都可以用任一方式给出，
名称一一对应如下。

| 环境变量 | 命令行参数 | `config.yaml` 键 | 默认值 | 作用 |
| --- | --- | --- | --- | --- |
| `OZDDNS_LISTEN` | `--listen` | `listen` | `127.0.0.1:9876` | API 与管理面板的监听地址。 |
| `OZDDNS_DB` | `--db` | `db` | `./ozddns.db` | SQLite 数据库路径。 |
| `OZDDNS_LOG_LEVEL` | `--log-level` | `logLevel` | `info` | `debug`、`info`、`warn` 或 `error`。 |
| `OZDDNS_BASE_PATH` | `--base-path` | `basePath` | `/` | 挂载在子路径反向代理后时的对外前缀。 |
| `OZDDNS_TRUST_PROXY` | `--trust-proxy` | `trustProxy` | `false` | 信任由反向代理**覆盖写入**的 `X-Forwarded-Proto` / `X-Forwarded-For`。 |
| `OZDDNS_TRUSTED_NETWORK` | `--trusted-network` | `trustedNetwork` | `false` | 声明监听地址只在自己控制的网络内可达；关闭“超出回环”暴露警告。 |
| `OZDDNS_CONFIG` | `--config` | — | `./config.yaml` | 配置文件路径（文件自身位置没有对应的键）。 |

`trustProxy` 不是通用的“已经启用 HTTPS”开关。只有当**每个请求**都经过可信反向代理，
并且代理会**覆盖** `X-Forwarded-Proto` 与 `X-Forwarded-For` 时才能开启。它同时决定
Session Cookie 的 `Secure` 属性，以及限流和“仅回环可首次配置”判断所使用的客户端
地址。直接可达的监听若信任这些请求头，客户端就可以伪造来源地址。

`trustedNetwork` 同样不是代理开关。它是运营者的显式声明：监听地址只在自己控制的网络内
可达——局域网，或 Tailscale 之类的覆盖网——因此有意绑定在回环之外的开放面板不再被标记。
它只去掉警告与面板提示，本身不提供任何访问控制。任何公网可直达的监听都不要开启它，并请
同时收紧绑定、代理 ACL 与网络 ACL。

`ozddns health` 会按同一套优先级解析地址并探测 `/healthz`，容器与脚本健康检查无需硬编码端口。

`ozddns serve` 就是守护进程：一个长驻进程同时承载 HTTP 服务、调度器、协调执行和
执行历史清理，不存在第二个 OzDDNS 监督进程。systemd、Docker 等进程管理器监督的是
这一个进程。

### 反向代理（nginx）

OzDDNS 自身不处理 TLS。请把它放在 nginx 之后，让守护进程继续只监听回环地址，由代理终止
HTTPS。面板既可以独占一个域名，也可以挂在共享域名的子路径下：

| 部署方式 | 对外地址 | 启动参数 |
| --- | --- | --- |
| 独立域名 | `https://ddns.example.com/` | 默认值 |
| 子路径 | `https://app.example.com/ddns/` | `--base-path /ddns` |

挂载点属于启动参数，因此同一份镜像两种拓扑都能用，无需为部署重新构建：

```bash
ozddns serve --base-path /ddns      # 或用 OZDDNS_BASE_PATH=/ddns，或 config.yaml 中的 basePath
```

前缀由守护进程自己剥离，反向代理只需原样转发，不需要任何 rewrite；面板使用 hash 路由，
不需要 history 模式的 `try_files` 回退。可直接复制的 server 配置、完整说明和排查清单见
[在 nginx 后面运行 OzDDNS](docs/nginx.md)：

- [nginx/ozddns-dedicated-domain.conf](docs/nginx/ozddns-dedicated-domain.conf)

- [nginx/ozddns-subpath.conf](docs/nginx/ozddns-subpath.conf)

## 首次配置

管理面板是最短的配置路径：

1. 添加并验证 DNS 服务商。
2. 添加 IP 来源。
3. 创建同步任务及 DNS 目标。
4. 预演协调差异。
5. 手动运行一次，然后保持调度器启用。
6. 面板对回环地址之外开放前，先启用授权。

### 访问模式

- **本地开放模式（默认）**：服务监听 `127.0.0.1:9876`，API 请求无需凭据。适用于
  可信的单机、单用户环境。
- **授权模式**：在 **设置 → 安全** 中开启。配置流程会创建唯一管理员、密码、
  TOTP 身份验证器和 10 个一次性恢复码。登录流程为：密码 → TOTP/恢复码 →
  服务端 Session。

首次授权配置只接受来自回环地址的请求，避免尚未授权的对外实例被远端抢先接管。
请先在本机完成配置，再配置反向代理并对外提供服务。

在面板中重新验证密码和 TOTP/恢复码后可以关闭授权。关闭会删除管理员、所有会话、
TOTP 密钥和全部恢复码；再次开启必须使用新的 TOTP 密钥重新完成全部配置。

授权只保护 HTTP 管理面；浏览器退出登录后，调度器、协调执行和通知仍会继续运行。
当前只支持单管理员，没有独立 API Token 或角色模型；脚本调用受保护接口时必须完成
同样的登录流程并保存 `ozddns_session` Cookie。

如果身份验证器和恢复码都丢失，请在运行数据库的机器上使用本地应急命令：

```bash
./bin/ozddns --db ./ozddns.db auth disable --yes
```

下面的“服务商 → IP 来源 → 任务”流程也可以通过 CLI 完成。授权配置本身有意只允许
本机通过面板完成；CLI 只提供 `auth status` 和依赖文件系统权限的
`auth disable --yes` 应急路径。

```bash
./bin/ozddns --db ./ozddns.db provider add \
  --type cloudflare \
  --name "Home Cloudflare" \
  --set api_token="$CF_API_TOKEN"

./bin/ozddns --db ./ozddns.db source add \
  --name "Public IPv4" \
  --type http \
  --family v4 \
  --target https://api.ipify.org

./bin/ozddns --db ./ozddns.db job add \
  --name "Home IPv4" \
  --provider "Home Cloudflare" \
  --source "Public IPv4" \
  --target home.example.com:A:300 \
  --jitter

./bin/ozddns --db ./ozddns.db job run --dry-run <job-id>
./bin/ozddns --db ./ozddns.db job run <job-id>
```

使用 `ozddns --help` 或 `ozddns <command> --help` 查看完整 CLI 说明。
支持机器可读输出的命令可在命令前添加 `--json`。

## 配置备份

导出和恢复服务商、IP 来源、任务及设置：

```bash
./bin/ozddns --db ./ozddns.db config export --out ozddns-config.json
./bin/ozddns --db ./ozddns.db config import --replace ozddns-config.json
```

管理面板的 **设置 → 存储** 提供相同操作。执行历史不包含在配置包中。

> 配置导出包含服务商凭据，以便恢复一个可工作的实例；授权状态、密码哈希、TOTP
> 密钥和恢复码不会被导出。请按照敏感信息的标准保护导出文件。

## 架构

```text
管理面板 ─┐
REST API ─┼── 应用服务 ── 调度器 / 协调引擎
CLI ──────┘       │               │
                  ├── SQLite      ├── IP 来源
                  └── 实例设置     └── DNS 服务商
```

主要目录：

```text
cmd/ozddns/          单二进制入口
internal/app/        面板、API 和 CLI 共享的用例
internal/auth/       密码、TOTP、恢复码与令牌基础能力
internal/engine/     调度器与协调差异计算
internal/provider/   服务商协议与内置适配器
internal/ipsource/   HTTP、网卡、命令和静态地址来源
internal/storage/    存储接口与 SQLite 实现
internal/server/     REST API 与 HTTP 中间件
scripts/             隔离的浏览器端到端测试
web/v2/              Vue 3、TypeScript 和 Vite 管理面板
web/                 嵌入式生产资源
```

更多文档：

- [设计与架构](docs/01-design.md)

- [REST API 参考](docs/api_references.md)

- [在 nginx 后面运行 OzDDNS](docs/nginx.md)

## 扩展服务商

服务商适配器需要实现 `provider.Factory` 和 `provider.Provider`，然后在
`internal/providers/all.go` 注册一个 Factory。服务商元数据描述配置字段，因此面板
无需编写服务商专用 UI 即可渲染配置表单。

协调引擎不依赖具体服务商：适配器负责验证凭据、列出区域与记录并应用变更；引擎负责
获取地址、计算差异、调度和记录执行历史。

最小的内置适配器示例见 `internal/provider/custom/custom.go`。

## 安全说明

- 监听地址属于部署配置，不是存储的设置：启动时由 `--listen`、`OZDDNS_LISTEN`
  或 `config.yaml` 解析（默认 `127.0.0.1:9876`），面板仅只读展示。

- 授权是可选功能。开放模式只有在监听回环地址时才适合使用；当开放实例可从其他
  主机访问时，守护进程会记录警告，面板也会显示全局提示。

- `--trusted-network` / `OZDDNS_TRUSTED_NETWORK` / `trustedNetwork` 可对“仅在
  自己控制的网络内可达”的监听关闭上述警告——例如局域网或 Tailscale 覆盖网。它不
  改变任何访问控制；公网可直达的监听不得开启。

- 首次授权配置只接受回环客户端。若前置反向代理，代理必须覆盖转发头，并且 OzDDNS
  必须以 `--trust-proxy` 启动；否则服务端无法区分远端客户端与本地代理。

- 授权模式使用 PBKDF2-SHA256 密码哈希、RFC 6238 TOTP、一次性恢复码和服务端
  Session。浏览器只保存 `HttpOnly`、`SameSite=Lax` Cookie，数据库只保存令牌哈希。

- Session 在闲置 30 分钟或总计 12 小时后过期；关闭授权会立即使所有 Session 失效。

- 当前安全模型为单管理员，不支持角色、按用户权限或独立 API Token；登录限流状态
  由单个 OzDDNS 进程按“来源 IP + 操作类型”维护。使用有硬性上限的 token bucket，
  最多保留 8192 个计数桶，并优先淘汰过期或较旧条目，不会随来源 IP 数量无限增长。

- 服务商密钥在常规 API、CLI 和面板读取时会被隐藏。

- 服务商凭据会存储在 SQLite 数据库和配置导出中。请使用适合敏感信息的文件权限与
  备份策略保护两者。

- OzDDNS 本身不终止 TLS。向其他网络开放前，请使用可信反向代理提供 HTTPS。

- 命令类型的 IP 来源会执行本地 Shell 命令，只应允许可信管理员配置。

## 开发

```bash
make web-v2      # 仅构建管理面板
make build       # 构建面板与单二进制
make test        # 运行 Go 测试
make test-race   # 使用竞态检测器运行测试
make vet         # 运行 go vet
make cover       # 生成 coverage.out
make docker      # 构建容器镜像
make hooks       # 为当前克隆安装 Git 钩子
```

前端开发：

```bash
cd web/v2
npm ci
npm run dev
```

授权与受保护面板的完整交互还由真实 Headless Chrome/CDP 测试覆盖。启动全新 OzDDNS
实例和独立的 Headless Chromium CDP 端点后运行：

```bash
OZDDNS_E2E_URL=http://127.0.0.1:9876 \
OZDDNS_CDP_URL=http://127.0.0.1:9222 \
node scripts/e2e-auth-cdp.mjs
```

该脚本驱动真实构建产物，不使用用户日常浏览器会话；覆盖本地开放模式、健康检查、
TOTP 配置、恢复码、会话刷新、离线退出保护、API 拦截、TOTP/恢复码重放拒绝、
关闭与重新开启授权、旧会话失效、主要路由以及桌面/移动端横向溢出。

生成的 `web/v2/dist` 会被有意提交到仓库，因为 `go:embed`、`go install`
以及直接执行 Go 测试都需要仓库中存在可嵌入的面板资源。

仓库内置了纳入版本管理的 `pre-push` 钩子，用于保持该目录同步：推送前会重新构建
`web/v2`，若 `web/v2/dist` 与源码不一致则拒绝推送。每个克隆安装一次即可：

```bash
make hooks
```

如需临时跳过，可在推送时使用 `--no-verify`。

### 发布版本

`Publish container image` GitHub Actions 工作流会在 Pull Request 中验证容器构建，
并将多架构镜像发布到 `ghcr.io/echoface/ozddns`：

- 分支推送不会发布镜像；

- 推送 `v1.2.3` 这样的 Git tag 会发布 `v1.2.3`、`1.2.3`、`1.2` 和
  `latest`。

创建发布镜像：

```bash
git tag v1.0.0
git push origin v1.0.0
```

不需要额外配置镜像仓库密钥；工作流使用 GitHub 提供的受限 `GITHUB_TOKEN` 发布。
第一次成功运行后，如需允许匿名拉取，请在 GitHub 的 Package 设置中将可见性修改为
**Public**。GitHub 会提示该可见性修改无法撤销。

## 致谢

OzDDNS 的设计参考了两个成熟 DDNS 项目的源码和实践：

- [qdm12/ddns-updater](https://github.com/qdm12/ddns-updater) 展示了广泛的
  服务商生态、周期性 DNS 协调、Web 界面和实用的容器交付方式。

- [jeessy2/ddns-go](https://github.com/jeessy2/ddns-go) 展示了易用的嵌入式
  管理面板、多种地址获取方式、Webhook 集成和直接的自托管部署体验。

OzDDNS 是独立实现。项目采用了“地址发现与 DNS 更新分离”这一经过实践验证的思路，
同时使用服务商注册表、规范化 SQLite 存储和共享差异引擎实现自己的架构。

本项目也依赖并受益于以下开源项目：

- [Go](https://go.dev/) 及其标准库；

- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，后端使用的
  无 CGo SQLite 驱动；

- [Vue](https://vuejs.org/) 和
  [Vue Router](https://router.vuejs.org/)，用于管理面板；

- [Vite](https://vite.dev/) 和
  [TypeScript](https://www.typescriptlang.org/)，用于前端开发与生产构建；

- [SQLite](https://www.sqlite.org/)，用于可靠的本地持久化。

感谢这些项目的维护者和贡献者。相关项目名称及商标归各自所有者所有。

## 许可证

本仓库当前未包含项目许可证。源码可见不代表已授予使用、修改或再分发权限。在将
OzDDNS 作为开源版本发布前，需要先选择并添加许可证。
