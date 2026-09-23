/**
 * REQ-105（M12）：国内常见模型供应商访问配置预设（前端静态清单）。
 * 需求说明允许「前端静态 / 后端种子」二选一实现——取前端静态：清单纯数据驱动，
 * 增删厂商只改本数组，不动交互代码（REQ-77「候选清单开放性」同思路）。
 *
 * 每条预填：协议（openai_compat）+ Base URL + 建议首个模型名（REQ-106 自动填充，
 * 用户仅需补 API Key；Key 归属提供商，模型可经 REQ-48 自动发现批量拉取）。
 * 接入点与模型名为常见文档口径，厂商侧随版本演进可能调整——预设仅是快捷起点，表单内可改。
 */

export interface ProviderPreset {
  key: string
  /** 提供商展示名（作为连接名前缀 `{提供商}·{模型}`） */
  name: string
  baseUrl: string
  /** 建议首个模型名 */
  defaultModel: string
  /** 该厂商主打连接类型（个别厂商两条预设分别给 chat / embedding） */
  connType: 'chat' | 'embedding'
  /** 控制台地址（提示去申请 API Key） */
  console?: string
}

export const PROVIDER_PRESETS: ProviderPreset[] = [
  {
    key: 'deepseek',
    name: 'DeepSeek 深度求索',
    baseUrl: 'https://api.deepseek.com/v1',
    defaultModel: 'deepseek-chat',
    connType: 'chat',
    console: 'https://platform.deepseek.com',
  },
  {
    key: 'bailian',
    name: '阿里百炼 DashScope',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    defaultModel: 'qwen-plus',
    connType: 'chat',
    console: 'https://bailian.console.aliyun.com',
  },
  {
    key: 'qianfan',
    name: '百度千帆',
    baseUrl: 'https://qianfan.baidubce.com/v2',
    defaultModel: 'ernie-4.5-turbo-128k',
    connType: 'chat',
    console: 'https://console.bce.baidu.com/qianfan',
  },
  {
    key: 'zhipu',
    name: '智谱 GLM',
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    defaultModel: 'glm-4.5',
    connType: 'chat',
    console: 'https://open.bigmodel.cn',
  },
  {
    key: 'kimi',
    name: '月之暗面 Kimi',
    baseUrl: 'https://api.moonshot.cn/v1',
    defaultModel: 'kimi-k2-0905-preview',
    connType: 'chat',
    console: 'https://platform.moonshot.cn',
  },
  {
    key: 'siliconflow',
    name: '硅基流动 SiliconFlow',
    baseUrl: 'https://api.siliconflow.cn/v1',
    defaultModel: 'deepseek-ai/DeepSeek-V3',
    connType: 'chat',
    console: 'https://cloud.siliconflow.cn',
  },
  {
    key: 'minimax',
    name: 'MiniMax',
    baseUrl: 'https://api.minimaxi.com/v1',
    defaultModel: 'MiniMax-M2',
    connType: 'chat',
    console: 'https://platform.minimaxi.com',
  },
  {
    key: 'spark',
    name: '讯飞星火 Spark',
    baseUrl: 'https://spark-api-open.xf-yun.com/v1',
    defaultModel: '4.0Ultra',
    connType: 'chat',
    console: 'https://console.xfyun.cn',
  },
  {
    key: 'qianfan-embedding',
    name: '百度千帆（向量）',
    baseUrl: 'https://qianfan.baidubce.com/v2',
    defaultModel: 'embeddings-v1',
    connType: 'embedding',
    console: 'https://console.bce.baidu.com/qianfan',
  },
]
