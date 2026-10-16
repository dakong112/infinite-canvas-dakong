---
title: 接口响应约定
description: 业务接口统一响应结构与前端处理约定
---

# 接口响应约定

后端业务接口统一返回 JSON：

```json
{
  "code": 0,
  "data": {},
  "msg": "ok"
}
```

- `code`: 业务状态码，`0` 表示成功，非 `0` 表示失败。
- `data`: 业务数据。失败时通常为 `null`。
- `msg`: 响应消息。成功默认为 `ok`，失败时放错误原因。

前端请求逻辑以 `code` 判断业务是否成功。当前后端业务失败也会返回 HTTP 200，前端不要只依赖 HTTP 状态码判断结果。

接口连接失败、服务不可达、返回体不是约定 JSON 时，前端按网络或接口异常处理。

## 后台权限码

`/api/admin/*` 先过 `AdminAuth`：本站管理员（未关联 new-api 的 `role=admin` 账号）拥有全部权限；统一账号用户需持有任一 `canvas.*` 权限码（new-api 角色授予，SSO 登录时下发、每 60 秒刷新）。new-api 管理员也靠权限码进入，SSO 应用须在 new-api 设置 `App=canvas`。各路由组再由 `RequirePermission` 检查一个权限码，缺少时返回 HTTP 403：

| 权限码 | 接口 |
| --- | --- |
| `canvas.user.read` | `GET /users` |
| `canvas.order.read` | `GET /credit-logs`、`GET /templates/fees` |
| `canvas.setting.write` | 系统设置（含电商提示词模板、渠道、存储、ComfyUI 桥接、RunningHub）、AI 日志、用户与算力点日志的增改删 |
| `canvas.prompt.write` | 提示词库、提示词分类同步、Skill 管理 |
| `canvas.asset.write` | 素材库 |
| `canvas.template.read` | 模板列表、示例记录、公共示例素材 |
| `canvas.template.write` | 新增 / 编辑 / 删除模板、示例生成、保存公共示例素材 |
| `canvas.template.publish` | 发布、下架、修改或删除已发布模板（服务端在保存 / 删除时额外检查） |
| `canvas.academy.read` | 学院课程列表、课程报名名单（`GET /academy/courses[/:id/enrollments]`） |
| `canvas.academy.write` | 新增 / 编辑 / 删除学院课程 |

`/api/auth/me` 返回 `permissions` 与 `isLocalAdmin`，后台菜单按此过滤，无权限的按钮置灰。

## 空间

`GET /api/spaces` 返回 `{current, spaces}`（个人空间 `id` 为空，团队为 `team:<ID>`，团队列表来自 new-api `POST /api/sso/teams`，缓存 60 秒）；`PUT /api/space {teamId}` 切换空间，0 为个人空间，非成员返回失败。页面请求带 `X-Space-Id`（`personal` 或 `team:<ID>`），`/api/v1/*` 发现与账号当前空间不一致时返回 HTTP 409「当前空间已切换，请刷新页面」。团队空间下画布项目、我的素材、作品、商品套图、带货视频按空间共享；平台分组渠道的生成改用团队 Key（团队钱包扣费），`/api/account/summary` 增加 `team`（团队余额、我的剩余额度）。

## 学院与更多应用

- `GET /api/academy/courses?category=`、`GET /api/academy/courses/:slug` 公开（详情带登录态时返回 `enrolled`）；`POST /api/v1/academy/courses/:slug/enroll` 报名（已报名再次调用返回原记录，不重复扣费；积分不足提示「积分不足，请充值后再报名」）；`GET /api/v1/academy/enrollments` 我的报名。
- `GET /api/account/apps`：用用户的 new-api 访问令牌代理 `GET /api/sso/apps`，按用户缓存 60 秒，去掉本站自身（`NEWAPI_CLIENT_ID`）和非 http(s) 的 `launch_url`，返回 `[{clientId, name, launchUrl}]`；侧栏「更多应用」用它，列表为空时不显示。

## 文件地址签名

- 业务 JSON 响应中的 `/api/files/<id>/content` 地址在输出时统一处理（`handler.SignFileURLs` 中间件）：公开文件去掉签名；当前用户是所有者（或管理员）的文件追加 `exp`、`sig`（7 天有效）；其他文件原样返回。数据库保存无签名地址。
- 前端需要自行拼地址时调用 `POST /api/files/sign {ids}`，返回 `{ [id]: url }`；`GET /api/files/:id` 返回的 `url` 同样按此规则签名。这两个接口除 `Authorization` 外也认匿名存储会话 Cookie（路径 `/api`），匿名上传者可续签；`GET /api/files/:id` 的 `objectKey`、`publicUrl` 等存储位置只返回给所有者、管理员或公开文件。
- 内容响应缓存：公开文件 `public, max-age=31536000, immutable`；凭签名访问 `private, max-age=<剩余有效秒数>`；凭登录身份访问 `private, no-store`。
- `FILE_URL_SIGNING=false` 关闭签名校验。

## AutoDL 渠道

- `POST /api/ai/autodl/workflows` 使用上述业务响应结构。请求为 `{ baseUrl, workflowId? }`，不传上游 API Key；不带工作流 ID 返回列表，带 ID 返回详情。元数据包含 `uuid`、`name`、`kind` 和详情的 `input_rules`，不返回上游内部节点映射。
- AutoDL 视频继续使用现有 `/api/v1/videos` 创建、查询及任务响应结构。
- `/api/v1/audio/speech` 是 AI 协议代理入口。AutoDL 成功时返回 `{ provider: "autodl", audio_url, mime_type }`；失败仍使用现有 `Fail` 业务结构。其他语音协议的响应保持原样。
- 现有 `/api/v1/canvas/audio-tasks` 消费 AutoDL 的上游音频 URL，返回既有任务结构，不新增结果下载或转存。

## 渠道自定传参转译

- 沿用 `/api/v1/images/generations`、`/api/v1/images/edits`、`/api/v1/audio/speech`、`/api/v1/videos` 及现有画布任务入口。命中自定义的账号请求携带 `{ model, n, _parameterTranslation: { kind, variables } }`；视频另携带面板 `seconds`，沿用现有按秒费用计算。`kind` 为 `image`、`video`、`audio`。
- `_parameterTranslation` 是系统内部负载。后端按账号或公共渠道读取保存的原文，转译后只向供应商发送配置里的方法、地址、请求头、查询参数和请求体。
- 图片 JSON 结果归一为 `{ data: [{ url }] }`；音频 URL 归一为 `{ provider: "parameter-translation", audio_url }`。配置直接返回文件时沿用二进制响应及 MIME。失败仍用现有 `Fail` 业务结构。
- 视频沿用现有任务响应，增加 `parameter_translation: true` 标记；私有查询快照和公共渠道脚本不随响应返回。创建、查询、完成和失败均进入现有任务管理，失败不重发默认请求。
