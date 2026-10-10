# OzDDNS 后端 API 对接参考

本文档依据当前后端实现整理，覆盖 `internal/server/server.go` 注册的全部 34 个
`/api/v1` 接口。接口以 REST + JSON 提供，主要供 Web 面板或第三方自动化程序调用。

## 1. 基础约定

### 1.1 地址与请求头

- API 前缀：`/api/v1`
- 默认监听地址：`http://127.0.0.1:9876`
- 请求体格式：`Content-Type: application/json`
- 响应体格式：`Content-Type: application/json; charset=utf-8`
- 时间格式：Go `time.Time` 的 RFC 3339 字符串，例如 `2026-10-10T08:30:00Z`
- 资源 ID：服务商、IP 源和任务使用字符串 ID；执行记录使用整数 ID

以下示例统一使用：

```bash
export OZDDNS_URL=http://127.0.0.1:9876
export OZDDNS_USER=admin
export OZDDNS_PASSWORD=your-password
```

### 1.2 鉴权

实例未设置密码时，接口允许匿名访问。设置密码后，所有请求必须携带 HTTP Basic
Auth：

```bash
curl -u "$OZDDNS_USER:$OZDDNS_PASSWORD" \
  "$OZDDNS_URL/api/v1/status"
```

认证失败：

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Basic realm="OzDDNS"
Content-Type: application/json; charset=utf-8
```

```json
{"error":"authentication required"}
```

### 1.3 通用错误

常规错误响应：

```json
{"error":"错误说明"}
```

常见状态码：

| 状态码 | 说明 |
|---|---|
| `200` | 查询、更新、删除或动作执行成功 |
| `201` | 资源创建成功 |
| `400` | JSON 无法解析、参数校验失败或资源仍被引用 |
| `401` | 已启用密码，但未提供或提供了错误的 Basic Auth |
| `404` | 资源不存在 |
| `405` | 路径存在，但 HTTP 方法不匹配 |
| `500` | 存储、聚合查询或未恢复的内部错误 |

以下动作接口采用“HTTP 200 + `ok`”表达业务结果，调用方不能只检查 HTTP 状态码：

- `POST /providers/{id}/validate`
- `POST /ip-sources/{id}/test`
- `POST /settings/test-notify`

`POST /jobs/{id}/run` 在任务已开始并产生执行记录后，即使同步失败也会返回 HTTP
200，调用方应检查响应中的 `status`。

### 1.4 PATCH 语义

`PATCH` 是部分更新：

- 未提供的字段保持不变。
- 布尔字段使用指针语义，可明确提交 `false`。
- 服务商、IP 源和任务的部分字符串字段提交空字符串时保持不变。
- 服务商 `config` 一旦提供，会作为新的配置对象处理；已有密钥会按“密钥掩码规则”合并保留。
- `settings.notifications` 一旦提供，会整体替换现有通知渠道数组。
- JSON 中未知字段当前会被忽略。

### 1.5 密钥掩码

服务商的密钥字段在读取时不会返回原文：

- 长度不超过 4 个字符：`••••`
- 其他值：`••••` 加最后 4 个字符，例如 `••••1234`

更新服务商时，密钥字段缺失、为空或仍是掩码值，表示保留原密钥；提交新的明文才会
替换。创建服务商时不能提交掩码值。

## 2. 接口总览

### 2.1 元数据与运行状态

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/bootstrap` | 获取面板初始化所需的完整聚合数据 |
| `GET` | `/provider-types` | 获取已注册的 DNS 服务商类型及配置字段 |
| `GET` | `/ip-source-types` | 获取已注册的 IP 来源类型 |
| `GET` | `/ip-sources/interfaces` | 获取本机可用网卡及地址 |
| `GET` | `/status` | 获取服务运行状态和执行统计 |
| `GET` | `/ip` | 立即探测当前 IPv4/IPv6 |
| `POST` | `/scheduler` | 暂停或恢复定时调度 |

### 2.2 DNS 服务商

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/providers` | 服务商列表 |
| `POST` | `/providers` | 创建服务商 |
| `GET` | `/providers/{id}` | 服务商详情 |
| `PATCH` | `/providers/{id}` | 部分更新服务商 |
| `DELETE` | `/providers/{id}` | 删除服务商 |
| `POST` | `/providers/{id}/validate` | 实时验证凭据和连通性 |
| `GET` | `/providers/{id}/zones` | 实时读取可管理 Zone |

### 2.3 IP 来源

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/ip-sources` | IP 来源列表 |
| `POST` | `/ip-sources` | 创建 IP 来源 |
| `PATCH` | `/ip-sources/{id}` | 部分更新 IP 来源 |
| `DELETE` | `/ip-sources/{id}` | 删除 IP 来源 |
| `POST` | `/ip-sources/{id}/test` | 立即测试 IP 解析 |

