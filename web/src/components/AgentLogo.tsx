import type { Agent } from '../api/types'

/** M13/D-O13 内置推理后端键（非此集合 = 自定义/外部部署后端，REQ-137 沿用登记 logo） */
export const BUILTIN_BACKENDS = new Set(['', 'eino-adk', 'claude-code', 'opencode', 'aider', 'deepseek-harness'])

/** REQ-165：内置推理后端官方标识（运行上下文执行身份用；来源注记见 15 号登记簿——
 *  CloudWeGo/Anthropic/opencode/Aider/DeepSeek 官方站点或组织头像，web/public/logos/backend/） */
export const BACKEND_LOGO: Record<string, string> = {
  'eino-adk': '/logos/backend/eino-adk.png',
  'claude-code': '/logos/backend/claude-code.ico',
  'opencode': '/logos/backend/opencode.png',
  'aider': '/logos/backend/aider.png',
  'deepseek-harness': '/logos/backend/deepseek-harness.ico',
}

/** REQ-165：运行上下文（执行身份）的图标 URL——内置后端取官方标识，非内置沿用登记 logo_url */
export function backendLogoOf(backend: string | undefined, logoUrl: string | undefined | null): string | null {
  const b = backend ?? ''
  if (BUILTIN_BACKENDS.has(b) && b !== '') return BACKEND_LOGO[b] ?? null
  const url = (logoUrl ?? '').trim()
  return url !== '' ? url : null
}

/** REQ-137：非内置后端且已登记 logo_url → 返回图标 URL；否则 null（用默认 glyph） */
export function agentLogoOf(backend: string | undefined, logoUrl: string | undefined | null): string | null {
  if (BUILTIN_BACKENDS.has(backend ?? '')) return null
  const url = (logoUrl ?? '').trim()
  return url !== '' ? url : null
}

/**
 * logo <img>（有 URL 时）或默认 glyph span。
 * REQ-165 双态渲染：
 *   - context='identity'（默认）：Agent 身份——导航树等处恒用 glyph/登记 logo，不随后端切换；
 *   - context='runtime'：执行身份——对话/详情等运行上下文按推理后端切换官方标识
 *     （内置后端=官方 logo；自定义/外部部署沿用登记 logo_url；eino-adk 亦显示标识）。
 */
export function AgentLogo({
  agent,
  backend,
  logoUrl,
  size = 22,
  context = 'identity',
}: {
  agent?: Pick<Agent, 'inference_backend' | 'logo_url'> | null
  backend?: string
  logoUrl?: string | null
  size?: number
  context?: 'identity' | 'runtime'
}) {
  const inf = agent ? agent.inference_backend : backend
  const url =
    context === 'runtime'
      ? backendLogoOf(inf, agent ? agent.logo_url : logoUrl)
      : agent
        ? agentLogoOf(agent.inference_backend, agent.logo_url)
        : agentLogoOf(backend, logoUrl)
  if (url) {
    return (
      <img
        src={url}
        alt=""
        title={context === 'runtime' ? `推理后端：${inf || 'eino-adk'}` : undefined}
        style={{ width: size, height: size, borderRadius: 5, objectFit: 'cover', display: 'inline-block', verticalAlign: 'middle' }}
        onError={(e) => {
          ;(e.target as HTMLImageElement).style.display = 'none'
        }}
      />
    )
  }
  return <span className="agent-glyph" />
}
