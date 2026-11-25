# OSFM 技术选型调研（tech-stack）

> 调研日期：2026-11-25。版本号与配额均以当日可查到的公开资料为准；无法从一手资料核实的内容标注「未核实」，基于推算的数字标注「估算」。

## 0. 结论摘要

| 层 | 推荐默认 | 备选 / 回退 |
|---|---|---|
| 地图渲染 | **MapLibre GL JS v6**（矢量瓦片，内置 GeoJSON 聚合） | Leaflet 2.0（仍是 beta）/ 1.9.4，适合纯栅格回退 |
| 底图（默认） | **实例自托管 Protomaps PMTiles 区域切片**（与应用同域，经 HTTP Range 读取） | 欧洲用户在线备选：OpenFreeMap（免 key）；VersaTiles |
| 底图（中国回退） | 自托管 PMTiles（同上，最稳）；可选**天地图**栅格图层（CGCS2000≈WGS-84，无需纠偏，需 tk） | 高德瓦片仅作“可选叠加”，须对标注做 GCJ-02 转换，且授权存疑 |
| 搜索 / 地理编码 | **手动拖钉为主**；Photon（公共实例）做名称搜索，服务器端代理与缓存 | Nominatim 公共实例（1 req/s、禁自动补全）；Overpass 查附近 POI 用于“吸附” |
| 后端 | **PocketBase 作为 Go 框架嵌入**，`go:embed` 前端，产出单一二进制 / 单一 Docker 镜像（arm64 + amd64） | Node 22 + Hono + better-sqlite3 + Drizzle |
| 存储 | SQLite（WAL）+ 普通 bbox 索引查询，数据量上万后再加 R*Tree | SpatiaLite 不需要 |
| 认证 | **邀请码 + 邮件 OTP**，邀请链可追溯；passkey 作为增强 | GitHub OAuth（可选）；X/Telegram 仅作“展示用外链”，不做登录 |
| 客户端 | **PWA**（vite-plugin-pwa，离线缓存已浏览区域与本圈数据） | 微信小程序不建议（个人主体无 web-view，需备案、腾讯地图、GCJ-02） |
| 联邦预留 | 稳定 UUID + `updated_at`/软删除 + 每条可见性 + 实例密钥签名的 GeoJSON Feed | 后期再评估 ActivityPub |
| 部署 | 单容器 + Caddy 自动 HTTPS；Litestream 0.5.x 增量备份到 S3 兼容存储 | 家宽在国内常封 80/443：用高位端口 + DNS-01 证书、IPv6 或境内 VPS 反代 |

**几处意外发现**：(1) X API 自 2026-02 起新开发者基本没有免费档，验证 X 账号需按次付费（用户读取约 $0.010/次）；(2) Leaflet 2.0 到今天仍是 beta（v2.0.0-beta.5，11 月 19 日）；(3) MapLibre 已进入 v6（v6.19.0，11 月 17 日），只发布 ES 模块；(4) jsDelivr 在大陆的可达性存疑，所有前端资源、字体、sprite 必须自托管；(5) 高德 2026-07 推出 6 位“位置口令”，可以作为新的导入入口（未核实）。

---

## 1. 地图渲染客户端