### 2.4 同步任务

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/jobs` | 任务列表 |
| `POST` | `/jobs` | 创建任务 |
| `GET` | `/jobs/{id}` | 任务详情 |
| `PATCH` | `/jobs/{id}` | 部分更新任务 |
| `DELETE` | `/jobs/{id}` | 删除任务 |
| `POST` | `/jobs/{id}/run` | 立即执行单个任务 |
| `POST` | `/jobs/{id}/dry-run` | 计算变更但不写 DNS |
| `POST` | `/jobs/run-all` | 顺序执行全部启用任务 |

### 2.5 执行记录

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/runs` | 查询执行记录 |
| `GET` | `/runs/{id}` | 获取单条执行记录 |
| `POST` | `/runs/purge` | 按保留周期立即清理历史 |

### 2.6 系统设置

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/settings` | 获取设置 |
| `PATCH` | `/settings` | 部分更新设置 |
| `POST` | `/settings/password` | 设置或清除登录密码 |
| `POST` | `/settings/test-notify` | 测试通知渠道 |

### 2.7 配置备份

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/config/export` | 导出服务商、IP 来源、任务和设置 |
| `POST` | `/config/import` | 用备份完整替换当前配置 |

## 3. 公共数据模型

### 3.1 ProviderView

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | 服务商实例 ID |
| `name` | `string` | 显示名称 |
| `type` | `string` | 服务商类型，见第 10 节 |
| `enabled` | `boolean` | 是否启用 |
| `config` | `object` | 配置；密钥字段已掩码 |
| `status` | `string` | `ok`、`warn`、`err` 或禁用时的 `mute` |
| `statusText` | `string` | 状态说明 |
| `zones` | `string[]` | 最近一次成功验证时缓存的 Zone 名称 |
| `records` | `integer` | 引用该服务商的任务目标记录总数 |
| `lastValidated` | `string` | 最近验证时间；未验证时可能为零时间 |
| `createdAt` | `string` | 创建时间 |
| `updatedAt` | `string` | 更新时间 |

### 3.2 IPSource

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | IP 来源 ID |
| `name` | `string` | 名称 |
| `type` | `string` | `http`、`interface`、`command`、`static` |
| `family` | `string` | `v4` 或 `v6` |
| `target` | `string` | URL、网卡名、Shell 命令或静态 IP |
| `enabled` | `boolean` | 是否启用 |
| `createdAt` | `string` | 创建时间 |
| `updatedAt` | `string` | 更新时间 |

不同类型的 `target`：

| `type` | `target` 示例 | 行为 |
|---|---|---|
| `http` | `https://api.ipify.org` | GET URL，从响应文本提取首个匹配地址 |
| `interface` | `en0` 或 `en0#1` | 读取网卡首个或指定序号的同族全局单播地址 |
| `command` | `tailscale ip -6` | 执行 `sh -c`，从标准输出提取地址 |
| `static` | `203.0.113.10` | 使用固定 IP |

### 3.3 Target

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `domain` | `string` | 是 | 完整域名 |
| `type` | `string` | 是 | `A`、`AAAA` 或 `CNAME` |
| `ttl` | `integer` | 否 | TTL 秒数，具体支持取决于服务商 |
| `proxied` | `boolean` | 否 | Cloudflare 代理开关 |
| `comment` | `string` | 否 | 记录备注 |

### 3.4 JobView

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | 任务 ID |
| `name` | `string` | 任务名称 |
| `providerId` | `string` | 服务商 ID |
| `providerName` | `string` | 服务商名称，后端反范式补充 |
| `providerType` | `string` | 服务商类型，后端反范式补充 |
| `ipSourceId` | `string` | IP 来源 ID |
| `ipSourceName` | `string` | IP 来源名称，后端反范式补充 |
| `targets` | `Target[]` | 要维护的 DNS 记录 |
| `intervalMinutes` | `integer` | 执行间隔，单位分钟 |
| `runOnChangeOnly` | `boolean` | 仅地址变化时执行的配置标记；当前执行链仅持久化和返回该字段，尚未据此跳过任务 |
| `jitter` | `boolean` | 是否启用调度抖动 |
| `enabled` | `boolean` | 是否启用 |
| `state` | `JobState` | 任务运行态 |
| `createdAt` | `string` | 创建时间 |
| `updatedAt` | `string` | 更新时间 |

