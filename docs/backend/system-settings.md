---
title: 系统配置数据结构
description: settings 表中 public 和 private 配置结构说明
---

# 系统配置数据结构

系统配置保存在 `settings` 表中，目前只使用两行：

| key | 说明 |
| --- | --- |
| `public` | 公开配置，前端可以读取 |
| `private` | 私有配置，只给后端和管理员使用 |

## public.value

```json
{
  "modelChannel": {
    "availableModels": ["gpt-5.5", "gpt-image-2"],
    "modelCosts": [
      { "model": "gpt-5.5", "credits": 1 },
      { "model": "gpt-image-2", "credits": 10 }
    ],
    "defaultModel": "gpt-image-2",
    "defaultImageModel": "gpt-image-2",
    "defaultTextModel": "gpt-5.5",
    "systemPrompt": "",
    "allowCustomChannel": true
  },
  "auth": {
    "allowRegister": true,
    "linuxDo": {
      "enabled": false
    }
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `modelChannel` | object | 模型渠道公开配置组 |
| `auth` | object | 认证相关公开配置 |

`modelChannel` 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `availableModels` | string[] | 系统可用模型；保存设置时会自动合并所有已启用私有渠道的模型 |
| `modelCosts` | object[] | 模型算力点配置，图片按张、视频按秒、文本和音频按次预扣，上游失败时返还；视频智能时长 `-1` 按 15 秒计算，未配置默认不扣除 |
| `defaultModel` | string | 默认模型，从 `availableModels` 中选择；为空或失效时优先选择文本模型 |
| `defaultImageModel` | string | 默认图片模型，从 `availableModels` 中选择；为空或失效时优先选择 `seedream`、`image`、`gpt-image` 模型 |
| `defaultVideoModel` | string | 默认视频模型，从 `availableModels` 中选择；为空或失效时优先选择 `seedance`、`video` 模型 |
| `defaultTextModel` | string | 默认文本模型，从 `availableModels` 中选择；为空或失效时优先选择非图片/视频模型 |
| `systemPrompt` | string | 系统提示词 |
| `allowCustomChannel` | boolean | 是否允许用户在配置弹窗中切换为本地直连渠道，默认允许 |

`modelCosts` 每项字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `model` | string | 模型名称 |
| `credits` | number | 图片每张、视频每秒、文本和音频每次调用预扣的算力点，最多保留两位小数 |

用户侧请求模式：

| 模式 | 说明 |
| --- | --- |
| 云端渠道 | 使用后端 `/api/v1/*` 代理接口，请求会按模型名匹配 `private.value.channels` 中的可用渠道 |
| 本地直连 | 默认可选；`allowCustomChannel` 关闭后不可选，用户在浏览器本地配置 `baseUrl`、`apiKey` 和模型列表后直接请求模型接口 |

`auth` 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `allowRegister` | boolean | 是否允许用户注册，默认允许；关闭后注册入口隐藏，注册接口拒绝新用户创建 |
| `linuxDo.enabled` | boolean | 是否开启 Linux.do 登录 |

`works` 字段（我的作品与生成队列）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `retentionDays` | number | 普通用户作品保存天数，默认 7，最大 3650 |
| `memberRetentionDays` | number | 会员作品保存天数，默认 90；按提交任务时的会员状态写入到期时间 |
| `userConcurrency` | number | 普通用户同时运行的任务数，默认 2，不超过全站上限 |
| `memberConcurrency` | number | 会员同时运行的任务数，默认 4，不超过全站上限 |
| `globalConcurrency` | number | 单个服务进程同时运行的任务数，默认 8，最大 64 |

`ecommerce` 字段（电商图片工具）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `visionModel` | string | 识图模型（解析卖点、规划方案、改写卖点），走 `/responses`；为空时取 `defaultTextModel` |
| `imageModel` | string | 生图模型（套图、详情图、复刻、去背景、换装），走 `/images/edits`；为空时取 `defaultImageModel` |
| `previewQuality` | string | 预览画质 `low`/`medium`/`high`/`auto`，默认 `low` |
| `finalQuality` | string | 正式图与工具出图画质，默认 `high` |

`video` 字段（带货视频）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `model` | string | 图生视频模型，走 `/videos`；为空时取 `defaultVideoModel` |
| `keyframeModel` | string | 关键帧生图模型，走 `/images/edits`；为空时取电商 `imageModel`，仍为空则每镜直接用第一张产品图生成视频 |
| `scriptModel` | string | 分镜脚本识图模型，走 `/responses`；为空时取电商 `visionModel` |
| `ttsModel` | string | 配音模型，走 `/audio/speech`；为空时不生成配音 |
| `ttsVoice` | string | 默认音色，默认 `alloy` |
| `maxShotSeconds` | number | 单镜最长秒数，默认 5，范围 2-15 |

## private.value

```json
{
  "channels": [
    {
      "protocol": "openai",
      "name": "默认渠道",
      "baseUrl": "https://api.example.com",
      "apiKey": "sk-xxx",
      "models": ["gpt-5.5", "gpt-image-2"],
      "weight": 1,
      "enabled": true,
      "remark": ""
    }
  ],
  "promptSync": {
    "enabled": true,
    "cron": "0 0 * * *"
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `channels` | object[] | 模型渠道列表 |
| `promptSync` | object | GitHub 远程提示词定时同步配置 |
| `templateSamples` | object | 模板示例生成用的公共示例素材（`product`/`portrait`/`model` 三组图片地址） |
| `bgmTracks` | object[] | 带货视频背景音乐库（`fileId`/`name`/`tags`/`durationSec`/`url`），由曲库接口单独读写，保存设置时保留原值 |
| `ecommercePrompts` | object | 电商套图 AI 模板覆盖：`analyze`、`plan`、`sellingPoints`、`sellingPointRewrite` 与 `variants`（`main`/`white`/`detail`/`sku` 各一组变体）；空值用内置默认，保存时与默认相同的存为空，缺少默认模板中的 `{{占位符}}` 时拒绝保存；默认内容见 `GET /api/admin/ecommerce/prompt-defaults` |

`channels` 每项字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `protocol` | string | 协议，支持 OpenAI、Gemini、Grok2API、MiniMax、APIMart、KIE、MiMo |
| `name` | string | 渠道名称 |
| `baseUrl` | string | 渠道接口地址 |
| `apiKey` | string | 渠道密钥 |
| `models` | string[] | 该渠道可用模型 |
| `parameterTranslation` | string | 渠道自定传参转译原文，按 `models` 中的真实模型名称配置；空值沿用默认调用 |
| `weight` | number | 渠道权重；同一模型有多个可用渠道时按权重随机 |
| `enabled` | boolean | 是否启用 |
| `remark` | string | 备注 |

后端调用模型时，会从已启用、已配置 `baseUrl` 和 `apiKey`、且 `models` 包含目标模型的渠道中选择一个。

公开渠道信息增加 `parameterTranslationModels: string[]`，只列出已配置生成请求且允许使用的模型名，供前端选择调用入口；不公开转译原文和密钥。个人渠道原文随用户 `modelConfig.channelTranslations` 保存，每项为 `{ channelId, parameterTranslation }`，与外部 `localChannels` 的 URL、密钥和模型配置分开。

`promptSync` 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `enabled` | boolean | 是否开启定时同步，默认开启 |
| `cron` | string | Cron 表达式，默认每天 0 点 |