| 维度 | MapLibre GL JS | Leaflet | OpenLayers |
|---|---|---|---|
| 当前版本 | v6.19.0（2026-11-17），v5.24.1 仍在维护 [releases](https://github.com/maplibre/maplibre-gl-js/releases)；v6 只发布 ESM [迁移指南](https://maplibre.org/maplibre-gl-js/docs/guides/v5-to-v6-migration-guide/) | 稳定版 1.9.4；2.0.0-beta.5（2026-11-19），尚无正式版 [releases](https://github.com/Leaflet/Leaflet/releases) | 未专门核实版本 |
| 体积（gzip） | 约 210–270 KB（第三方统计 [depscope](https://mcp.depscope.dev/pkg/npm/maplibre-gl)，「未核实」） | 1.x 约 38 KB；2.0 地图+瓦片图层 25.6 KB（官方 release notes） | 支持 tree-shake，常见打包 150 KB 以上（「未核实」） |
| 矢量瓦片 | 原生支持，WebGL 渲染，可在客户端换样式/语言 | 需插件（如 protomaps-leaflet），性能一般 | 原生支持，但样式体系与 MapLibre 不同 |
| 移动端性能 | GPU 渲染，缩放、旋转流畅；需要 WebGL | DOM/Canvas 渲染，标记数量多时会变慢 | 不错，但 API 较重 |
| 标记 / 聚合 | GeoJSON source 设 `cluster: true` 即可（内部用 supercluster），可用 HTML Marker 做自定义样式 | Leaflet.markercluster 成熟 | 内置 Cluster source |
| 离线 | 无内置离线；可用 PMTiles 文件加 Service Worker 实现 | 栅格瓦片缓存简单 | 同左 |
| CJK 文字 | `localIdeographFontFamily` 默认 `'sans-serif'`，中日韩字形用本机字体渲染，不需要下载巨大的 CJK glyph PBF [MapOptions](https://maplibre.org/maplibre-gl-js/docs/API/type-aliases/MapOptions/) | 栅格瓦片自带文字 | — |

**推荐：MapLibre GL JS（v6）**。主要理由有三点。第一，自托管 PMTiles 矢量底图需要它。第二，同一份数据可以在客户端切换中文、英文、德文标注。第三，中文字形默认用本机字体渲染，对国内移动网络很友好。体积比 Leaflet 大一个数量级，不过 PWA 首次加载后会缓存。

**风险**：v6 刚发布不久，部分插件可能还没跟上，必要时先锁定 v5.24.x。极少数老旧安卓 WebView 的 WebGL 不稳定，可以保留一个 Leaflet + 栅格的降级模式（低优先级）。

---

## 2. 底图瓦片（关键）

### 2.1 OSM 官方瓦片 tile.openstreetmap.org

依据 [OSMF Tile Usage Policy](https://operations.osmfoundation.org/policies/tiles/)：
- 可以在应用中使用，但必须满足以下条件：HTTPS、可见的署名、**应用专属且稳定的 User-Agent**、Web 端带有效的 Referer，并遵守缓存头（无法读取时至少缓存 7 天）。
- **明确禁止**：批量下载或预取、**离线使用（“下载离线地图”功能不允许）**、库默认 UA。
- 没有数字配额，属于 best-effort。**可以随时无通知封禁**，商业或募捐类服务更要预期被撤销访问。
- 对 OSFM 的影响：每个圈子自建实例，流量很小，原则上合规；但“旅行时离线”这个核心场景与政策直接冲突。另外 2022-10 起 openstreetmap.org 域名在大陆遭 DNS 污染，当时有报告称瓦片子域未受影响 [OSM 日记](https://openstreetmap.org/user/booktiger/diary/400233)，近两年的可达性「未核实」。

**结论**：只作为“开发期/临时”选项，不作默认。

### 2.2 替代方案对比

| 方案 | 形态 | 费用/限额 | 自托管体积 | 大陆可达性 |
|---|---|---|---|---|
| **Protomaps / PMTiles** | 单文件矢量瓦片，静态 HTTP Range 读取，`pmtiles extract` 按 bbox/多边形和 `--maxzoom` 裁剪 [docs](https://docs.protomaps.com/basemaps/downloads) | 开源免费；官方不鼓励直链 build 文件 | 全球 z0–15：**139.7 GB**（2026-11-24 build，v4.15.2，见 [builds.json](https://build-metadata.protomaps.dev/builds.json)）；柏林全级别约 84 MB [示例](https://til.simonwillison.net/gis/pmtiles)；每加一级约翻倍 | 取决于部署位置；**与实例同机部署时可达性最好** |
| **OpenFreeMap** | 公共矢量瓦片（OpenMapTiles schema），提供 Positron/Liberty 等样式 [官网](https://openfreemap.org/) | 免费、无 key、无请求上限；离线下载限 3000 瓦片/50 MB | 自托管需约 200 GB 磁盘（全球），且只有全球包 [self_hosting](https://github.com/hyperknot/openfreemap/blob/main/docs/self_hosting.md) | 「未核实」（服务器在境外） |
| **VersaTiles** | 开源栈（Rust 服务器），Shortbread schema [docs](https://docs.versatiles.org/) | tiles.versatiles.org 免费但无保证 | 全球 OSM 矢量 62.1 GB（20261027），下载页可选 bbox，支持导出 .pmtiles [download](https://download.versatiles.org/) | 「未核实」 |
| MapTiler Cloud | 托管 | 免费档 5,000 sessions/月，非商业；Flex $30/月 [pricing](https://www.maptiler.com/cloud/pricing/) | — | 「未核实」 |
| Stadia Maps | 托管 | 免费 200,000 credits/月（1 瓦片=1 credit），**免费档不可商用**；Starter $20/月 [pricing](https://stadiamaps.com/pricing/) | — | 「未核实」 |
| Thunderforest | 托管栅格 | Hobby 150,000 瓦片/月；下一档 $125/月 [pricing](https://www.thunderforest.com/pricing/) | — | 「未核实」 |
| **天地图** | WMTS 栅格（vec/img/cva，_w 为球面墨卡托） | 需注册取 `tk`；个人额度说法不一：每 key 每类瓦片 1 万次/日、每账号最多 5 个 key [CSDN](https://blog.csdn.net/qq_40772640/article/details/130920905)，[知乎](https://zhuanlan.zhihu.com/p/720368932)（「未核实」，以控制台为准） | 不可自托管 | 国内服务，可达；有文章提到接口不稳定 |
| 高德瓦片 | `wprd0{1-4}.is.autonavi.com/appmaptile?...` 非公开文档地址 [segmentfault](https://segmentfault.com/a/1190000042678520) | 无公开授权条款允许第三方直接调用，商用风险高（「未核实」） | — | 国内很快 |

**天地图坐标**：天地图使用 CGCS2000，与 WGS-84 的差异在厘米级，在线切片没有加偏，可直接叠加 WGS-84 数据 [阿里云文章](https://developer.aliyun.com/article/1579198)（二手资料，但与大地测量常识一致）。2025-04 天地图改版上线 [新华网](https://www.news.cn/politics/20250422/48cecb53ffcd4e65a2e544482446f6a0/c.html)。天地图的缺点是城市 POI 和小巷细节不如商业图。

**高德/腾讯（GCJ-02）**：如果叠加高德瓦片，需要把 WGS-84 的标注实时转换为 GCJ-02 再绘制（见第 7 节），或者反过来把瓦片平移；授权问题无法解决，只能作为“用户自行开启的可选图层”。

### 2.3 推荐

- **默认**：每个实例自带 PMTiles 文件，与应用同域提供（PocketBase/Go 静态文件服务即支持 Range；也可交给 Caddy）。建议内容：一个低缩放全球底图（z0–6，「估算」数十 MB，用 `pmtiles extract --maxzoom=6 --dry-run` 确认），再加上圈子常去城市的全级别切片（单城约 50–200 MB，「估算」，参照柏林 84 MB）。中国全境 z0–15 的体积未能实测（「未核实」，估算为数 GB 量级），机顶盒 8 GB 内存、外接存储完全放得下。
- **字体与 sprite**：Protomaps 默认从 GitHub Pages 拉取 [docs](https://docs.protomaps.com/basemaps/maplibre)，必须下载 basemaps-assets 自托管，因为 github.io 在大陆不稳定。CJK 交给 `localIdeographFontFamily`，只需要托管拉丁字形。
- **在线回退**：欧洲用户可选 OpenFreeMap；国内用户可选天地图栅格（实例管理员填写自己的 tk）。
- **离线**：PWA 用 Service Worker 缓存 PMTiles 的 Range 响应需要自定义逻辑（Cache API 对 206 响应支持不好）。更简单的做法是服务器把 PMTiles 解包为 `/tiles/{z}/{x}/{y}.mvt` 普通 URL，这样 SW 可以按 URL 缓存（「未核实」具体实现难度，需原型验证）。

**风险/开放问题**：在中国境内公开展示地图需要审图号，OSM 数据的边界表达不符合国内要求。一个圈子内部、邀请制的自建实例风险较低，但如果公开运营则存在合规问题（[gcoord README](https://github.com/hujiulong/gcoord) 中也有相关法律提示）。另外，OSM 在中国小城镇的数据非常稀疏 [2025 乡镇完备度报告](https://www.openstreetmap.org/user/Higashimado/diary/407990)，大城市较好；底图上看不到店铺是常态，这也是 OSFM 存在的理由。

---

## 3. 地理编码 / POI 搜索

| 服务 | 公共实例政策 | 自托管成本（小区域） | 中文效果 |
|---|---|---|---|
| **Nominatim** | 最多 **1 req/s**（按应用合计）；**禁止自动补全**；必须自定义 UA/Referer；鼓励缓存 [policy](https://operations.osmfoundation.org/policies/nominatim/) | PostgreSQL + 导入，国家级需数十 GB 磁盘（「未核实」具体数） | 结构化地址弱，中国乡镇层级缺失较多（见上述报告） |
| **Photon**（komoot） | photon.komoot.io 允许“合理用量”，无 SLA，可能限流 [README](https://github.com/komoot/photon) | 1.0.0（2026-02）改用 OpenSearch，Java 21+ [发布](https://nominatim.org/2026/02/11/photon-1.0-released.html)；全球约 95 GB、推荐 64 GB RAM；已有按大洲/国家的导出 [exports](https://nominatim.org/2025/08/13/photon-exports-renewed.html)，JSON 导出含 `zh` | 支持 search-as-you-type，有中文名 |
| **Overpass API** | 约 10,000 次/日、1 GB/日；定期调用的应用应再除以 100 [wiki](https://wiki.openstreetmap.org/wiki/Overpass_API) | 自托管很重，不建议 | 能查附近 `amenity=restaurant` 等，取决于 OSM 是否已有数据 |

**推荐**：
1. **手动拖钉为主要录入方式**：打开地图，定位到当前 GPS 或搜索到的大致位置，再拖动准星。国内 OSM POI 稀疏，不能依赖地理编码。
2. 搜索框接 Photon 公共实例，由**服务器端代理**（统一 UA、缓存、节流），失败时回退 Nominatim（防抖，不做逐字补全）。不在机顶盒上自托管 Photon：64 GB RAM 的推荐配置远超硬件能力；国家级导出的内存需求「未核实」。
3. **“吸附”到附近 POI**：落点后用 Overpass 查 50 m 内的有名 POI（结果缓存），作为“参照物”候选。同一实例内已有的推荐也作为参照物候选（这类数据往往比 OSM 更全）。

**相对位置的数据模型（建议）**：
- `lat, lon`（WGS-84，必填）和 `precision`（`exact` / `approx_50m` / `area`）；
- `landmark_ref`：`{type: "osm"|"entry"|"text", osm_type, osm_id, entry_uuid, name}`；
- `relation`：枚举（对面 / 旁边 / 楼上 / 地下 / 巷口 / 小区底商 / 院内……）加 `directions_text` 自由文本（楼层、入口、招牌颜色）；
- `source_coord`：原始输入坐标、坐标系（wgs84/gcj02/bd09）和来源链接，便于以后重新换算和审计。

---

## 4. 后端与存储

| 方案 | 交付形态 | 内存占用 | 开发量 | 评价 |
|---|---|---|---|---|
| **PocketBase**（Go + SQLite） | 单二进制，自带管理后台、REST、实时订阅、文件上传、邮件 | 很低（「未核实」具体值，FAQ 称 $4 VPS 可承载 1 万+ 实时连接 [FAQ](https://pocketbase.io/faq/)） | 最低 | 可作为 Go/JS 框架扩展；**仍是 0.x**（v0.40.6，2026-10-23），v0.22 线并行维护；release 将迁出 GitHub [releases](https://github.com/pocketbase/pocketbase/releases)；只能纵向扩展 |
| Go 自写（chi/echo + modernc sqlite） | 单二进制，静态编译，跨架构简单 | 最低 | 中 | 长期可控，但认证、邮件、后台都要自己写 |
| Node/TS（Hono 或 Fastify + better-sqlite3 + Drizzle） | Docker 镜像（node:alpine 基础镜像约 50 MB+，「估算」） | 约 50–100 MB（「估算」） | 中 | 生态最熟，前后端同语言；better-sqlite3 是原生模块，多架构构建稍麻烦 |
| Python（FastAPI/Django） | Docker | 较高 | 中 | Django admin 好用，但镜像较大 |
| PHP | 传统 LAMP | 中 | 中 | 部署门槛反而高（需 php-fpm + web server） |
| 全栈框架（SvelteKit/Next/Nuxt/Remix） | Node 运行时 | Next 偏重 | 中 | SvelteKit（adapter-static）适合做前端；Next.js 对本场景过重 |

**地理查询**：几千到几万个点时，`WHERE lat BETWEEN ? AND ? AND lon BETWEEN ? AND ?` 加复合索引已经足够（毫秒级）。SQLite 内置的 [R*Tree](https://sqlite.org/rtree.html) 是数据增长后的升级路径，多数发行版的 SQLite 默认编译了 R*Tree，modernc 纯 Go 版是否包含需确认（「未核实」）。SpatiaLite 需要加载扩展，空间索引是独立表，查询必须显式引用 [GDAL ticket](https://trac.osgeo.org/gdal/ticket/4212)，对本项目没有必要。需要注意跨 180° 经线的 bbox 要拆成两段。

**迁移**：PocketBase 自带 migrations（Go 或 JS 文件）；Node 方案用 Drizzle Kit。

**推荐**：**PocketBase 作为 Go 库使用**（而不是直接用官方二进制）。原因有四：
1. 在 Go 中写自定义路由（邀请、导入解析、Feed 导出、瓦片服务）；
2. 用 `go:embed` 打包前端 SPA 和样式资源，一个二进制在 linux/arm64 与 linux/amd64 上交叉编译即可；
3. 认证、邮件 OTP、管理后台、文件上传都现成；
4. 机顶盒级硬件也能跑。

**风险**：0.x 阶段可能有破坏性变更，需锁定版本，升级时读 changelog；项目由单人维护。如果团队更熟 TS，可选 Node + Hono 方案，功能对等但开发量更大。

---

## 5. 邀请制圈子的认证

| 方式 | 适用 | 中国用户可用性 | 备注 |
|---|---|---|---|
| **邀请码/邀请链接** | 必选 | 无障碍 | 单次有效、带过期时间、记录邀请人，形成信任链，便于连带撤销 |
| **邮件 OTP / magic link** | 推荐作为主登录 | QQ/163 邮箱可收，但自建实例的发信信誉是问题（「未核实」） | PocketBase 原生支持 OTP，并可组合 MFA [docs](https://pocketbase.io/docs/authentication/)；SMTP 可用 Resend/SES/国内邮件推送 |
| **Passkey / WebAuthn** | 增强 | iOS 正常；国产安卓无 GMS 时凭据管理器的支持情况「未核实」（Android 官方方案依赖 Google 密码管理器 [Android 文档](https://developer.android.com/training/sign-in/passkeys?hl=zh-cn)） | PocketBase 文档未提及 passkey，需要用 Go WebAuthn 库自行接入 |
| GitHub OAuth | 可选 | github.com 在大陆时通时断（「未核实」） | PocketBase 内置 |
| Telegram Login | 不建议作为主登录 | **Telegram 在大陆被封** [TechCrunch](https://techcrunch.com/?p=2604832) | 新版为 OIDC，需建 bot [docs](https://core.telegram.org/bots/telegram-login) |
| X(Twitter) OAuth | 不建议 | 大陆被封 | X API 现为按量付费；新开发者自 2026-02 起无免费档（第三方 [postproxy](https://www.postproxy.dev/blog/x-api-pricing-2026/)）；用户读取 $0.010/次 [官方定价](https://docs.x.com/x-api/getting-started/pricing) |

**关于“附带 X ID”的低成本验证**：
- X 不渲染 `rel=me`，普通 HTTP 抓取个人页也拿不到 HTML，IndieAuth 早已因此停用 Twitter 验证 [说明](https://aaronparecki.com/2018/05/27/10/indieauth-twitter)。
- 最便宜的可行方案是**不验证、只展示**（自报 handle，由邀请人担保）。或者“在 bio 或推文中贴验证码，再由管理员人工核对”。
- 自动验证需要付费 API。每次约 $0.01，量很小，但需要绑卡并充值。

**推荐**：邀请码注册 + 邮件 OTP 登录，作为 MVP。Passkey 放在 P1。外部身份（X/GitHub/小红书等）作为“自报的签名链接”字段，不承担认证职责。

---

## 6. 客户端形态

| 形态 | 优点 | 限制 |
|---|---|---|
| **PWA** | 一套代码，免审核，可安装；Service Worker 离线 | iOS 只能经“分享 → 添加到主屏幕”手动安装；Safari 与主屏 App 的存储互相隔离；长期不用的 Safari 站点数据可能被清除，添加到主屏可缓解；Web Push 需 iOS 16.4+ 且已添加到主屏 [汇总](https://www.magicbell.com/blog/pwa-ios-limitations-safari-support-complete-guide)。WebKit 官方存储策略见 [WebKit blog](https://webkit.org/blog/14403/updates-to-storage-policy/)（本次未能直接访问，配额细节「未核实」） |
| 原生 App | 离线和定位最强 | 双平台开发与分发成本高；国内安卓需进各家应用商店，且需 [App 备案](https://www.gov.cn/zhengce/zhengceku/202308/content_6897341.htm) |
| 微信小程序 | 群聊里分享即用 | 自 2023-09-01 起须**小程序备案**后才能上架 [澎湃](https://www.thepaper.cn/newsDetail_forward_24155262)；**个人与海外主体不能用 web-view** [Taro 文档](https://docs.taro.zone/docs/components/open/web-view)，必须用原生 `map` 组件（腾讯地图），定位与 `openLocation` 使用 GCJ-02 [wx.getLocation](https://developers.weixin.qq.com/miniprogram/dev/api/location/wx.getLocation.html)；不能用 OSM 底图（「未核实」是否允许自定义瓦片）；后端域名需要 HTTPS 并在后台配置 |
| Telegram Mini App | 海外圈子方便 | 大陆被封；中国用户不可用 |

**推荐：PWA**。旅行场景需要的是“已收藏城市的底图和本圈数据离线可读”。PWA 可以把这两部分都缓存下来：数据存 IndexedDB，瓦片存 Cache Storage。在 iOS 上要引导用户“添加到主屏幕”。微信生态与每个圈子自建实例的模式冲突（每个实例都要单独注册小程序并备案），不适合开源自托管。可以后期做一个“分享卡片”页面（普通 H5）来兼容微信内打开。

---

## 7. GCJ-02 / BD-09 处理与链接导入

**库**：
- [gcoord](https://github.com/hujiulong/gcoord)（JS，MIT，gzip 约 3 KB）：支持 WGS84/GCJ02/BD09/BD09MC/WebMercator，可直接转换 GeoJSON。
- Go 端可用 [qichengzx/coordtransform](https://pkg.go.dev/github.com/qichengzx/coordtransform)。
- [PRCoords](https://github.com/Artoria2e5/PRCoords)（公有领域，多语言）。

**精度**：GCJ-02 没有闭式逆变换。常见的一步近似逆变换误差约 1–2 m，迭代或精确版可以小于 0.5 m [eviltransform](https://github.com/googollee/eviltransform)。对美食点来说 1–2 m 足够，建议在服务端使用迭代版。

**何时需要转换**：
1. 叠加高德/腾讯/Google 中国道路图时（显示层转换，存储仍用 WGS-84）；
2. 从高德、腾讯、大众点评、微信位置导入坐标时（GCJ-02 → WGS-84）；
3. 从百度导入时（BD-09 → WGS-84；如果是 BD09MC 墨卡托米制坐标，要先转回经纬度）；
4. 使用天地图或 OSM 时**不需要转换**。
5. 中国境内的 Google Maps 道路图同样有 GCJ-02 偏移（业内常识，「未核实」具体链接中坐标的坐标系），从 Google 链接导入中国境内坐标时需要提示用户确认。

**分享链接解析**（服务器端展开短链，按规则提取，失败则只保存链接，由用户手动落点）：

| 来源 | 形式 | 坐标系 |
|---|---|---|
| 高德 | `uri.amap.com/marker?position=lng,lat&coordinate=gaode` [官方 URI API](https://lbs.amap.com/api/uri-api/guide/mobile-web/point)；`surl.amap.com` 短链需展开（展开后的格式「未核实」） | 默认 GCJ-02（`coordinate=wgs84` 时为 WGS-84） |
| 高德“位置口令” | 2026-07 推出的 6 位码 [凤凰科技](https://tech.ifeng.com/c/8uwEDlr9dJF) | 是否有公开解析接口「未核实」 |
| 百度 | 链接中的 `bd09ll` 或 `bd09mc` 坐标（「未核实」具体参数名） | BD-09 |
| Google Maps | `@lat,lng,zoom`、`?q=lat,lng`；`maps.app.goo.gl` 短链需展开 | 中国境外为 WGS-84 |
| 大众点评 | 只有 shopId，坐标需要抓取页面，有违反服务条款的风险 | 不建议自动解析；保存链接、店名，由用户手动落点 |

---

## 8. 联邦（只预留接口，不实现）

| 方案 | 适配度 | 说明 |
|---|---|---|
| ActivityPub | 中 | BookWyrm 的做法：自定义 `Review` 类型，发给非 BookWyrm 服务时降级为 `Article`/`Note` [BookWyrm 文档](https://docs.joinbookwyrm.com/activitypub.html)；但 Mastodon 对 Article 只显示标题和链接 [issue #29981](https://github.com/mastodon/mastodon/issues/29981)。实现成本高（inbox/outbox、HTTP Signatures、WebFinger），而且“只对熟人可见”与 AP 的公开传播模型冲突 |
| AT Protocol | 低 | 可以自定义 Lexicon（NSID 反向域名）[atproto](https://atproto.com/guides/lexicon)，但数据默认公开存储在 PDS，不适合隐私条目 |
| **签名 GeoJSON Feed（拉模式）** | **高** | 实例 A 给实例 B 发一个订阅 token；B 定期拉取 `GET /federation/feed.geojson?since=…`，响应由 A 的实例密钥（Ed25519）签名。类似 RSS/Atom 的增量订阅，实现最简单，也易于用 HTTP 缓存 |

**MVP 需要预留的“接口”**：
1. 每条推荐使用 **UUIDv7**（全局唯一，可排序），附 `created_at`、`updated_at`，**软删除**（tombstone）；
2. 每条推荐带 `visibility`：`private` / `circle` / `federated` / `public`（MVP 只实现前两种）；
3. 作者使用**实例内稳定 ID 加可选化名**，导出时只输出化名；
4. 实例启动时生成 **Ed25519 实例密钥**，在 `/.well-known/osfm.json` 公布公钥和 schema 版本；
5. 定义 `osfm` 属性命名空间的 **GeoJSON Feature schema**（见第 9 节），同时用于导出、备份和联邦；
6. 不使用自增 ID 作为对外引用。

---

## 9. 导入 / 导出格式

| 格式 | 用途 | 要点 |
|---|---|---|
| **GeoJSON（主格式）** | 导出、备份、联邦 | `properties` 中放 `osfm:` 前缀字段；uMap 导入时会保留**全部 properties** [uMap 导入指南](https://wiki.openstreetmap.org/wiki/UMap/Guide/Import_data_files) |
| KML/KMZ | Google My Maps 导入导出 | uMap 导入 KML 时只保留 name/description；KMZ 是 zip 包 |
| GPX | 户外 App、Organic Maps | 只有 name/desc，导出时把推荐语塞进 desc |
| CSV | 表格批量录入 | uMap 规则：列名以 lat/lon 开头的列作为坐标，默认 WGS-84 |
| Google Takeout | 迁移 Google 收藏 | 已加星地点为 GeoJSON（`Saved Places.json`），未知位置记为 0,0，需要过滤 [Google schema](https://developers.google.com/data-portability/schema-reference/local_actions)；**自定义列表导出为 CSV，只有 Google Maps URL，没有坐标** [Organic Maps 讨论](https://github.com/orgs/organicmaps/discussions/928)，需要展开 URL 或手动落点 |
| uMap `.umap` | 与 uMap 互通 | 包含地图和图层元数据；建议只支持 GeoJSON 互通 |

**推荐**：内部规范格式为 GeoJSON FeatureCollection，同时提供 KML、GPX、CSV 导出。导入支持 GeoJSON、KML/KMZ、CSV、Takeout，每次导入都要显示预览并让用户确认坐标系。

---

## 10. 部署与运维

- **镜像**：Go/PocketBase 方案可以用 `distroless/static` 或 `alpine` 基础镜像，镜像约 30–50 MB（「估算」），通过 `docker buildx` 构建多架构（amd64 + arm64）。PMTiles 文件放在挂载卷里，不打进镜像。
- **HTTPS**：Caddy 自动申请证书。国内家宽通常封入站 80/443 [V2EX 报告](https://global.v2ex.com/t/1121687)，HTTP-01 验证不可用，需改用 **DNS-01** 验证（Caddy 的 DNS 插件，支持阿里云/腾讯云/Cloudflare DNS）加高位端口。
- **NAT 穿透**：

| 方案 | 国内可用性 | 备注 |
|---|---|---|
| IPv6 直连 | 视运营商而定，部分地区 v6 的 80/443 也被封 | 移动网络普遍支持 v6 |
| Cloudflare Tunnel | 能用，但 Cloudflare 自己承认境外节点对国内访问延迟高、丢包多 [Cloudflare China](https://www.cloudflare.com/en-au/network/china/)；`trycloudflare` 是否被封「未核实」 | 更适合欧洲成员 |
| Tailscale / Headscale | 默认 DERP 在境外，国内连接质量不稳；通常需要自建 DERP [Tailscale 文档](https://tailscale.com/kb/1232/derp/)，[中文指南](https://www.ssdnodes.com/learn/lang/zh-hans/does-tailscale-work-in-china) | 适合“只对成员开放”，但需要每人装客户端，不适合旅途中随手打开 |
| frp + 境内 VPS | 最稳 | 境内 VPS 用 80/443 需 ICP 备案；可改用高位端口 |
| 境外 VPS 部署 | 欧洲用户最佳 | 大陆访问慢，GFW 曾在 2025-08-20 短暂封锁全部 TCP 443 [x-cmd](https://www.x-cmd.com/blog/250823) |

- **备份**：[Litestream v0.5](https://fly.io/blog/litestream-v050-is-here/)（2025-10）改了文件格式和配置（`replicas` 改为 `replica`），早期版本有坑 [评测](https://mtlynch.io/notes/hold-off-on-litestream-0.5.0/)，建议用 0.5.7 以上并实测恢复；支持阿里云 OSS 等 S3 兼容存储。更简单的兜底方案：定时 `VACUUM INTO` 或 PocketBase 内置备份，再加 GeoJSON 导出，存到 NAS 或网盘。
- **前端依赖**：所有 JS、CSS、字体、sprite 都要打包进实例，**不使用公共 CDN**。jsDelivr 在大陆被 GreatFire 检测为封锁（2026-03 检测）[GreatFire](https://en.greatfire.org/https/cdn.jsdelivr.net/npm/jquery@3.7.1/dist/jquery.min.js)。

---

## 附：待原型验证的开放问题

1. 用 `pmtiles extract --dry-run` 实测：中国全境、单城市、全球 z0–6 的体积（本次环境未能运行 CLI）。
2. Service Worker 缓存 PMTiles Range 请求，与服务器解包成 z/x/y URL 两种做法的难度比较。
3. 天地图个人 key 的当前日限额，以及 Web 端 `tk` 暴露在前端是否需要域名白名单。
4. 国产安卓（无 GMS）上的 passkey 实测。
5. 高德 `surl` 短链、百度分享链接展开后的具体参数格式。