`JobState`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `jobId` | `string` | 任务 ID |
| `lastStatus` | `string` | `success`、`nochange`、`failed`、`skipped` 或空 |
| `lastRunAt` | `string` | 最近执行时间 |
| `nextRunAt` | `string` | 下次执行时间 |
| `current` | `string` | 最近已知当前地址 |
| `desired` | `string` | 最近解析出的目标地址 |
| `error` | `string` | 最近错误；成功后清空 |
| `lastChangedAt` | `string` | 最近实际修改 DNS 的时间 |

### 3.5 Run

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `integer` | 执行记录 ID |
| `jobId` | `string` | 任务 ID |
| `startedAt` | `string` | 开始时间 |
| `finishedAt` | `string` | 完成时间 |
| `durationMs` | `integer` | 耗时毫秒 |
| `status` | `string` | `success`、`nochange`、`failed`、`skipped` |
| `oldIp` | `string` | 原地址；为空时省略 |
| `newIp` | `string` | 目标地址；为空时省略 |
| `trigger` | `string` | `manual` 或 `schedule` |
| `message` | `string` | 结果说明；为空时省略 |
| `records` | `integer` | 实际创建或更新数量；为 0 时省略 |

### 3.6 Settings

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `language` | `string` | `en` | 界面语言 |
| `theme` | `string` | `light` | 界面主题 |
| `timezone` | `string` | `UTC` | 界面时区 |
| `defaultInterval` | `integer` | `5` | 新任务默认间隔（分钟） |
| `schedulerPaused` | `boolean` | `false` | 是否暂停自动调度 |
| `schedulerTickSeconds` | `integer` | `5` | 扫描到期任务的周期 |
| `schedulerWorkers` | `integer` | `4` | 最大并发执行数 |
| `runOnStart` | `boolean` | `true` | 启动时是否执行一次 |
| `runRetentionDays` | `integer` | 有效值 `1` | 执行历史保留天数，范围 1～365 |
| `username` | `string` | 空 | Basic Auth 用户名 |
| `passwordSet` | `boolean` | `false` | 是否已设置密码，只读状态字段 |
| `allowWan` | `boolean` | `false` | WAN 访问配置标记 |
| `tls` | `boolean` | `false` | TLS 配置标记 |
| `listen` | `string` | `127.0.0.1:9876` | 监听地址配置 |
| `notifications` | `Notification[]` | `[]` | 通知渠道 |

`Notification`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | 渠道 ID |
| `kind` | `string` | `slack`、`discord`、`ntfy`、`gotify` 或 `webhook` |
| `name` | `string` | 渠道名称 |
| `target` | `string` | 接收 POST JSON 的 URL |
| `enabled` | `boolean` | 是否启用 |
| `on` | `string[]` | `change`、`failure`、`success` 或 `all` |

## 4. 元数据与运行状态接口

### 4.1 获取初始化数据

```http
GET /api/v1/bootstrap
```

一次返回面板初始化所需的全部数据：

```json
{
  "providers": [],
  "ipSources": [],
  "jobs": [],
  "runs": [],
  "ip": {
    "v4": "203.0.113.10",
    "v6": "",
    "v4Source": "default",
    "v6Source": "",
    "checkedAt": "2026-10-10T08:30:00Z"
  },
  "status": {},
  "scheduler": {
    "running": true,
    "tickSeconds": 5,
    "workers": 4,
    "runOnStart": true,
    "defaultIntervalMinutes": 5
  },
  "settings": {},
  "providerTypes": [],
  "ipSourceTypes": [],
  "interfaces": []
}
```

说明：

- `runs` 最多返回最近 200 条。
- 会实时探测公网 IPv4/IPv6，因此响应时间受 IP 来源影响，内部总超时约 8 秒。
- 任一关键聚合查询失败时返回 `500`。

### 4.2 获取服务商类型

```http
GET /api/v1/provider-types
```

响应为 `ProviderMeta[]`：

