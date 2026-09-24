import type { Agent } from '../api/types'

/** M13/D-O13 内置推理后端键（非此集合 = 自定义/外部部署后端，REQ-137 沿用登记 logo） */
export const BUILTIN_BACKENDS = new Set(['', 'eino-adk', 'claude-code', 'opencode', 'aider'])

/** REQ-137：非内置后端且已登记 logo_url → 返回图标 URL；否则 null（用默认 glyph） */
export function agentLogoOf(backend: string | undefined, logoUrl: string | undefined | null): string | null {
  if (BUILTIN_BACKENDS.has(backend ?? '')) return null
  const url = (logoUrl ?? '').trim()
  return url !== '' ? url : null
}

/** logo <img>（有 URL 时）或默认 glyph span */
export function AgentLogo({
  agent,
  backend,
  logoUrl,
  size = 22,
}: {
  agent?: Pick<Agent, 'inference_backend' | 'logo_url'> | null
  backend?: string
  logoUrl?: string | null
  size?: number
}) {
  const url = agent ? agentLogoOf(agent.inference_backend, agent.logo_url) : agentLogoOf(backend, logoUrl)
  if (url) {
    return (
      <img
        src={url}
        alt=""
        style={{ width: size, height: size, borderRadius: 5, objectFit: 'cover', display: 'inline-block', verticalAlign: 'middle' }}
        onError={(e) => {
          ;(e.target as HTMLImageElement).style.display = 'none'
        }}
      />
    )
  }
  return <span className="agent-glyph" />
}
