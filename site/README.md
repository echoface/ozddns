# OzDDNS 项目主页

`site/` 是 OzDDNS 的静态项目主页：纯 HTML + CSS + JavaScript，无框架、无构建步骤、
无外部请求（字体、图标、脚本全部本地）。可直接部署到 Vercel、Cloudflare Pages
或任何静态文件服务器。

```
site/
├── index.html          页面内容（英文 / 简体中文，同一份 HTML）
├── 404.html            自定义 404
├── assets/
│   ├── style.css       样式与亮/暗主题
│   ├── app.js          语言切换、主题、代码复制、标签页
│   ├── favicon.svg     站点图标
│   └── og.png          社交分享图（1200×630）
├── _headers            Cloudflare Pages 响应头
├── vercel.json         Vercel 响应头与缓存
└── robots.txt
```

## 本地预览

任选一种：

```bash
# Python
python3 -m http.server 8080 --directory site

# Node
npx serve site
```

打开 <http://127.0.0.1:8080>。不要直接双击 `index.html`：本地 `file://` 下
剪贴板与部分浏览器行为会受限。

## 部署到 Vercel

### 方式一：网页导入（推荐）

1. 把仓库推到 GitHub。
2. 在 Vercel 选择 **Add New → Project**，导入 `echoface/ozddns`。
3. 关键设置：
   - **Root Directory**：`site`
   - **Framework Preset**：`Other`
   - **Build Command**：留空
   - **Output Directory**：留空
   - **Install Command**：留空
4. Deploy。之后每次推送都会自动重新部署。

### 方式二：CLI

```bash
cd site
npx vercel --prod
```

首次运行会询问项目名与目录；项目根目录选择当前 `site/` 即可。

### 自定义域名

Vercel 项目 → **Settings → Domains → Add**，按提示在域名注册商处添加记录：

- 根域名：`A 76.76.21.21`（以 Vercel 面板显示的值为准）
- `www`：`CNAME cname.vercel-dns.com`

HTTPS 证书由 Vercel 自动签发与续期。

## 部署到 Cloudflare Pages

### 方式一：连接到 Git

1. Cloudflare Dashboard → **Workers & Pages → Create → Pages → Connect to Git**。
2. 选择仓库，设置：
   - **Framework preset**：`None`
   - **Build command**：留空
   - **Build output directory**：`site`
   - **Root directory**：留空（仓库根）
3. Save and Deploy。

`_headers` 会被自动应用，无需额外配置。

### 方式二：Wrangler 直接上传

```bash
npx wrangler pages deploy site --project-name ozddns
```

首次运行需要登录 Cloudflare 账号；也可以在 CI 中使用
`CLOUDFLARE_API_TOKEN` 与 `CLOUDFLARE_ACCOUNT_ID`。

### 自定义域名

Pages 项目 → **Custom domains → Set up a domain**。若域名已托管在同一 Cloudflare
账号，记录会自动创建；否则按提示添加 `CNAME`。证书自动签发。

> Cloudflare 的静态托管本身没有“年费”。真正产生年费的是域名注册/续费；
> 两家平台的免费额度对项目主页都足够，具体限制以官网当前页面为准。

## 维护说明

- **文案与语言**：所有可翻译文本都写成
  `data-en="..."` / `data-zh="..."`，HTML 中默认保留英文，保证禁用 JavaScript
  时仍可阅读。新增文案时同时补齐两种语言。
- **默认语言**：首次访问按浏览器语言选择，之后记录在 `localStorage` 的
  `oz-lang` 中；右上角按钮可随时切换。
- **主题**：默认跟随系统，手动切换后记录在 `oz-theme`。
- **版本号**：`v2.0.3` 出现在顶栏、hero 徽标、Docker 示例和 `assets/og.png` 中。发布新版本时同时修改这些位置并重新生成分享图。
- **社交分享图**：`og:image` 目前是相对路径 `assets/og.png`。绑定正式域名后，
  建议改成绝对地址（`https://你的域名/assets/og.png`），并补上
  `<meta property="og:url">`。
- **响应头**：`_headers` 只对 Cloudflare Pages 生效，`vercel.json` 只对 Vercel
  生效；修改安全头时两边都要改。
- **不要**把真实的 DNS 凭据、面板截图或未公开的主机名放进页面。

## 设计约束

这个页面是展示层，不是 OzDDNS 管理面板：

- 不承诺尚未实现的能力，不把开放模式描述成可公网暴露的方案；
- 明确默认监听 `127.0.0.1:9876`、TLS 由反向代理终止；
- 许可证状态如实展示（仓库目前没有 license 文件）；
- 没有第三方分析、Cookie 或远程字体请求。
