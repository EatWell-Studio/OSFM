# OSFM 数据格式

OSFM 实例导出的文件格式：字段、版本与许可。架构上的位置见 [architecture.md](architecture.md) §3.8。

`/.well-known/osfm.json` 的 `dataFormatVersion`（目前为 1）是整套数据格式的版本；每种文件另在文件里写自己的格式名与版本号（如 `osfm-places/1`）。同一版本内只会增加字段，不会删除或改变已有字段的含义；不兼容的修改换新的版本号。读取方应忽略不认识的字段。

## 按层 GeoJSON 导出（`osfm-places/1`、`osfm-recommendations/1`）

圈子的地图数据分两层导出（`F1`）：**地点层**与**推荐层**，各是一个 GeoJSON 文件。

### 获取

- `GET /api/osfm/exports/places`、`GET /api/osfm/exports/recommendations`，可选 `?city=<城市标识>`：只含该城市范围（`bbox`，含边界）内的地点及其推荐；不是实例的城市时 400 `unknown_city`，参数为空时等于不带。
- 需要登录，所有成员都可以下载两层；网页上在"我的"页的"导出地图数据"。
- 响应为文件下载：`Content-Type: application/geo+json`，`Content-Disposition: attachment; filename="osfm-<层>[-<城市>]-<UTC 日期>.geojson"`，`Cache-Control: no-store`。两层合计每个成员 10 次起、之后每 30 秒 1 次，超出 429 `rate_limited`（带 `Retry-After`）。

### 包含什么

两层都是"圈子层"：**每位成员都能看到的推荐**——可见性为 `circle`（或预留的 `federated`）、未被隐藏、未删除、所在地点未删除——以及至少有一条这样推荐的地点。因此：

- 谁下载内容都一样，维护者也不例外；文件里只有成员在地图上本来就能看到的内容。请求者自己的私密推荐与被隐藏的推荐也不在其中（个人数据导出里才有）。
- 私密、被隐藏、已删除的推荐不在推荐层；只有这类推荐的地点不在地点层。
- 不含墓碑（删除记录）：M2 的导出只反映现状，墓碑留给以后的联邦。
- 不含任何成员 uuid、内部记录 ID、作者或修改人、署名方式（主名还是化名）、成员的外部链接；地点层不含任何成员信息。
- 每个成员在每个地点最多一条推荐（数据库保证）；即使数据不一致，推荐层每个作者每个地点也只取最新的一条，不会出现同一个人的两个署名。

### 文件结构

