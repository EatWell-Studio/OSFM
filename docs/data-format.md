# OSFM 数据格式

OSFM 导出的文件的格式。每种文件在顶层用 `osfm:schema` 写明格式与版本（`名称/版本号`）；同一版本内只会增加字段，不兼容的修改才升版本号。读取方应忽略不认识的字段。设计背景见 [architecture.md](architecture.md) §3.8。

## 个人数据导出（`osfm-personal/1`）

成员在"我的"页下载自己的全部数据（`E6`）。接口 `GET /api/osfm/me/export`，只限已登录的成员，只返回请求者本人的数据：

- 响应是一个 JSON 文件下载：`Content-Type: application/json; charset=utf-8`、`Content-Disposition: attachment; filename="osfm-personal-export-<UTC 日期>.json"`、`Cache-Control: no-store`。
- 未登录 401。每个成员有单独的额度（一次 3 个，之后每分钟 1 个），超出 429 `rate_limited` + `Retry-After`。
- 时间是 RFC 3339 的 UTC 时间；"日期"是 `YYYY-MM-DD`。可选的时间没有值时为 `null`，可选的文本没有值时为空字符串，列表没有内容时为 `[]`。字段名用 camelCase，与接口的响应一致。

### 不包含的内容

- **其他成员的任何信息**，唯一的例外是邀请人的主名（`account.invitedBy`）：没有其他成员的 `uuid`、登录名或化名，没有谁用了自己的邀请，修改记录只写修改人的角色（作者 / 维护者），没有隐藏推荐的维护者，举报只写被举报推荐的 `uuid`、不写它的作者或内容。
- **任何秘密**：没有密码哈希、TOTP 密钥、passkey 的凭据 ID 与公钥、用户句柄、恢复码（及其哈希）、会话或令牌。凭据只有类型、设备名和日期。
- **已删除的推荐**（删除时正文已被抹去）与**所在地点已被维护者删除的推荐**（这类推荐对任何人都不可见，包括作者本人）。
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
| `recommendations[]` | 自己的推荐，新的在前，包括私密的、被隐藏的和用化名署名的 |
| `….displayAs` | 署名方式：`main`（主名）或 `pseudonym`（化名） |
| `….visibility` | `private`（只有自己）或 `circle`（圈内） |
| `….attitude` | `must_try`、`good`、`ok`、`avoid` |
| `….text`、`dishes`、`timeSlots`、`season`、`sources`、`visitedOn` | 推荐的内容；时段取值 `breakfast`、`lunch`、`afternoon`、`dinner`、`late_night`；来源 `[{url?, kind, excerpt?}]`，`kind` 为 `visited`、`chat`、`note`、`other` |
| `….createdAt`、`updatedAt`、`editedByMaintainerAt` | 创建、最后修改、维护者最后一次修改的时间 |
| `….hidden` | 未隐藏时为 `null`；被隐藏时为 `{kind, reason?}`，`kind` 为 `maintainer`、`reports`、`member_removed`，`reason` 是维护者写的原因（举报自动隐藏没有原因） |
| `….place` | 所在地点：`uuid`、`name`、`lat`、`lon`（WGS-84，5 位小数） |
| `….revisions[]` | 修改记录，新的在前：`uuid`、`createdAt`、`editorRole`（`author` 或 `maintainer`）、`content`（修改前的内容：`attitude`、`text`、`dishes`、`timeSlots`、`season`、`sources`、`visitedOn`） |
| `reports[]` | 自己提交的举报，新的在前：`recommendation`（被举报推荐的 `uuid`）、`reason`（`advertising`、`abuse`、`political_illegal`、`unverified_claim`、`false_or_duplicate`、`other`）、`note`（自己写的说明）、`createdAt` |
| `notices[]` | 站内提示，新的在前：`uuid`、`kind`（如 `maintainer_edited`、`hidden_by_maintainer`、`hidden_by_reports`、`restored`、`violation_deleted`）、`ref`（相关推荐的 `uuid`）、`createdAt`、`readAt` |