```json
[
  {
    "id": "cloudflare",
    "label": "Cloudflare",
    "desc": "API Token, zones & proxied records (A / AAAA / CNAME)",
    "color": "cloudflare",
    "tag": "cloudflare",
    "supportsProxied": true,
    "fields": [
      {
        "key": "api_token",
        "label": "API Token",
        "type": "secret",
        "required": true,
        "secret": true,
        "help": "..."
      }
    ]
  }
]
```

字段 `type` 可为 `text`、`secret`、`bool`、`number`、`select`、`textarea`。
建议客户端根据本接口动态生成服务商配置表单。

### 4.3 获取 IP 来源类型

```http
GET /api/v1/ip-source-types
```

```json
[
  {
    "id": "command",
    "label": "Shell command",
    "desc": "Run a command and extract the first matching IP.",
    "icon": "terminal"
  }
]
```

当前类型：`command`、`http`、`interface`、`static`。

### 4.4 获取本机网卡

```http
GET /api/v1/ip-sources/interfaces
```

```json
[
  {
    "name": "en0",
    "up": true,
    "addresses": [
      {
        "target": "en0",
        "name": "en0",
        "addr": "192.0.2.10",
        "family": "v4",
        "index": 0,
        "prefix": 24
      }
    ]
  }
]
```

回环、链路本地和不可用地址不会返回。一个网卡存在多个同族地址时，后续地址的
`target` 形如 `en0#1`。

### 4.5 获取运行状态

```http
GET /api/v1/status
```

```json
{
  "version": "dev",
  "build": "",
  "scheduler": "running",
  "uptimeSeconds": 3600,
  "providers": 2,
  "jobs": 3,
  "jobsEnabled": 2,
  "runsSuccess": 10,
  "runsNoChange": 25,
  "runsFailed": 1,
  "runsSkipped": 0,
  "dbDriver": "sqlite",
  "dbPath": "/data/ozddns.db",
  "listen": "127.0.0.1:9876",
  "serverTime": "2026-10-10T08:30:00Z"
}
```

### 4.6 探测当前 IP

```http
GET /api/v1/ip
```

```json
{
  "v4": "203.0.113.10",
  "v6": "2001:db8::10",
  "v4Source": "Home IPv4",
  "v6Source": "Home IPv6",
  "checkedAt": "2026-10-10T08:30:00Z"
}
```

选择规则：

1. 每个地址族使用第一个已启用且族匹配的已配置来源。
2. 未配置时，IPv4 使用 `https://api.ipify.org`，IPv6 使用
   `https://v6.ifconfig.co`。
3. 某个地址族解析失败时，该族地址和来源返回空字符串，接口仍返回 `200`。

### 4.7 暂停或恢复调度器

```http
POST /api/v1/scheduler
```

请求：

```json
{"running":false}
```

响应：

```json
{"running":false}
```

该操作会将反向值持久化到 `settings.schedulerPaused`，重启后仍有效。请求体不含
`running` 时只返回当前状态，不修改配置。

## 5. DNS 服务商接口

### 5.1 获取服务商列表

```http
GET /api/v1/providers
```

响应：`ProviderView[]`。所有密钥字段均已掩码。

### 5.2 创建服务商

```http
POST /api/v1/providers
```

请求模型：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `name` | `string` | 建议必填 | 显示名称 |
| `type` | `string` | 是 | 服务商类型 |
| `config` | `object` | 是 | 对应类型的配置 |
| `enabled` | `boolean` | 否 | 默认 `true` |

示例：

```bash
curl -u "$OZDDNS_USER:$OZDDNS_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Cloudflare Main",
    "type": "cloudflare",
    "config": {"api_token": "plain-text-token"},
    "enabled": true
  }' \
  "$OZDDNS_URL/api/v1/providers"
```

成功返回 `201` 和 `ProviderView`。创建时先校验配置结构，保存后会尝试实时验证；
实时验证失败不会撤销创建，返回对象的 `status` 会反映失败。

### 5.3 获取服务商详情

```http
GET /api/v1/providers/{id}
```

成功返回 `ProviderView`；不存在返回 `404`。

### 5.4 更新服务商

```http
PATCH /api/v1/providers/{id}
```

请求字段与创建接口相同，均可省略。示例：

```json
{
  "name": "Cloudflare Production",
  "config": {
    "api_token": "••••1234",
    "zone_id": "zone-id"
  },
  "enabled": true
}
```

示例中的掩码会被替换为数据库中原有密钥。更新后重新执行配置校验和实时验证。

### 5.5 删除服务商

```http
DELETE /api/v1/providers/{id}
```

成功：

