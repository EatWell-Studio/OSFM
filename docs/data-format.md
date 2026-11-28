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

## 个人数据导出（`osfm-personal/1`）

成员在"我的"页下载自己的全部数据（`E6`）。接口 `GET /api/osfm/me/export`，只限已登录的成员，只返回请求者本人的数据：

- 响应是一个 JSON 文件下载：`Content-Type: application/json; charset=utf-8`、`Content-Disposition: attachment; filename="osfm-personal-export-<UTC 日期>.json"`、`Cache-Control: no-store`。
- 未登录 401。每个成员有单独的额度（一次 3 个，之后每分钟 1 个），超出 429 `rate_limited` + `Retry-After`。
- 时间是 RFC 3339 的 UTC 时间；"日期"是 `YYYY-MM-DD`。可选的时间没有值时为 `null`，可选的文本没有值时为空字符串，列表没有内容时为 `[]`。字段名用 camelCase，与接口的响应一致。

### 不包含的内容

- **其他成员的任何信息**，唯一的例外是邀请人的主名（`account.invitedBy`）：没有其他成员的 `uuid`、登录名或化名，没有谁用了自己的邀请，修改记录只写修改人的角色（作者 / 维护者），没有隐藏推荐的维护者，举报只写被举报推荐的 `uuid`、不写它的作者或内容。
- **任何秘密**：没有密码哈希、TOTP 密钥、passkey 的凭据 ID 与公钥、用户句柄、恢复码（及其哈希）、会话或令牌。凭据只有类型、设备名和日期。
- **已删除的推荐**（删除时正文已被抹去）。所在地点已被维护者删除、推荐本身未删除的，仍然导出（推荐语仍属于作者），标为 `placeDeleted: true`，地点按保存时的样子给出；不写谁删除了地点、为什么删除。
- 实例内部的记录 ID：对象一律用 `uuid`（UUIDv7）指代。

文件里有成员自己的私密推荐，也写明了哪些推荐用的是化名（`displayAs`）：这正是只有本人才能知道的信息，文件应妥善保管。

### 结构

```json
{
  "osfm:schema": "osfm-personal/1",
  "exportedAt": "2026-03-01T08:00:00Z",
  "account": {
    "uuid": "0190a1b2-0000-7000-8000-000000000002",
    "loginName": "example_member",
    "displayName": "Example Member",
    "pseudonym": "Example Pen Name",
    "externalLinks": [{ "url": "https://example.org/blog", "label": "Blog" }],
    "role": "member",
    "joinedAt": "2026-01-01T00:00:00Z",
    "invitedBy": "Example Maintainer"
  },
  "invites": [
    {
      "uuid": "0190a1b2-0000-7000-8000-000000000010",
      "state": "used",
      "createdAt": "2026-02-01T00:00:00Z",
      "expiresAt": "2026-02-08T00:00:00Z",
      "usedAt": "2026-02-02T00:00:00Z",
      "revokedAt": null
    }
  ],
  "credentials": [
    { "kind": "passkey", "deviceName": "Phone", "createdAt": "2026-01-01T00:00:00Z", "lastUsedAt": "2026-02-28T00:00:00Z" }
  ],
  "recommendations": [
    {
      "uuid": "0190a1b2-0000-7000-8000-000000000020",
      "displayAs": "pseudonym",
      "visibility": "private",
      "attitude": "must_try",
      "text": "Example text",
      "dishes": ["Example dish"],
      "timeSlots": ["lunch"],
      "season": "",
      "sources": [{ "url": "https://example.org/post", "kind": "visited", "excerpt": "Example excerpt" }],
      "visitedOn": "2026-02-20",
      "createdAt": "2026-02-21T00:00:00Z",
      "updatedAt": "2026-02-22T00:00:00Z",
      "editedByMaintainerAt": null,
      "hidden": null,
      "place": { "uuid": "0190a1b2-0000-7000-8000-000000000030", "name": "Example Noodle Bar", "lat": 0.34, "lon": 7.06 },
      "placeDeleted": false,
      "revisions": [
        {
          "uuid": "0190a1b2-0000-7000-8000-000000000040",
          "createdAt": "2026-02-22T00:00:00Z",
          "editorRole": "author",
          "content": {
            "attitude": "good",
            "text": "Example text before the edit",
            "dishes": [],
            "timeSlots": [],
            "season": "",
            "sources": [],
            "visitedOn": ""
          }
        }
      ]
    }
  ],
  "reports": [
    { "recommendation": "0190a1b2-0000-7000-8000-000000000050", "reason": "advertising", "note": "", "createdAt": "2026-02-25T00:00:00Z" }
  ],
  "notices": [
    {
      "uuid": "0190a1b2-0000-7000-8000-000000000060",
      "kind": "maintainer_edited",
      "ref": "0190a1b2-0000-7000-8000-000000000020",
      "createdAt": "2026-02-22T00:00:00Z",
      "readAt": null
    }
  ]
}
```

