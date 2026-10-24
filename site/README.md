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
│   ├── favicon.svg     站点图标（SVG）
│   ├── favicon-32x32.png
│   ├── apple-touch-icon.png
│   ├── icon-192.png    PWA / manifest 图标
│   ├── icon-512.png
│   └── og.png          社交分享图（1200×630）
├── llms.txt            面向 AI 搜索/生成式引擎的项目摘要
├── robots.txt          爬虫规则、AI 爬虫放行、Sitemap
├── sitemap.xml         站点地图
├── site.webmanifest    PWA manifest
├── _headers            Cloudflare Pages 响应头
└── vercel.json         Vercel 响应头与缓存
```

> 线上 canonical 地址目前是 <https://ozddns.pages.dev/>。
> 页面、结构化数据和 `llms.txt` 都使用这个绝对地址。

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

## SEO 与 GEO

### 已包含的 SEO 信息

- `<title>`、description、keywords、author、robots、canonical、Open Graph、Twitter Card；
  分享图使用绝对地址，避免社交平台解析失败。
- JSON-LD `@graph`：`WebSite`、`Person`、`SoftwareApplication`、`FAQPage`、`WebPage`。
  其中 FAQ 的问答与页面可见内容保持一致。
- `sitemap.xml`、带 Sitemap 声明的 `robots.txt`、`site.webmanifest`，以及 SVG/PNG/
  Apple Touch 图标。
- 自定义 404 使用 `noindex, follow`，不会被搜索引擎收录。

### GEO 指生成式引擎优化

本项目中的 **GEO** 指 Generative Engine Optimization（生成式引擎优化），不是地理定位：

- `llms.txt` 提供机器可读的项目摘要、能力边界、部署方式、安全模型和文档链接；
- FAQ 使用可见的问答内容，并同步到 `FAQPage` 结构化数据；
- `SoftwareApplication` / `WebSite` / `Person` 结构化数据明确产品、版本、作者和代码仓库；
- `robots.txt` 明确放行主流 AI 爬虫，并指向 Sitemap。

没有添加 `geo.region`、`geo.position` 或 `ICBM`：OzDDNS 不是本地商家，虚构地理位置
会误导搜索引擎。若确实需要地区标记，先确定城市/地区，再同时补到页面和文档中。

如果 Cloudflare 账号启用了 AI Crawl Control 或托管版 `robots.txt`，边缘层可能覆盖
仓库里的规则；需要在 Cloudflare 控制台中放行对应爬虫，GEO 设置才会生效。

### 更换域名时

当前 canonical 为 `https://ozddns.pages.dev/`。绑定自定义域名后，必须同步修改：

1. `index.html`：`canonical`、`og:url`、`og:image`、`twitter:image`，以及 JSON-LD 中
   所有 `https://ozddns.pages.dev/` 地址；
2. `sitemap.xml`：`<loc>`；
3. `robots.txt`：`Sitemap:`；
4. `llms.txt`：Homepage 与 Sitemap 链接；
5. 本文档中的 canonical 说明。

修改内容后，同时更新 `sitemap.xml` 的 `<lastmod>`、JSON-LD 的 `dateModified`、
`og:updated_time` 和页脚 `<time>`。

## 维护说明

- **文案与语言**：所有可翻译文本都写成
  `data-en="..."` / `data-zh="..."`，HTML 中默认保留英文，保证禁用 JavaScript
  时仍可阅读。新增文案时同时补齐两种语言。
- **默认语言**：首次访问按浏览器语言选择，之后记录在 `localStorage` 的
  `oz-lang` 中；右上角按钮可随时切换。
- **主题**：默认跟随系统，手动切换后记录在 `oz-theme`。
- **版本号**：`v2.0.3` 出现在顶栏、hero 徽标、Docker 示例、JSON-LD
  `softwareVersion`、`llms.txt` 和 `assets/og.png` 中。发布新版本时同时修改这些位置
  并重新生成分享图。
- **社交分享图**：`og:image` 与 `twitter:image` 已使用绝对地址
  `https://ozddns.pages.dev/assets/og.png`；换域名时按上一节同步。
- **响应头**：`_headers` 只对 Cloudflare Pages 生效，`vercel.json` 只对 Vercel
  生效；修改安全头或缓存时两边都要改。
- **不要**把真实的 DNS 凭据、面板截图或未公开的主机名放进页面。

## 设计约束

这个页面是展示层，不是 OzDDNS 管理面板：

- 不承诺尚未实现的能力，不把开放模式描述成可公网暴露的方案；
- 明确默认监听 `127.0.0.1:9876`、TLS 由反向代理终止；
- 许可证状态如实展示（仓库目前没有 license 文件）；
- 没有第三方分析、Cookie 或远程字体请求。