```json
{"ok":true}
```

仍有任务引用时返回 `400`：

```json
{"error":"provider is used by 2 task(s); reassign them first"}
```

### 5.6 验证服务商

```http
POST /api/v1/providers/{id}/validate
```

实时验证凭据，并尝试读取 Zone。成功：

```json
{
  "ok": true,
  "provider": {
    "id": "p-...",
    "status": "ok",
    "statusText": "validated successfully",
    "zones": ["example.com"]
  }
}
```

业务失败仍返回 HTTP `200`：

```json
{
  "ok": false,
  "error": "provider error detail",
  "provider": {
    "id": "p-...",
    "status": "err"
  }
}
```

资源不存在时也会落入该业务失败格式，调用方应检查 `ok`。

### 5.7 获取服务商 Zone

```http
GET /api/v1/providers/{id}/zones
```

实时请求服务商，不使用缓存：

```json
[
  {"id":"zone-id","name":"example.com","status":"active"}
]
```

失败按常规 `4xx/5xx` 错误返回。

## 6. IP 来源接口

### 6.1 获取 IP 来源列表

```http
GET /api/v1/ip-sources
```

响应：`IPSource[]`。

### 6.2 创建 IP 来源

```http
POST /api/v1/ip-sources
```

请求：

```json
{
  "name": "Public IPv4",
  "type": "http",
  "family": "v4",
  "target": "https://api.ipify.org",
  "enabled": true
}
```

校验规则：

- `name` 非空。
- `type` 必须为 `http`、`interface`、`command` 或 `static`。
- `family` 必须为 `v4` 或 `v6`。
- `target` 非空，并能通过对应来源工厂的结构校验。
- `enabled` 默认 `true`。

成功返回 `201` 和 `IPSource`。

### 6.3 更新 IP 来源

```http
PATCH /api/v1/ip-sources/{id}
```

请求字段与创建接口相同，均可省略。若有任务引用该来源，不允许改变 `family`：

```json
{
  "error": "address family is locked: Home DNS still use this source — reassign those tasks first, or add a new source"
}
```

### 6.4 删除 IP 来源

```http
DELETE /api/v1/ip-sources/{id}
```

成功：

```json
{"ok":true}
```

若仍被任务引用则返回 `400`，必须先重分配或删除任务。

### 6.5 测试 IP 来源

```http
POST /api/v1/ip-sources/{id}/test
```

成功：

```json
{"ok":true,"ip":"203.0.113.10"}
```

业务失败仍返回 `200`：

```json
{"ok":false,"error":"no v4 address found in response"}
```

单次测试总超时约 12 秒；HTTP 和命令来源自身还有约 10 秒超时。

## 7. 同步任务接口

### 7.1 获取任务列表

```http
GET /api/v1/jobs
```

响应：`JobView[]`。

### 7.2 创建任务

```http
POST /api/v1/jobs
```

请求模型：

| 字段 | 类型 | 必填 | 默认值 |
|---|---|---|---|
| `name` | `string` | 是 | 无 |
| `providerId` | `string` | 是 | 无 |
| `ipSourceId` | `string` | 是 | 无 |
| `targets` | `Target[]` | 是，至少 1 项 | 无 |
| `intervalMinutes` | `integer` | 建议提供 | 存储值为 `0`；调度计算时按 5 分钟处理 |
| `runOnChangeOnly` | `boolean` | 否 | `true` |
| `jitter` | `boolean` | 否 | `false` |
| `enabled` | `boolean` | 否 | `true` |

请求示例：

```json
{
  "name": "Home DNS",
  "providerId": "p-abc",
  "ipSourceId": "src-def",
  "targets": [
    {
      "domain": "home.example.com",
      "type": "A",
      "ttl": 300,
      "proxied": false,
      "comment": ""
    }
  ],
  "intervalMinutes": 5,
  "runOnChangeOnly": true,
  "jitter": false,
  "enabled": true
}
```

校验规则：

- 名称去除首尾空白后不能为空。
- 服务商和 IP 来源必须存在。
- 至少一个目标，每个目标的 `domain` 非空。
- 记录类型只能是 `A`、`AAAA`、`CNAME`。

成功返回 `201` 和 `JobView`。新任务的 `nextRunAt` 会设为当前时间，以便尽快被调度。

### 7.3 获取任务详情

```http
GET /api/v1/jobs/{id}
```

成功返回 `JobView`；不存在返回 `404`。

### 7.4 更新任务