### 字段

| 字段 | 说明 |
| --- | --- |
| `osfm:schema` | `osfm-personal/1` |
| `exportedAt` | 生成文件的时间（精确到秒） |
| `account.uuid` | 成员自己的 `uuid` |
| `account.loginName` | 登录名 |
| `account.displayName` | 主名 |
| `account.pseudonym` | 化名；没有时为空字符串 |
| `account.externalLinks` | 自己填写的外部链接 `[{url, label?}]`，最多 5 条 |
| `account.role` | `member` 或 `maintainer` |
| `account.joinedAt` | 加入时间 |
| `account.invitedBy` | 邀请人的主名；第一个维护者为空字符串 |
| `invites[]` | 自己创建的邀请，新的在前：`uuid`、`state`（`active`、`used`、`expired`、`revoked`）、`createdAt`、`expiresAt`、`usedAt`、`revokedAt`。邀请码本身只在创建时出现一次，不在导出里 |
| `credentials[]` | 登录方式，旧的在前：`kind`（`passkey` 或 `password_totp`）、`deviceName`（passkey 的名称，密码 + TOTP 为空）、`createdAt`、`lastUsedAt`（最近一次用它登录或再次验证的日期，UTC 零点；从未用过为 `null`） |
| `recommendations[]` | 自己的推荐，新的在前，包括私密的、被隐藏的、用化名署名的，以及所在地点已被删除的 |
| `….displayAs` | 署名方式：`main`（主名）或 `pseudonym`（化名） |
| `….visibility` | `private`（只有自己）或 `circle`（圈内） |
| `….attitude` | `must_try`、`good`、`ok`、`avoid` |
| `….text`、`dishes`、`timeSlots`、`season`、`sources`、`visitedOn` | 推荐的内容；时段取值 `breakfast`、`lunch`、`afternoon`、`dinner`、`late_night`；来源 `[{url?, kind, excerpt?}]`，`kind` 为 `visited`、`chat`、`note`、`other` |
| `….createdAt`、`updatedAt`、`editedByMaintainerAt` | 创建、最后修改、维护者最后一次修改的时间 |
| `….hidden` | 未隐藏时为 `null`；被隐藏时为 `{kind, reason?}`，`kind` 为 `maintainer`、`reports`、`member_removed`，`reason` 是维护者写的原因（举报自动隐藏没有原因） |
| `….place` | 所在地点：`uuid`、`name`、`lat`、`lon`（WGS-84，5 位小数） |
| `….placeDeleted` | 所在地点已被维护者删除时为 `true`：推荐不再对任何人显示，但仍是作者的数据；`place` 是删除前保存的地点 |
| `….revisions[]` | 修改记录，新的在前：`uuid`、`createdAt`、`editorRole`（`author` 或 `maintainer`）、`content`（修改前的内容：`attitude`、`text`、`dishes`、`timeSlots`、`season`、`sources`、`visitedOn`） |
| `reports[]` | 自己提交的举报，新的在前：`recommendation`（被举报推荐的 `uuid`）、`reason`（`advertising`、`abuse`、`political_illegal`、`unverified_claim`、`false_or_duplicate`、`other`）、`note`（自己写的说明）、`createdAt` |
| `notices[]` | 站内提示，新的在前：`uuid`、`kind`（如 `maintainer_edited`、`hidden_by_maintainer`、`hidden_by_reports`、`restored`、`violation_deleted`）、`ref`（相关推荐的 `uuid`）、`createdAt`、`readAt` |
