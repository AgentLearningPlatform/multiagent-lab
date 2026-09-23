import type { InferenceBackendStatus } from '../api/types'

/** M13 §6.16：推理后端下拉选项——外部后端未发现（PATH 探测失败）时置灰不可选 */
export function inferenceBackendOptions(backends: InferenceBackendStatus[]) {
  return (backends ?? []).map((b) => ({
    value: b.name,
    label: b.default
      ? 'eino-adk（自研默认）'
      : b.available
        ? `${b.name}（已发现${b.version ? ` · ${b.version}` : ''}）`
        : `${b.name}（未发现）`,
    disabled: !b.default && !b.available,
  }))
}