```http
PATCH /api/v1/jobs/{id}
```

请求字段与创建接口相同，均可省略。注意：

- `targets: []` 会触发“至少一个目标”校验错误。
- `intervalMinutes` 只有大于 0 时才会更新。
- 更新后会重新校验关联资源和目标。

仅启停任务：

```json
{"enabled":false}
```

### 7.5 删除任务

```http
DELETE /api/v1/jobs/{id}
```

```json
{"ok":true}
```

删除任务时会在同一数据库事务中一并硬删除任务运行态和该任务的全部历史执行记录。

### 7.6 立即执行任务

```http
POST /api/v1/jobs/{id}/run
```

不需要请求体。执行会真实读取并修改 DNS，并写入一条 `Run`。

成功或无变化时返回 `200` 和 `Run`：

```json
{
  "id": 101,
  "jobId": "j-abc",
  "startedAt": "2026-10-10T08:30:00Z",
  "finishedAt": "2026-10-10T08:30:01Z",
  "durationMs": 620,
  "status": "success",
  "oldIp": "203.0.113.9",
  "newIp": "203.0.113.10",
  "trigger": "manual",
  "message": "updated 1 record(s)",
  "records": 1
}
```

若任务存在且执行失败，仍返回 `200` 和 `status: "failed"` 的 `Run`。只有执行前无法
取得任务等未产生有效任务上下文的错误才返回 `4xx`。

### 7.7 预演任务

```http
POST /api/v1/jobs/{id}/dry-run
```

读取 IP 来源和现有 DNS，但不写入：

```json
{
  "targets": [
    {
      "domain": "home.example.com",
      "type": "A",
      "current": "203.0.113.9",
      "desired": "203.0.113.10",
      "action": "update"
    }
  ]
}
```

`action` 枚举：

| 值 | 说明 |
|---|---|
| `create` | 当前记录不存在，将创建 |
| `update` | 地址或 Cloudflare `proxied` 状态不同，将更新 |
| `none` | 已符合目标状态 |
| `skip` | 无法取得目标地址，跳过 |

### 7.8 执行全部启用任务

```http
POST /api/v1/jobs/run-all
```

不需要请求体。所有启用任务在当前请求中顺序执行：

```json
{
  "total": 2,
  "success": 1,
  "noChange": 0,
  "failed": 1,
  "skipped": 0,
  "results": [
    {
      "jobId": "j-1",
      "name": "Home DNS",
      "status": "success",
      "message": "updated 1 record(s)"
    },
    {
      "jobId": "j-2",
      "name": "Office DNS",
      "status": "failed",
      "message": "provider request failed"
    }
  ]
}
```

单个任务失败不会中断后续任务，也不会使接口返回非 200。调用方必须检查 `failed`
和每项 `results[].status`。

## 8. 执行记录接口

### 8.1 查询执行记录

```http
GET /api/v1/runs?job={jobId}&status={status}&limit={limit}
```

查询参数：

| 参数 | 必填 | 说明 |
|---|---|---|
| `job` | 否 | 按任务 ID 精确过滤 |
| `status` | 否 | `success`、`nochange`、`failed`、`skipped` |
| `limit` | 否 | 返回数量；缺失、非数字或小于等于 0 时默认 200 |

响应：按 `startedAt` 倒序排列的 `Run[]`。

```bash
curl -u "$OZDDNS_USER:$OZDDNS_PASSWORD" \
  "$OZDDNS_URL/api/v1/runs?job=j-abc&status=failed&limit=20"
```

### 8.2 获取执行记录详情

```http
GET /api/v1/runs/{id}
```

`id` 必须为十进制整数。格式错误返回：

```json
{"error":"invalid run id"}
```

成功返回 `Run`；不存在返回 `404`。

### 8.3 清理执行历史

```http
POST /api/v1/runs/purge
```

立即删除早于当前 `runRetentionDays` 保留窗口的记录：

```json
{"ok":true,"deleted":12,"keptDays":30}
```

系统启动约 30 秒后也会自动执行一次，之后每小时执行一次。

## 9. 系统设置接口

### 9.1 获取设置

```http
GET /api/v1/settings
```

返回 `Settings`。密码哈希永不返回，只通过 `passwordSet` 表示是否已设置密码。

### 9.2 更新设置

```http
PATCH /api/v1/settings
```

所有字段可选：