两层都是 [RFC 7946](https://www.rfc-editor.org/rfc/rfc7946) 的 `FeatureCollection`，每个地点或推荐一个 `Feature`，几何为 `Point`，坐标 `[经度, 纬度]`（WGS-84，5 位小数）。`Feature` 的 `id` 是该地点或推荐的 `uuid`。要素按 `uuid` 排序。

集合上的附加成员（RFC 7946 §6.1 的 foreign members）：

| 成员 | 内容 |
| --- | --- |
| `osfm:schema` | 格式名与版本：`osfm-places/1` 或 `osfm-recommendations/1` |
| `osfm:license` | 该层的许可 `{id, name, url}`：地点层 `ODbL-1.0`（Open Database License v1.0），推荐层 `CC-BY-SA-4.0`（Creative Commons 署名-相同方式共享 4.0 国际） |
| `osfm:attribution`、`osfm:attribution_url` | `© OpenStreetMap contributors`，`https://www.openstreetmap.org/copyright`（`H5`：位置是在 OpenStreetMap 的地图上标出、用它的数据搜到的） |
| `osfm:notice` | 上面两项的文字说明（英文） |
| `osfm:exported` | 导出时间（UTC，RFC 3339，到秒） |
| `osfm:city` | 按城市导出时的城市标识；导出全部时没有这一项 |

属性的约定：时间是 UTC 的 RFC 3339 字符串；列表没有值时为 `[]`；文字字段没有值时为 `""`；可以没有的取值（类别、营业状态、参照物、到店日期、维护者修改时间）没有时为 `null`。固定取值与接口相同（类别为 OSM 的标签值，菜系为 OSM `cuisine` 取值）。

### 地点层属性（`osfm-places/1`）

| 属性 | 内容 |
| --- | --- |
| `name` | 店名（供 uMap 等显示，见下文"显示用属性"） |
| `description` | 别名（逗号分隔）、位置描述、参照物，每项一行（供显示） |
| `osfm:uuid` | 地点的 `uuid`（UUIDv7） |
| `osfm:name` | 店名 |
| `osfm:aliases` | 别名列表 |
| `osfm:precision` | 位置精度：`exact`（精确）、`approx`（大致）、`area`（片区） |
| `osfm:position_note` | 位置描述 |
| `osfm:landmark` | 参照物：`{"type": "place", "place": <地点 uuid>, "name": <店名>}` 或 `{"type": "text", "text": <文字>}`；没有时为 `null`。参照地点不在地点层（例如只有私密推荐）时也为 `null`，不透露它的存在 |
| `osfm:category` | 类别（OSM 取值，如 `restaurant`、`cafe`），没有时为 `null` |
| `osfm:cuisine` | 菜系列表（OSM `cuisine` 取值） |
| `osfm:status` | 营业状态 `open`、`maybe_closed`、`closed`，没人确认过时为 `null` |
| `osfm:created`、`osfm:updated` | 创建与最后修改时间 |

### 推荐层属性（`osfm-recommendations/1`）

几何是所在地点的点。

| 属性 | 内容 |
| --- | --- |
| `name` | 店名 + ` · ` + 署名（供显示） |
| `description` | 推荐语（供显示） |
| `osfm:uuid` | 推荐的 `uuid` |
| `osfm:place` | 所在地点的 `uuid`（地点层里的要素） |
| `osfm:display_name` | 署名：作者的主名或化名（服务器按推荐的署名方式决定，文件里不说明是哪一种）；作者已退出圈子时，主名推荐写 `已退出成员`，化名推荐照常写化名 |
| `osfm:author_left` | 主名推荐的作者已退出圈子时为 `true`（`osfm:display_name` 为 `已退出成员`），否则为 `false` |
| `osfm:attitude` | 态度：`must_try`（强推）、`good`（可以）、`ok`（一般）、`avoid`（避雷） |
| `osfm:text` | 推荐语 |
| `osfm:dishes` | 推荐的菜 |
| `osfm:time_slots` | 时段：`breakfast`、`lunch`、`afternoon`、`dinner`、`late_night` 中的若干个 |
| `osfm:season` | 季节或时令说明 |
| `osfm:sources` | 来源 `[{"url", "kind", "excerpt"}]`：链接（没有时为 `""`）、类型（`visited`、`chat`、`note`、`other`）、短摘录 |
| `osfm:visited_on` | 到店日期 `YYYY-MM-DD`，没有时为 `null` |
| `osfm:created`、`osfm:updated` | 创建与最后修改时间 |
| `osfm:edited_by_maintainer_at` | 维护者最后一次修改它的时间，没有时为 `null`；不说明是哪位维护者 |

### 显示用属性与 uMap

`name` 与 `description` 只为 uMap 等地图工具显示用（uMap 弹窗的标题与正文），内容以 `osfm:` 属性为准。uMap 把 `description` 当作带格式的文字渲染成 HTML（允许部分 HTML 标签，`{{链接}}` 显示为图片，`{属性名}` 替换为其他属性），所以这两项里的 `<`、`>`、`{`、`}` 换成了全角的 `＜`、`＞`、`｛`、`｝`：成员写的文字不会让看地图的人的浏览器去加载外部图片。`osfm:` 属性里的文字保持原样；在 uMap 里选用"表格"式弹窗（显示全部属性）时，这些原样文字同样会被 uMap 当作格式渲染，建议用默认弹窗。

导入 uMap 时（以 uMap 3.8.1 的浏览器端导入逻辑离线验证）：要素数量、坐标顺序、`name` 与 `description`、全部属性（包括 `osfm:` 前缀的键、列表、对象与 `null`）都能保留；uMap 不保留 `Feature` 的 `id`（它只接受 5 个字符的 ID，会另行生成），`uuid` 仍在 `osfm:uuid` 里；集合上的 `osfm:` 附加成员（格式、许可、署名）不会带进 uMap 的图层，在 uMap 上公开时需要在地图的许可与署名设置里自己写明。

### 许可

- **地点层**：Open Database License (ODbL) v1.0。位置是在 OpenStreetMap 上标出的，使用时注明 "© OpenStreetMap contributors"。
- **推荐层**：推荐的文字按 Creative Commons 署名-相同方式共享 4.0 国际（CC BY-SA 4.0）许可，署名为每条推荐的 `osfm:display_name`；其中的位置来自地点层（ODbL v1.0，© OpenStreetMap contributors）。

### 版本记录

- `osfm-places/1`、`osfm-recommendations/1`（T041）：首个版本。
