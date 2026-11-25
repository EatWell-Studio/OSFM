# OSFM 开源生态调研（Open-Source Landscape）

> 调研日期：2026-11-25。GitHub 数据（Stars、最近推送时间、License）均通过 GitHub API 于当日读取；GitLab / Codeberg / Framagit 数据通过各自 API 读取。Stars 为近似值。无法直接核实的内容标注「未核实」。
>
> 判读约定：「最近推送」指仓库最近一次 push，不等于正式发版；「无 License」表示仓库没有许可证文件，按著作权法默认**保留所有权利**，不能合法复用代码。

---

## 0. 速览：对团队初始清单的更正

| 团队原描述 | 核实结果 |
|---|---|
| uMap（AGPLv3），「零开发替身，但缺信任链」 | **基本正确**。License 为 AGPL-3.0-or-later（[LICENSE](https://github.com/umap-project/umap/blob/master/LICENSE)）。补充：uMap 3.0 起支持实时协作，2.x 起有「团队」与草稿/私有地图，4.0 alpha（2026-10-29）把渲染引擎从 Leaflet 换成了 OpenLayers（[changelog](https://docs.umap-project.org/en/master/changelog/)）。它有「编辑者名单」，但没有「每条推荐由谁担保」的签名语义。 |
| MapComplete，主题化 OSM 编辑器，写入 OSM 主库 | **正确，但要补两点**：① GitHub 仓库已归档（最后推送 2025-02-28），开发迁到自建 Forgejo [source.mapcomplete.org](https://source.mapcomplete.org/MapComplete/MapComplete)；② License 为 **GPL-3.0-or-later**（见 [package.json](https://github.com/pietervdvn/MapComplete/blob/develop/package.json)）；③ 它**已经集成了 Mangrove Reviews 的匿名评论**（[OSM 论坛讨论](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235)）。也就是说，评论这一层并不写进 OSM 主库。 |
| Foodfolk：个人 GitHub 项目，Node + SQLite，License 不明 | **仓库名其实是 `foodfolk-discovery-map`**：[patelpreet404-alt/foodfolk-discovery-map](https://github.com/patelpreet404-alt/foodfolk-discovery-map)。另有一个描述相同的**空仓库**：[preet23cs494-ui/foodfolk-discovery-map](https://github.com/preet23cs494-ui/foodfolk-discovery-map)。技术栈核实为 Node 26 内置 `node:sqlite`、手写 `node:http` API、React 19 + Vite 8 + Leaflet、sharp 处理图片。**没有 License 文件**，0 star，总共 3 个提交且都在 2026-10-13。**它是开放注册加「好友请求」，不是邀请制**。 |
| better-food-map，「工具型非社区型」 | 实际仓库是 [EvolHeartrise/better-food-map](https://github.com/EvolHeartrise/better-food-map)（线上站 betterfoodmaps.com）。技术栈为 SvelteKit + Cloudflare Workers + D1 + MapLibre，Google 登录，**没有 License**。定位是公开的全球餐馆菜单与饮食禁忌搜索，**「工具型」的判断成立**。 |
| food-map，「工具型非社区型」 | **无法确定指哪个仓库（未核实）**。GitHub 上名为 `food-map` 的仓库有几十个，几乎都是 0 star 的个人作品。按名称和 star 数看，最接近的是 [laurencleek/open_food_map](https://github.com/laurencleek/open_food_map)（MIT，约 62★，抓取 Google Maps API 再用机器学习找「被低估」的店），它确实是工具型。请团队补充原始链接。 |
| 明日方舟「蟑螂地图」 | 指 2026 年 11 月走红的同人站 **「舟P地图 / 舟友地图」**（[zhoupmap.com](https://zhoupmap.com/)），因为玩家在地图上落下的标记是一只「小强」而得名（[搜狐报道](https://www.sohu.com/a/1099798138_122004016)、[网易报道](https://m.163.com/dy/article/LBFILPUN0526VGN7.html)）。**它不是游戏内的地图，也没找到开源代码（未核实是否开源）**。它的 UX 经验见第 6 节。 |

---

## 1. 团队初始参考项目（核实版）

| 项目 | 链接 | License | 技术栈 | 活跃度(最近提交/Stars, 2026) | 与 OSFM 的重合 | 缺口 | 可借鉴/可复用 |
|---|---|---|---|---|---|---|---|
| uMap | [GitHub](https://github.com/umap-project/umap) | AGPL-3.0-or-later | Django + PostgreSQL/PostGIS；前端 Leaflet（4.0 alpha 改为 OpenLayers）；Redis/ASGI 做实时协作 | 2026-11-24 / ~1.6k★；3.8.1（2026-08）、4.0.0a0（2026-10-29） | OSM 底图、自定义标记与字段、团队、私有或仅编辑者可见、可自托管（有 YunoHost 包） | 没有「推荐人签名」，任何编辑者都能改别人的点；可见性按地图或图层设置，做不到单条；没有邀请树；没有联邦 | **零开发过渡方案首选**（见结论 2）。代码为 AGPL，OSFM 若取宽松 License 就不能复制其代码 |
| MapComplete | [Forgejo](https://source.mapcomplete.org/MapComplete/MapComplete) / [GitHub 归档](https://github.com/pietervdvn/MapComplete) | GPL-3.0-or-later | TypeScript + Svelte，无后端，读 Overpass、写 OSM API | GitHub 于 2025-02 归档；Forgejo 需登录查看（活跃度未核实）/ GitHub ~245★ | 主题化 POI 展示、`cuisine`/`diet:*` 问答式补标签、Mangrove 评论 | 数据写进公共 OSM，不能私有；没有熟人圈 | 「问题卡片」式补全字段的 UX；餐饮主题的标签模板可当作 OSFM 字段映射参考 |
| foodfolk-discovery-map | [GitHub](https://github.com/patelpreet404-alt/foodfolk-discovery-map) | **无**（保留所有权利） | Node 26（`node:sqlite`、`node:http`）+ React 19 + Vite + Leaflet；Photon 搜索加 Overpass 附近发现；Docker / Render 部署 | 3 个提交，全在 2026-10-13 / 0★ | **最接近**：好友推荐层、每条评论可设 `public/friends/private`、SQLite 单体、OSM 搜索 | 开放注册、没有邀请或白名单；好友关系是双向请求，没有「担保」；没有「相对位置描述」和「时段」字段；没有联邦；疑似一次性练手项目 | **代码不可复用**（无 License）。可参考它的思路：[schema.sql](https://github.com/patelpreet404-alt/foodfolk-discovery-map/blob/main/server/schema.sql) 里 `reviews.visibility`、`restaurants.source/source_url/normalized` 的去重键；由服务端代理 Photon 请求，不把用户位置暴露给第三方 |
| better-food-map | [GitHub](https://github.com/EvolHeartrise/better-food-map) | **无** | SvelteKit 5 + Cloudflare Workers + D1 + MapLibre；osmium 离线抽取 OSM 餐饮 POI；MCP 服务器维护数据 | 2026-11-02 / 0★（约 100+ 提交，有 iOS/Android 壳） | 用 OSM 做餐饮 POI 底库；饮食过滤与 `diet:*` 对应 | 由运营方集中维护，公开站点、Google 登录，没有社区层，不能自托管 | 「离线 osmium 抽取 OSM 餐饮 POI 到 SQLite 暂存库」的做法可借鉴（只借思路） |
| open_food_map（food-map 的候选，未核实） | [GitHub](https://github.com/laurencleek/open_food_map) | MIT | Python 数据管线 + 静态 HTML 地图 | 2026-02-10 / ~62★ | 城市餐馆地图 | 数据来自 Google Maps API 抓取（有 ToS 风险），模型打分，没有人的担保 | 无实质复用价值 |

---

## 2. 开放评论协议与开放数据

| 项目 | 链接 | License | 技术栈 | 活跃度 | 与 OSFM 的重合 | 缺口 | 可借鉴/可复用 |
|---|---|---|---|---|---|---|---|
| Mangrove Reviews | [GitLab](https://gitlab.com/open-reviews/mangrove) · [数据格式文档](https://docs.mangrove.reviews/) | 代码 Apache-2.0；评论数据 CC BY 4.0（据[论坛](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235?page=2)转述其条款第 8 条，原文未核实） | 服务端栈未核实（GitHub 组织里有 Rust/diesel 相关仓库，推测为 Rust + PostgreSQL） | GitLab 2026-11-19 / ~47★；MapComplete 已集成，OsmAnd 有原型，Organic Maps / CoMaps 有功能请求（[论坛](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235?page=3)） | **评论自带密码学签名**：每条评论是 ES256 签名的 JWT，`kid` 就是公钥；地点主体用 `geo:lat,lng?q=店名&u=半径` URI 表示；支持编辑、删除、举报、等价声明，以及身份声明（DID） | 面向公开世界的匿名评论；签名只证明「同一把钥匙」，不证明「是熟人」；没有私密可见性 | **强烈建议借鉴它的数据模型**：①「推荐 = 签名对象」天然对应「熟人担保」；② `geo:` URI 加名称模糊匹配，很适合「位置常是相对描述」的场景；③ 公开条目可以选择同步到 Mangrove。Apache-2.0 与任何 OSFM License 都兼容 |
| lib.reviews | [GitHub](https://github.com/permacommons/lib.reviews) | 代码 CC0-1.0（GitHub 元数据）；内容许可未核实 | Node（Express 5）+ PostgreSQL | 2026-11-15 / ~203★；2016 年起运营 | **注册需要邀请码**（[README](https://github.com/permacommons/lib.reviews)），在 OSM 论坛上被当作防垃圾的关键手段 | 通用「评论万物」，不是地图；没有圈子私密层 | 邀请码流程、团队（team）概念、公开脱敏数据库转储 |
| OpenStreetMap Notes | [OSM Wiki](https://wiki.openstreetmap.org/wiki/Notes) | 属于 openstreetmap-website（GPL-2.0）；数据 ODbL | Rails | 持续活跃 / ~2.9k★（openstreetmap-website） | 「在地图上落一个带文字的点」 | 完全公开，用于给 OSM 报错，**禁止当作评论或推荐使用** | 只用作 OSFM 的「回馈 OSM」出口：成员发现地图上缺店时，一键生成 Note |
| OSM `cuisine=*` / `diet:*` 标签 | [cuisine](https://wiki.openstreetmap.org/wiki/Key:cuisine) · [diet](https://wiki.openstreetmap.org/wiki/Key:diet:*) | 数据 ODbL | — | — | 菜系、素食/清真等字段 | 只描述客观属性，没有主观推荐 | **直接采用为 OSFM 的受控词表**，以后与 OSM 和 Mangrove 互通都更省事 |
| Wikidata | [wikidata.org](https://www.wikidata.org/) | CC0 | — | — | 连锁品牌（OSM 的 `brand:wikidata`）、菜品条目 | 不收录单店 | 连锁店去重，菜名多语言 |
| OpenTripMap | [API](https://dev.opentripmap.com) | 数据 ODbL（官网声明）；**代码是否开源未核实** | 托管 API | 未核实 | POI 底库（OSM + Wikidata + Wikipedia） | 以景点为主，餐饮薄弱 | 意义不大 |
| cartes.app | [Codeberg](https://codeberg.org/cartes/web)（GitHub [已迁出](https://github.com/cartesapp/cartes)） | AGPL-3.0 | Next.js/TS；评论走 **AT Protocol**（lexicon `app.cartes.comment`，评论 ODbL） | Codeberg 2026-11-24 / ~137★ | OSM 地图上的社交评论，联邦化 | 公开网络，不是熟人圈 | 「评论存放在用户自己的 PDS」这一 ATProto 路线，可作为与 ActivityPub 并列的联邦备选 |

---

## 3. 联邦化 / Fediverse 模型

| 项目 | 链接 | License | 技术栈 | 活跃度 | 与 OSFM 的重合 | 缺口 | 可借鉴/可复用 |
|---|---|---|---|---|---|---|---|
| BookWyrm | [GitHub](https://github.com/bookwyrm-social/bookwyrm) | **Anti-Capitalist Software License v1.4**（[LICENSE.md](https://github.com/bookwyrm-social/bookwyrm/blob/main/LICENSE.md)，**不是 OSI 开源许可**，也不是 AGPL） | Django + PostgreSQL + Celery/Redis | 2026-11-22 / ~2.8k★；v0.9.5 | **最好的联邦模型样板**：一个读书会就是一个实例，各实例自选联邦对象；书评以 ActivityPub 对象形式联邦，Mastodon 也能看到；支持邀请制注册 | 对象是书，不是地点 | **只借协议，不抄代码**（ACSL 与 GPL/AGPL/MIT 都不兼容）。参考 [FEDERATION.md](https://github.com/bookwyrm-social/bookwyrm/blob/main/FEDERATION.md) 的 Review/Rating 扩展，以及 [runyourown.social](https://runyourown.social/) 的「小型高信任社区」理念 |
| Mobilizon | [Framagit](https://framagit.org/kaihuri/mobilizon) | AGPL-3.0 | Elixir/Phoenix + PostgreSQL/PostGIS | 2026-11-25 / ~160★（仓库已从 Framasoft 转到 Kaihuri 命名空间） | 群组 + 带地点的活动，ActivityPub 联邦 | 关注活动，不是长期推荐 | 「群组 actor」模型：一个 OSFM 实例对外可以表现为一个 Group actor，供其他实例订阅 |
| places.pub | [GitHub](https://github.com/social-web-foundation/places.pub) | AGPL-3.0 | JavaScript | 2026-11-19 / ~30★ | 把每个 OSM 要素映射成 AS2 `Place` 对象，URL 形如 `places.pub/{node\|way\|relation}/{id}`（[SWF 介绍](https://socialwebfoundation.org/tag/checkin/)） | 地点不是 actor，不能被关注；搜索功能弱 | **联邦时用它的 URL 当跨实例的地点主键**，两个实例谈论同一家店时可以对齐 |
| checkin（swf.pub 示例客户端） | [GitHub](https://github.com/social-web-foundation/checkin) | 无 | 纯前端 + ActivityPub C2S（OAuth 2.0） | 2026-08-21 / ~5★ | 类 Swarm 的签到，签到对象引用 places.pub | 极简 demo | 「Arrive 活动 + Place 对象」的消息形状可直接参考 |
| wanderer | [GitHub](https://github.com/open-wanderer/wanderer) | AGPL-3.0 | SvelteKit + PocketBase + Meilisearch（另有 Dart 移动端） | 2026-11-24 / ~4.1k★；v0.17.0（2025-06）起支持 ActivityPub | 可自托管的地理内容、分享与列表、**已经实现地理内容的联邦** | 内容是徒步轨迹 | 研究它的 ActivityPub 实现，看「地理对象 + 列表」怎样联邦 |

---

## 4. 自托管地图 / 旅行类应用

| 项目 | 链接 | License | 技术栈 | 活跃度 | 与 OSFM 的重合 | 缺口 | 可借鉴/可复用 |
|---|---|---|---|---|---|---|---|
| FacilMap | [GitHub](https://github.com/FacilMap/facilmap) | AGPL-3.0 | TypeScript（Node + Socket.io），前端 Leaflet；数据库可选（是否支持 SQLite 未核实） | 2026-11-13 / ~412★（GitHub Release 停在 v4.1.2 / 2024-07） | 协作地图、标记类型可自定义字段、实时协作，**不需要账号**，靠管理/编辑/只读三种链接控制权限 | 信任只取决于「谁拿到链接」，没有个人身份和签名 | 零开发方案之二；自定义字段类型的设计值得参考 |
| AdventureLog | [GitHub](https://github.com/seanmorley15/AdventureLog) | GPL-3.0 | Django + DRF + PostGIS；SvelteKit + svelte-maplibre | 2026-11-24 / ~3.9k★ | 地点打卡、分类、公开/私有、邀请编辑者或查看者 | 以个人旅行为中心，没有「群推荐」和担保 | 地点、访问、分类的数据模型；GPL-3.0 可并入 AGPL 项目 |
| TRIP | [GitHub](https://github.com/itskovacs/trip) | MIT | Python（SQLModel + Alembic + **SQLite**）+ Angular 21 + Leaflet/markercluster；支持 OIDC | 2026-10-02 / ~2.0k★ | **栈最接近 MVP**：SQLite 单体、POI 加分类、协作分享 | 面向行程规划，没有推荐人和信任链 | MIT 可以放心复用；markercluster 加分类图标的交互、Docker 单容器部署 |
| Dawarich | [GitHub](https://github.com/Freika/dawarich) | AGPL-3.0 | Rails + PostgreSQL/PostGIS | 2026-11-24 / ~11k★；1.23.0（2026-11-15） | 家庭成员位置共享、Places/Visits | 是位置历史，不是推荐 | 「到访」推断：以后可以让成员证明自己去过（类似 OSM 论坛提出的「GPS 确认到访」） |
| Organic Maps | [GitHub](https://github.com/organicmaps/organicmaps) | Apache-2.0 | C++ 原生 App | 2026-11-25 / ~16k★ | 书签列表可用 KML/KMZ/GPX 导出、分享 | 移动端离线地图，没有服务端 | **导出 KML 让成员在离线地图里使用**是低成本的增值功能 |
| CoMaps | [Codeberg](https://codeberg.org/comaps/comaps) | Apache-2.0 | C++（Organic Maps 的社区分叉，2025） | 2026-11-25 / ~2.3k★（Codeberg） | 同上 | 同上 | 同上；已有展示 Mangrove 评论的功能请求 |
| OsmAnd | [GitHub](https://github.com/osmandapp/OsmAnd) | 代码 GPLv3；美术资源 CC-BY-NC-ND / 部分专有 | Java/Android + iOS | 2026-11-24 / ~6.2k★ | 收藏夹（GPX）、分组 | 同上 | 提供 GPX 收藏夹导出 |
| Kepler.gl | [GitHub](https://github.com/keplergl/kepler.gl) | MIT | React + deck.gl | 2026-11-25 / ~12k★ | — | 大数据可视化，不是协作编辑 | 不需要 |
| Felt | 商业 SaaS | 专有 | — | — | 协作地图 | 闭源 | 仅作 UX 参照 |
| Google My Maps | 商业 | 专有 | — | — | 群里最常见的「凑合方案」 | 数据锁定，无法做到熟人担保 | 开源替代就是 uMap 和 FacilMap |

注：[awesome-selfhosted 的 Maps 分类](https://github.com/awesome-selfhosted/awesome-selfhosted#maps-and-global-positioning-system-gps)目前收录 AdventureLog、Dawarich、TRIP、wanderer 等，**没有收录 uMap 和 FacilMap**，也**没有任何「熟人美食推荐」类条目**；Food 相关条目全部在 Recipe Management（Mealie、Tandoor 等菜谱管理）里。

---

## 5. 美食专项开源项目

| 项目 | 链接 | License | 技术栈 | 活跃度 | 与 OSFM 的重合 | 缺口 | 可借鉴/可复用 |
|---|---|---|---|---|---|---|---|
| FFM · Fzu Food Map | [GitHub](https://github.com/155TuT/FFM-FzuFoodMap) | MIT | TypeScript 静态站（GitHub Pages） | 2026-08 / ~14★ | **「个人与身边朋友实际探店后整理」**，理念上最接近熟人担保 | 维护者集中整理，通过 issue 投稿，没有账号和签名 | 声明文案（「未收录不代表不好吃」「与店家无商业合作」）可以直接借鉴为 OSFM 的社区公约 |
| valaxy-addon-food-map | [GitHub](https://github.com/CacheTide/valaxy-addon-food-map) | MIT | Vue / Valaxy 博客插件 | 2026-11-19 / ~5★ | 从 Markdown frontmatter 生成美食点，**导出共享 JSON，并能聚合其他博客的 JSON** | 只是静态博客插件 | **「联邦精简版」样板**：每个实例发布一份公开 JSON feed，别的实例订阅聚合，同时把私密时间线排除在外。OSFM 联邦 v0 可以先走这条路，比 ActivityPub 简单得多 |
| hedonism | [GitHub](https://github.com/kirs/hedonism) | MIT | 每城一个 Markdown 文件 + 地图（Ruby/JS） | 2026-05 / ~42★ | 个人署名的全球美食推荐 | 单人维护 | 「推荐即 Markdown」便于导入导出 |
| where-to-eat（多人聚餐选址） | [GitHub](https://github.com/shaozhengmao/where-to-eat) | MIT | 高德路线 | 2026-10 / ~10★ | 群体决策「吃哪」 | 不是推荐库 | 以后可做「群聚餐选点」插件 |
| findalicious | [GitHub](https://github.com/kelvinfan001/findalicious) | GPL-3.0 | — | 2025-09 / ~6★ | 好友左右滑选餐厅 | 同上 | 同上 |
| 微信小程序美食地图模板 | [CloudKits/miniprogram-foodmap](https://github.com/CloudKits/miniprogram-foodmap) | Apache-2.0 | 微信小程序 | 2026-11 / ~369★ | 中文用户熟悉的交互 | 绑定微信，不能自托管 | 中文 UI 文案和交互参考 |

结论：**没有任何美食专项开源项目把「邀请制 + 推荐人署名 + 单条可见性 + 自托管 + 联邦」组合在一起。**

---

## 6. 交互参考：明日方舟「蟑螂地图」（舟P地图 / 舟友地图）

- **是什么**：B 站 UP 主做的《明日方舟》同人交友地图，玩家在世界地图上放下一只代表自己的「小强」，可以选本命干员、可选填游戏 UID。几天内就落下 2 万到 2.5 万个标记（[搜狐](https://www.sohu.com/a/1099798153_122348420)）。原视频被 B 站下架，但网站靠转发继续扩散。它不是游戏内地图，也没有公开源码（未核实）。
- **机制**（来自其[隐私政策](https://zhoupmap.com/privacy)）：邮箱验证码登录；「碰触须」需要**双方同意**才互相公开 UID；标记可以「搬家」或收回；社交记录 90 天自动删除，公告位置 7 天删除；**来自 3 个不同「可信用户」的举报会先临时隐藏对方的名字和 UID**，再人工复核；有黄牌、7 天禁言、封禁等分级处罚；要求 18 岁以上；底图可在高德与 OSM 间切换，搜索用 Photon。
- **对 OSFM 的 UX 启示**：
  1. **标记本身就是身份**：小强的外观代表这个人。OSFM 可以让推荐人的头像或代号直接作为图钉的样式，「谁推荐的」一眼就能看到，这比在详情页里写「推荐人：xxx」更能体现担保。
  2. **默认模糊，同意后才披露**：联系方式要双方同意才公开。对应到 OSFM：化名默认展示，真名只对同一实例的成员公开，跨实例联邦时只给化名。
  3. **位置隐私要讲在前面**：开放后马上有人担心被「开盒」。OSFM 的店铺位置没有隐私问题，但「推荐人常去的时段 + 常驻区域」组合起来能推出个人的作息规律，需要支持只标注到区或商圈的粗粒度，并支持单条可见性。
  4. **可信用户举报阈值**：「N 个可信成员举报就先隐藏」很适合 OSFM 处理不实推荐或软广，可以和邀请树结合（见第 7 节）。
  5. **数据定期过期**：社交痕迹定期清理，把「已关店」和「过时推荐」做成自然衰减，而不是让它们永久留在地图上。
  6. **趣味性带来增长**：NPC 随机事件、雷达热榜等玩法证明，在「熟人地图」上加一点游戏化（例如「本周新增」「群友足迹」）很能拉动群聊传播。

---

## 7. 邀请树 / 信任模型参考

| 项目 | 链接 | License | 技术栈 | 活跃度 | 机制要点 | OSFM 可借鉴 |
|---|---|---|---|---|---|---|
| Lobsters | [GitHub](https://github.com/lobsters/lobsters) · [About](https://lobste.rs/about) | BSD-3-Clause | Rails | 2026-11-24 / ~4.9k★ | **邀请树公开**，每个人的主页显示是谁邀请的；被邀请者因垃圾内容或马甲被封时，管理员会考虑**取消邀请人的邀请权**，极少数情况下连带封禁；新用户前 70 天不能邀请、不能标记；所有管理操作公开可查 | ① `users.invited_by` 字段；② 新成员冷静期；③ 连带责任（只收回邀请权，不连坐封号）；④ 公开审计日志。BSD 许可可以放心参考 |
| Tildes | [GitLab](https://gitlab.com/tildes/tildes) | AGPL-3.0 | Python（Pyramid）+ PostgreSQL | 2026-08-17 / ~355★ | 一对一邀请码，按配额发放；「被谁邀请」**曾公开，后来改为不公开**，管理员仍可查看（[公告](https://tildes.net/~tildes.official/26m/invited_by_information_for_users_is_no_longer_displayed)）；信任系统长期处于规划中 | 印证了「邀请树要不要公开」是真实存在的取舍。OSFM 建议：**实例内成员可见，对外（联邦）不可见** |
| Discourse 信任等级 | [GitHub](https://github.com/discourse/discourse) · [官方说明](https://blog.discourse.org/2018/06/understanding-discourse-trust-levels/) | GPL-2.0 | Rails + Ember | 2026-11-25 / ~48k★ | TL0 到 TL4 按阅读量、天数、获赞数自动晋升，逐级解锁能力 | OSFM 不需要那么复杂。可以简化为三档：「新成员（推荐需一名老成员确认）→ 成员 → 管理员」。社区插件 [discourse-invite-stats](https://github.com/ducks/discourse-invite-stats) 做邀请问责，0★，仅供参考 |
| lib.reviews | 见第 2 节 | CC0 | — | — | 用邀请码防垃圾评论 | 同上 |
| BookWyrm | 见第 3 节 | ACSL | — | — | 实例级的邀请与申请队列 | 「申请加入 → 管理员批准」流程 |

---

## 结论

### (1) 「熟人担保层 + 地图」这个位置有没有成熟的开源产品？——**确认没有**

证据如下：
- **地图协作工具**（uMap、FacilMap、Google My Maps 的开源替代）有地图和协作，但信任单位是「编辑权限」或「链接」，**没有逐条推荐的署名与担保语义**，也做不到单条可见性。
- **开放评论体系**（Mangrove、lib.reviews、cartes.app）有签名或社交评论，但都面向**公开世界**。2025 年 9 月到 2026 年 4 月 OSM 社区论坛关于「众包评论服务」的长帖里，主要难题正是垃圾内容、刷评和「评论者有没有真去过」（[帖子](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235)），被提出的最有效防线是 lib.reviews 的**邀请制**。这从侧面证明「熟人担保」是公开评论体系的缺口，不是现成能力。
- **最接近的个人项目** foodfolk-discovery-map 有好友层和单条可见性，但它开放注册、没有 License、只有一天的提交历史，不能算产品；FFM 福大美食地图有熟人理念，但只是静态策展。
- awesome-selfhosted 的 Maps 和 Food 分类里没有同类条目。
- **保留意见**：GitHub 上每天都有新的 0★「food-map」个人仓库，不能排除存在未被检索到的小项目，但可以确定**不存在成熟、活跃、可自托管的同类产品**。

### (2) 零开发过渡方案

| 方案 | 做法 | 优点 | 局限 |
|---|---|---|---|
| **A. 自托管 uMap（首选）** | 用 Docker 或 YunoHost 部署，或者先借用公共实例 umap.openstreetmap.fr。建一个团队，把地图设为「仅编辑者可见」，编辑权限只给团队成员。**每位成员一个图层**，图层名就是化名，以此模拟署名；在标记属性里约定字段：`店名 / 位置描述 / 时段 / 推荐人 / 来源链接 / cuisine`；用 uMap 3.x 的实时协作同时编辑 | 有 OSM 底图、能导出 GeoJSON 和 CSV（以后可无损迁入 OSFM）、有权限控制、开源 | 署名靠自觉，别人也能改你的点；登录依赖实例配置的 OAuth 提供方；公共实例上「仅编辑者可见」的隐私强度要自己评估 |
| B. FacilMap | 建一张协作地图，把**编辑链接**只发到群里，用自定义标记类型放上面那些字段 | 无需账号，上手最快 | 链接泄露就等于失守，没有任何身份；只适合非常小的群 |
| C. TRIP（MIT，SQLite） | 自托管，接 OIDC，用分类管理店铺 | 栈与 OSFM MVP 最接近，迁移成本低 | 面向行程，多人推荐的语义弱 |
| D. 共享 KML 列表 | 在 Organic Maps / CoMaps 里建书签列表，导出 KML 发到群里 | 零服务器、离线可用 | 没有协作，只能由一个人维护 |

建议：**群里先用方案 A 跑 1 到 2 个月**，同时观察「位置描述」「时段」这些字段在实际使用中的填写率，用来反推 OSFM 的 schema。

### (3) 可以复用什么

- **前端地图**：Leaflet（BSD-2，[GitHub](https://github.com/Leaflet/Leaflet)，~46k★）加 leaflet.markercluster，或者 MapLibre GL JS（BSD-3，~12k★）。uMap 4.0 改用 OpenLayers（BSD-2）也可以考虑。
- **地理搜索**：Photon（Apache-2.0，[GitHub](https://github.com/komoot/photon)，~3.2k★），做法学 foodfolk，由服务端代理请求；Nominatim；Overpass API（服务端是 AGPL，但**调用**公共 API 不受影响，只需遵守各自的使用政策）。
- **数据模型**：
  - 推荐的形状参照 **Mangrove 的 JWT 签名评论**：每位成员一把密钥，推荐即签名对象，`sub` 用 `geo:lat,lng?q=店名&u=半径`。它天然能承载「相对位置描述」，也让「担保」在密码学上可以验证，联邦时可以验签。
  - 分类采用 OSM 的 `cuisine=*` 和 `diet:*` 作为受控词表；店铺关联 OSM 的 `node/way` ID，连锁店关联 `brand:wikidata`。
  - 单条可见性借鉴 foodfolk 的 `public/friends/private` 思路（只借思路，不抄代码），OSFM 可以扩展为 `instance / federated / private`。
  - 邀请树：`members.invited_by`、新成员冷静期、连带收回邀请权（Lobsters），再加「N 个可信成员举报就先隐藏」（舟P地图）。
- **联邦协议（分两步）**：
  - v0：照 valaxy-addon-food-map 的做法，每个实例发布一份签名的公开 JSON feed，好友实例订阅聚合；
  - v1：ActivityPub，实例作为 Group actor（Mobilizon），推荐作为 Review/Note（BookWyrm 的 FEDERATION.md），地点引用 places.pub 的 URL，参照 wanderer 的实现。ATProto（cartes.app）作为备选。
- **回馈 OSM**：缺店时引导成员提交 OSM Note 或打开 MapComplete，**不要**把成员推荐写进 OSM。

### (4) License 建议：**服务端用 AGPL-3.0-or-later**；协议规范、JSON Schema、客户端 SDK 另用 Apache-2.0 或 CC-BY 4.0

理由：
1. **形态是可自托管的服务端软件**。GPL-3.0 的「网络服务漏洞」意味着有人把 OSFM 改成闭源 SaaS 运营时无需公开修改；AGPL 的第 13 条正好堵住这一点。同类自托管或联邦软件大多用 AGPL：uMap、FacilMap、Mobilizon、Tildes、wanderer、Dawarich、places.pub、cartes.app。社区对 AGPL 已经很习惯，并不妨碍群主自托管，因为**自托管且不修改代码时，AGPL 没有任何额外义务**。
2. **代码借用的兼容性**：
   - AGPL-3.0：uMap、FacilMap、places.pub、Mobilizon、cartes 的代码**只有 OSFM 也用 AGPL-3.0 时才能直接复用**。如果选 MIT 或 Apache，这些代码一行都不能复制。
   - GPL-3.0-or-later：MapComplete、AdventureLog 的代码可以并入 AGPL-3.0 作品（GPLv3 与 AGPLv3 的第 13 条互相允许组合）。如果选 MIT 或 Apache，同样不能复制。
   - Apache-2.0、MIT、BSD：Mangrove、Photon、CoMaps、Organic Maps、TRIP、Leaflet、MapLibre 在任何选择下都可以用。
   - **不能复用**：BookWyrm（ACSL，非开源许可）、foodfolk-discovery-map 和 better-food-map（都没有 License），这些只能参考思路和协议文档。
3. **兼顾推广**：联邦要靠别人实现兼容客户端。把**协议、数据格式和 SDK** 放在宽松许可下，别人做兼容实现、App 或插件就没有顾虑；服务端用 AGPL 防止闭源托管分叉。Mastodon（服务端 AGPL，API 生态开放）和 Mangrove（规范 Apache）都是这种分层做法。
4. **只有在下面这种情况才考虑 MIT/Apache-2.0**：团队明确要吸引商业公司嵌入、并且**承诺绝不复制 uMap/FacilMap 代码**。但 OSFM 的核心价值在于「防止商家渗透的熟人层」，闭源商业分叉恰恰最可能破坏这个价值，因此不推荐。
5. **附带的数据许可提醒（未核实，需法律确认）**：如果 OSFM 把 OSM 的店名和坐标**复制**进自己的库并对外提供，可能构成 ODbL 下的「衍生数据库」，需要在被请求时以 ODbL 提供。只存 OSM ID 引用加成员自己写的推荐，风险较低。邀请制私有实例算不算 ODbL 所说的「公开使用」存在灰色地带。成员推荐内容的许可建议在用户协议里明确，例如默认只在实例内授权，选择联邦公开时采用 CC BY 4.0，以便与 Mangrove 的数据兼容。

---

### 附：主要来源
- OSM 社区论坛「A crowd-sourced review service for OpenStreetMap」：[第 1 页](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235) · [第 2 页](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235?page=2) · [第 3 页](https://community.openstreetmap.org/t/a-crowd-sourced-review-service-for-openstreetmap/136235?page=3)
- [Mangrove 数据格式文档](https://docs.mangrove.reviews/) · [uMap changelog](https://docs.umap-project.org/en/master/changelog/) · [Lobsters About](https://lobste.rs/about) · [Social Web Foundation checkin 标签](https://socialwebfoundation.org/tag/checkin/)
- 舟P地图：[隐私政策](https://zhoupmap.com/privacy) · [搜狐报道一](https://www.sohu.com/a/1099798138_122004016) · [搜狐报道二](https://www.sohu.com/a/1099798153_122348420) · [网易报道](https://m.163.com/dy/article/LBFILPUN0526VGN7.html)
- [awesome-selfhosted](https://github.com/awesome-selfhosted/awesome-selfhosted) · [OpenTripMap API](https://dev.opentripmap.com)
