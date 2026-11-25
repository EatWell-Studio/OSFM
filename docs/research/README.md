# 初步产品调研（2026-11）

> 调研日期：2026-11-25。目的：为 PRD 与第一版框架提供依据。各文档的事实部分附来源链接；无法从一手资料核实的内容标「未核实」，推算的数字标「估算」。合规部分不构成法律意见。

## 文档

| 文档 | 回答的问题 |
| --- | --- |
| [product-form.md](product-form.md) | 产品是什么、给谁用、信任模型、数据模型草案、MVP 范围、产品形态 |
| [tech-stack.md](tech-stack.md) | 地图库、底图瓦片、搜索、后端与存储、认证、客户端、坐标转换、联邦预留、导入导出、部署 |
| [open-source-landscape.md](open-source-landscape.md) | 现有开源项目（约 35 个）能做什么、缺什么、能复用什么，以及 license 选择 |
| [geodata-and-compliance.md](geodata-and-compliance.md) | OSM 在中国的数据现状（含实测）、坐标系、中国 / 欧盟法规、ODbL、瓦片政策 |

商业竞品与商业化方面的调研不在公开仓库中。

## 核心结论

1. **"熟人担保层 + 地图"这一格没有成熟的开源产品。** 最接近的个人项目开放注册、无 license、不可复用；uMap、FacilMap 有协作地图但没有邀请、署名、化名与逐条可见性。OSM 社区讨论众包点评时，公认的未解问题正是刷评与假评，而邀请制是被提出的解法。→ [open-source-landscape.md](open-source-landscape.md)
2. **OSM 在国内"有但不全"，这恰好是 OSFM 数据层存在的理由。** 实测（Overpass，约 1.1×1.1 km）：西安钟鼓楼—回民街一带 112 个餐饮要素，营业时间覆盖 12.5%，约 23% 的条目 2020 年后无人更新，街边小摊大多缺失；慕尼黑老城同面积 323 个、营业时间覆盖 86%。回民街的名店基本都在 OSM 里，可以作为"吸附"锚点。→ [geodata-and-compliance.md](geodata-and-compliance.md)
3. **地图 + 定位全程 WGS-84 时不需要纠偏。** 只有导入高德 / 点评（GCJ-02）、百度（BD-09）的坐标，或叠加国内厂商底图时才需要换算；在回民街一带不换算的误差约 462 m / 1.14 km，足以把店放到另一条街。数据库只存 WGS-84，另记原始坐标系。
4. **推荐技术栈（待原型验证）**：MapLibre GL JS + 实例自托管的 Protomaps PMTiles 区域切片；PocketBase 嵌入 Go，前端打包进单一二进制 / 单一镜像（arm64 + amd64）；SQLite；邀请码 + 邮件验证码登录；PWA。OSM 官方瓦片只适合开发期。→ [tech-stack.md](tech-stack.md)
5. **PWA 是唯一合适的客户端形态。** 微信小程序要求主体、备案、腾讯地图与 GCJ-02，与"每个圈子自部署"冲突；原生 App 无法按实例分发。
6. **合规的关键在"私人、邀请制、境外托管、只标餐饮"。** 《公开地图内容表示规范》（2023）第十八条允许标注挂公开招牌的单位名称；在中国大陆**公开**提供可上传标注的地图服务需要测绘资质与审图号，个人无法取得，项目应明确不支持这种部署方式。
7. **X（Twitter）账号只能做自报展示。** X API 自 2026-02 起对新开发者基本无免费档，且在大陆不可用；化名推荐不显示外部账号，降低去匿名化风险。
8. **国内家宽通常封入站 80/443，公共 CDN（如 jsDelivr）在大陆不可靠。** 所有前端资源、字体、图标必须由实例自托管；部署文档要覆盖 DNS 验证证书 + 高位端口、IPv6、境外 VPS 反代等方案。

## 跨文档的综合建议

下列几点在单篇文档里各说了一部分，这里合在一起：