```json
{
  "language": "zh-CN",
  "theme": "dark",
  "timezone": "Asia/Shanghai",
  "defaultInterval": 5,
  "username": "admin",
  "password": "new-password",
  "allowWan": false,
  "tls": false,
  "listen": "127.0.0.1:9876",
  "schedulerPaused": false,
  "schedulerTickSeconds": 5,
  "schedulerWorkers": 4,
  "runOnStart": true,
  "runRetentionDays": 30,
  "notifications": []
}
```

注意：

- `defaultInterval`、`schedulerTickSeconds`、`schedulerWorkers` 只有大于 0 时生效。
- `runRetentionDays` 必须在 1～365 之间，否则返回 `400`。
- 调度设置保存后立即应用，无需重启。
- `password` 非空时会更新密码；此接口不能用空字符串清除密码。
- `passwordSet` 是响应字段，提交不会直接改变密码状态。
- `listen`、`allowWan`、`tls` 会持久化；进程监听方式是否立即变化取决于启动层，
  此 PATCH 本身不会重启 HTTP Server。

### 9.3 设置或清除密码

```http
POST /api/v1/settings/password
```

设置密码：

```json
{"username":"admin","password":"new-password"}
```

清除密码：

```json
{"username":"admin","password":""}
```

成功：

```json
{"ok":true}
```

设置成功后，后续请求立即需要新的 Basic Auth。`username` 为空时保留当前用户名。

### 9.4 测试通知

```http
POST /api/v1/settings/test-notify
```

请求：

```json
{"id":"n-main"}
```

- 提供 `id`：测试指定渠道。
- `id` 为空或不提供：测试通知列表中的第一个渠道。
- 后端向 `target` 发出 `POST application/json`，超时约 8 秒。

成功：

```json
{"ok":true}
```

业务失败仍返回 `200`：

```json
{"ok":false,"error":"notification channel \"n-main\" not found"}
```

测试通知发送的 JSON 同时包含以下字段，以兼容常见 Webhook：

```json
{
  "text": "OzDDNS test\nIf you can read this, the channel works.",
  "content": "OzDDNS test\nIf you can read this, the channel works.",
  "title": "OzDDNS test",
  "message": "If you can read this, the channel works."
}
```

### 9.5 导出配置

```http
GET /api/v1/config/export
```

返回 `ConfigBundle` JSON，并设置下载文件名。导出包含服务商凭据以保证可以恢复工作实例，
但不包含面板密码哈希。导出文件应作为敏感数据保存。

### 9.6 导入配置

```http
POST /api/v1/config/import
Content-Type: application/json
```

请求体是 `/config/export` 返回的 `ConfigBundle`。导入采用完整替换语义：现有服务商、
IP 来源、任务和设置会被替换，执行历史保留，当前面板密码也会保留。

成功响应：

```json
{"providers":2,"ipSources":3,"jobs":4}
```

## 10. 服务商配置字段

也可通过 `GET /provider-types` 动态获取。以下是当前编译进后端的 9 种类型。

### 10.1 Cloudflare

类型：`cloudflare`

| 配置键 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `api_token` | secret | 条件必填 | - | 推荐，需 Zone.DNS Edit 权限 |
| `email` | string | 条件必填 | - | 使用 Global API Key 时提供 |
| `api_key` | secret | 条件必填 | - | 与 `email` 配套 |
| `zone_id` | string | 否 | 自动发现 | 指定 Zone ID |
| `proxied` | boolean | 否 | `false` | 服务商级默认代理标记 |
| `ttl` | integer | 否 | `300` | 服务商级默认 TTL |

认证方式二选一：单独提供 `api_token`，或同时提供 `email` + `api_key`。
任务目标上的 `proxied` 是当前写入逻辑采用的值；服务商配置中的 `proxied` 当前仅作为
元数据字段保存。

### 10.2 Alibaba Cloud DNS

类型：`alidns`

| 配置键 | 类型 | 必填 | 默认值 |
|---|---|---|---|
| `access_key_id` | string | 是 | - |
| `access_key_secret` | secret | 是 | - |
| `region` | select | 否 | `cn-hangzhou` |

`region` 可选值：`cn-hangzhou`、`cn-beijing`、`ap-southeast-1`。

### 10.3 DNSPod

类型：`dnspod`

| 配置键 | 类型 | 必填 | 默认值 |
|---|---|---|---|
| `login_token_id` | string | 是 | - |
| `login_token` | secret | 是 | - |
| `record_line` | string | 否 | `默认` |

### 10.4 DuckDNS