| 主题 | 建议 | 依据 |
| --- | --- | --- |
| 软件 license | 服务端代码 **AGPL-3.0-or-later**（防止闭源 SaaS 分叉，且与 uMap / FacilMap 兼容，可借用其代码）；导出格式规范与将来的客户端 SDK 可用 Apache-2.0 | open-source-landscape §结论 |
| 数据许可（按数据层） | **地点层**（坐标、店名、类别、精度、原话位置描述、OSM 引用）整层 **ODbL 1.0**：OSM 数据与成员补充的同类店铺在同一区域混合，属于衍生数据库，无法按来源分开授权；地点层不含任何成员信息。**推荐层**（推荐语、菜品、时段、来源、推荐人引用）只按 ID 引用地点，与地点层相互独立，许可另定，建议 **CC BY-SA 4.0**。**成员层**不对外授权。ODbL 义务对象是实例成员：为成员提供地点层的 ODbL 导出即可 | product-form §5.1；geodata-and-compliance §4 |
| 地图显示范围 | 默认 `minZoom` 设在城市级（约 10），用 `maxBounds` 限定在实例关心的城市，不渲染国界级视图；不提供任何边界或自定义类型图层 | geodata-and-compliance §0 |
| 地点类别 | 类别固定为餐饮白名单（沿用 OSM `amenity=restaurant / fast_food / cafe` 等与 `cuisine=*`），不允许自由类型 | geodata-and-compliance §0；product-form §5 |
| 位置精度 | 存储保留约 5 位小数；每个点带精度级别（门口 / 街道 / 片区）与原话位置描述；界面不暗示高于约 10 m 的精度 | product-form §4；geodata-and-compliance §0 |
| 联邦预留 | UUIDv7 + 软删除 + 逐条可见性（含"允许联邦"预留值）+ 实例签名密钥 + 有版本号的 GeoJSON 导出格式；评论格式参考 Mangrove（签名评论、按坐标 + 名称识别地点） | tech-stack §8；open-source-landscape §2；product-form §9 |
| 过渡方案 | 框架完成前，可用自托管 uMap（每个成员一个图层代替署名）收集种子数据，以后从其导出（GeoJSON / uMap 格式）导入 OSFM | open-source-landscape §结论 |
| 向 OSM 回馈 | OSFM 不直接写 OSM；只引导愿意的成员用自己的账号补充餐饮节点，只用实地确认的信息，绝不从高德 / 百度 / 点评 / Google 复制 | geodata-and-compliance §3、§4 |

## 交给 PRD 的决策

| # | 决策 | 调研给出的倾向 |
| --- | --- | --- |
| R1 | 软件 license | AGPL-3.0-or-later（服务端） |
| R2 | 后端：PocketBase（快、单一维护者、0.x）还是自写 Node / Go 服务 | 原型先用 PocketBase，数据访问封装一层以便替换 |
| R3 | 底图：自托管 PMTiles 的覆盖范围（全球低缩放 + 哪些城市） | 按圈子常去城市配置，原型阶段实测文件大小 |
| R4 | 登录方式 | 邀请码 + 邮件验证码；passkey 第二步 |
| R5 | 化名的语义与上限 | 见 product-form §11 |
| R6 | 是否支持在中国大陆托管实例 | 只支持私用、不公开；文档写明风险 |
| R7 | 推荐层的内容许可（地点层随 OSM 固定为 ODbL） | CC BY-SA 4.0 或实例可选 |
| R8 | 过渡期是否先开一个 uMap 实例收集种子数据 | 建议是，成本低 |

## 尚未核实、需要原型或实测的事项

- 中国全境与单个城市的 PMTiles 区域文件大小（目前只有全球文件的实数）。
- 大陆真机（国产 Android 定制系统）浏览器定位返回的坐标系是否为 WGS-84。
- 大陆访问 OSM 官方瓦片、OpenFreeMap、Photon 公共实例的可达性与速度。
- 若干法条的逐字原文（见 [geodata-and-compliance.md](geodata-and-compliance.md) §8）。
- PocketBase 在 2 核 ARM 设备上的内存与冷启动表现。