类型：`duckdns`

| 配置键 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `token` | secret | 是 | DuckDNS Token |
| `subdomain` | string | 是 | `.duckdns.org` 前的名称 |

DuckDNS 没有读取记录接口，后端无法比较现有值，执行时会按写入处理。

### 10.5 Namecheap

类型：`namecheap`

| 配置键 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `password` | secret | 是 | Advanced DNS 中的 DDNS Password |
| `domain` | string | 是 | 根域名，如 `example.com` |

Namecheap DDNS 适配器为只写且仅处理 IPv4；AAAA 变更不会写入。

### 10.6 GoDaddy

类型：`godaddy`

| 配置键 | 类型 | 必填 |
|---|---|---|
| `key` | secret | 是 |
| `secret` | secret | 是 |

### 10.7 deSEC

类型：`desec`

| 配置键 | 类型 | 必填 |
|---|---|---|
| `token` | secret | 是 |

### 10.8 Vercel DNS

类型：`vercel`

| 配置键 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `token` | secret | 是 | Vercel Access Token |
| `team_id` | string | 否 | 团队作用域 ID |

### 10.9 Custom Webhook

类型：`custom`

| 配置键 | 类型 | 必填 | 默认值 |
|---|---|---|---|
| `url` | string | 是 | - |
| `method` | select | 否 | `GET` |
| `body` | textarea | 否 | `{"domain":"{{domain}}","ip":"{{ip}}"}` |
| `auth` | secret | 否 | - |

`url` 和 `body` 支持 `{{ip}}`、`{{domain}}`、`{{type}}` 占位符。`method` 可选
`GET`、`POST`、`PUT`；`auth` 会原样写入 `Authorization` 请求头。该适配器为只写，
因此无法比较现有 DNS 记录。

## 11. 推荐对接流程

### 11.1 初始化面板

1. 调用 `GET /bootstrap`。
2. 若返回 `401`，显示登录界面并收集 Basic Auth 凭据。
3. 登录后重新调用 `GET /bootstrap`。
4. 使用 `providerTypes` 和 `ipSourceTypes` 动态渲染创建表单。

### 11.2 创建同步任务

1. `POST /providers` 创建服务商。
2. 检查返回对象的 `status`，必要时调用 `/providers/{id}/validate`。
3. `POST /ip-sources` 创建来源。
4. 调用 `/ip-sources/{id}/test`，检查响应体中的 `ok`。
5. `POST /jobs` 创建任务。
6. 调用 `/jobs/{id}/dry-run` 审核计划变更。
7. 调用 `/jobs/{id}/run` 执行，并检查 `Run.status`。

### 11.3 JavaScript 请求封装

```js
const baseURL = "http://127.0.0.1:9876/api/v1";
const auth = btoa(`${username}:${password}`);

async function request(path, options = {}) {
  const response = await fetch(`${baseURL}${path}`, {
    ...options,
    headers: {
      Accept: "application/json",
      Authorization: `Basic ${auth}`,
      ...(options.body ? {"Content-Type": "application/json"} : {}),
      ...options.headers,
    },
  });

  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(body?.error || `HTTP ${response.status}`);
  }
  return body;
}

const result = await request("/ip-sources/src-abc/test", {
  method: "POST",
});
if (!result.ok) {
  throw new Error(result.error);
}
```

## 12. 对接注意事项

- 不要记录或二次展示创建、更新请求中的明文凭据。
- 不要把读取接口返回的 `••••xxxx` 当成真实密钥另存；更新时可省略密钥字段。
- 对 `validate`、`test`、`test-notify` 检查 `ok`，对 `run` 检查 `status`，对
  `run-all` 检查 `failed`。
- 删除服务商或 IP 来源前，应先处理引用它们的任务。
- 改变已被任务引用的 IP 来源地址族会被拒绝；应新建来源并更新任务。
- `GET /ip` 和 `GET /bootstrap` 会访问外部 IP 探测地址，不适合当作高频健康检查；
  健康检查优先使用 `GET /status`。
- `POST /jobs/run-all` 是同步、顺序执行，任务多或上游较慢时请求会持续较久。
- 当前 API 没有分页游标；执行记录仅支持 `job`、`status` 和 `limit` 过滤。
- 当前 API 未配置 CORS，Web 客户端通常应与 OzDDNS 同源部署。
- 接口没有幂等键。网络超时后重试创建请求可能产生重复资源，客户端应先查询确认。
