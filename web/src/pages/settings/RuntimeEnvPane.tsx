/**
 * 设置页「运行环境」分区（REQ-191/M31）：
 * - 智能体沙箱运行方式（进程内嵌 / Docker 容器 / K8s Pod / auto 自动检测）+ Docker/K8s 参数；
 * - K8s 访问认证：kubeconfig 路径 + context + namespace + 端点模式——认证跟随 kubeconfig
 *   标准机制，平台不自建 token 管理（D-O5 自研边界）；
 * - 连接测试（docker daemon ping / kubectl readyz，复用 REQ-190 Prober）；
 * - 本体引擎执行方式自「全局参数」迁入集中呈现（REQ-179/M-O16 runtime_config 存储 API 不变）。
 * 生效语义：DB 覆盖启动期 env（空字段=跟随环境）；保存后新 Run/新方案启动生效，
 * 运行中实例不受影响。
 */
import { useEffect, useState } from 'react'
import { Alert, Button, Card, Input, Select, Space, Spin, Tag, Typography } from 'antd'
import { api } from '../../api/client'
import type { RuntimeEnvPayload, RuntimeEnvSettings } from '../../api/client'
import { useUI } from '../../store/ui'

const MODE_OPTIONS = [
  { value: '', label: '跟随启动环境（env）' },
  { value: 'inprocess', label: '进程内嵌（无沙箱）' },
  { value: 'docker', label: 'Docker 容器' },
  { value: 'k8s', label: 'K8s Pod' },
  { value: 'auto', label: '自动检测（k8s → docker → 进程内）' },
]
const SCOPE_OPTIONS = [
  { value: '', label: '跟随启动环境（env）' },
  { value: 'agent', label: '按智能体常驻（复用）' },
  { value: 'run', label: '按运行独立（用后即清）' },
]
const ENDPOINT_OPTIONS = [
  { value: '', label: '跟随启动环境（env）' },
  { value: 'port-forward', label: 'port-forward（平台在集群外）' },
  { value: 'pod-ip', label: 'Pod IP（平台与集群同网）' },
]

export function RuntimeEnvPane() {
  const { showToast } = useUI()
  const [payload, setPayload] = useState<RuntimeEnvPayload | null>(null)
  const [form, setForm] = useState<RuntimeEnvSettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState<'docker' | 'k8s' | null>(null)
  const [testResult, setTestResult] = useState<Record<string, { ok: boolean; detail: string }>>({})
  const [err, setErr] = useState<string | null>(null)

  const load = () => {
    api
      .runtimeEnv()
      .then((p) => {
        setPayload(p)
        setForm(p.settings)
      })
      .catch((e: any) => setErr(e?.message ?? '加载失败'))
  }
  useEffect(load, [])

  if (err) {
    return <Alert type="error" showIcon title={err} action={<Button size="small" onClick={() => { setErr(null); load() }}>重试</Button>} />
  }
  if (!payload || !form) {
    return <div style={{ marginTop: 16 }}><Spin /></div>
  }

  const mode = form.sandbox_mode || ''
  const set = (patch: Partial<RuntimeEnvSettings>) => setForm({ ...form, ...patch })
  const save = async (): Promise<boolean> => {
    setSaving(true)
    try {
      const p = await api.setRuntimeEnv(form)
      setPayload(p)
      setForm(p.settings)
      showToast('运行环境配置已保存（新会话/新方案生效）')
      return true
    } catch (e: any) {
      showToast(e?.message ?? '保存失败', 'err')
      return false
    } finally {
      setSaving(false)
    }
  }
  const test = async (target: 'docker' | 'k8s') => {
    setTesting(target)
    try {
      const p = await api.setRuntimeEnv(form) // 测试前先落当前表单（改完即测直觉）
      setPayload(p)
      setForm(p.settings)
      const r = await api.testRuntimeEnv(target)
      setTestResult((m) => ({ ...m, [target]: { ok: r.ok, detail: r.detail } }))
    } catch (e: any) {
      showToast(e?.message ?? '测试失败', 'err')
    } finally {
      setTesting(null)
    }
  }
  const showDocker = mode === 'docker' || mode === 'auto'
  const showK8s = mode === 'k8s' || mode === 'auto'

  return (
    <>
      <div className="settings-head">
        <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>运行环境 · 智能体沙箱</Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          集中配置沙箱运行方式与 K8s 访问认证（REQ-191）；留空字段跟随平台启动环境（env 兜底，存量部署零影响）。
          保存后对<strong>新会话 / 新方案启动</strong>生效，运行中实例不受影响；K8s 认证跟随 kubeconfig 标准机制（平台不自建 token 管理）。
        </Typography.Paragraph>
      </div>

      <Card size="small" style={{ marginTop: 12, maxWidth: 720 }}>
        <Space direction="vertical" size={10} style={{ width: '100%' }}>
          <Space size={10} wrap>
            <Typography.Text strong>运行方式</Typography.Text>
            <Select style={{ width: 300 }} value={mode} onChange={(v) => set({ sandbox_mode: v as RuntimeEnvSettings['sandbox_mode'] })} options={MODE_OPTIONS} />
            {payload.sandbox_enabled ? <Tag color="green" style={{ margin: 0 }}>沙箱已启用（{payload.effective.sandbox_mode || 'docker'}）</Tag> : <Tag style={{ margin: 0 }}>进程内嵌（未启用沙箱）</Tag>}
          </Space>
          {mode !== 'inprocess' && (
            <>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>agentd 镜像</Typography.Text>
                <Input style={{ width: 320 }} value={form.sandbox_image} placeholder={payload.defaults.sandbox_image || '必填（docker/k8s/auto 共用）'} onChange={(e) => set({ sandbox_image: e.target.value })} />
              </Space>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>实例作用域</Typography.Text>
                <Select style={{ width: 220 }} value={form.sandbox_scope} onChange={(v) => set({ sandbox_scope: v as RuntimeEnvSettings['sandbox_scope'] })} options={SCOPE_OPTIONS} />
              </Space>
            </>
          )}
          {showDocker && (
            <>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>docker CLI</Typography.Text>
                <Input style={{ width: 320 }} value={form.docker_bin} placeholder={payload.defaults.docker_bin || '默认 PATH 查找'} onChange={(e) => set({ docker_bin: e.target.value })} />
              </Space>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>容器回访平台</Typography.Text>
                <Input style={{ width: 320 }} value={form.platform_url_external} placeholder={payload.defaults.platform_url_external || 'http://host.docker.internal:8080'} onChange={(e) => set({ platform_url_external: e.target.value })} />
              </Space>
            </>
          )}
          {showK8s && (
            <>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>K8s 访问 / 认证（auto 模式下与 Docker 候选并存，k8s 优先探测）</Typography.Text>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>kubeconfig</Typography.Text>
                <Input style={{ width: 380 }} value={form.k8s_kubeconfig} placeholder="默认 ~/.kube/config（认证随其内嵌凭据）" onChange={(e) => set({ k8s_kubeconfig: e.target.value })} />
              </Space>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>context</Typography.Text>
                <Input style={{ width: 180 }} value={form.k8s_context} placeholder={payload.defaults.k8s_context || '当前 context'} onChange={(e) => set({ k8s_context: e.target.value })} />
                <Typography.Text strong>namespace</Typography.Text>
                <Input style={{ width: 160 }} value={form.k8s_namespace} placeholder={payload.defaults.k8s_namespace || 'context 默认'} onChange={(e) => set({ k8s_namespace: e.target.value })} />
              </Space>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>端点模式</Typography.Text>
                <Select style={{ width: 260 }} value={form.k8s_endpoint_mode} onChange={(v) => set({ k8s_endpoint_mode: v as RuntimeEnvSettings['k8s_endpoint_mode'] })} options={ENDPOINT_OPTIONS} />
                <Typography.Text strong>Pod 回访平台</Typography.Text>
                <Input style={{ width: 220 }} value={form.platform_url_in_cluster} placeholder="集群内 service/节点地址" onChange={(e) => set({ platform_url_in_cluster: e.target.value })} />
              </Space>
              <Space size={10} wrap>
                <Typography.Text strong style={{ width: 90 }}>kubectl CLI</Typography.Text>
                <Input style={{ width: 320 }} value={form.kubectl_bin} placeholder={payload.defaults.kubectl_bin || '默认 PATH 查找'} onChange={(e) => set({ kubectl_bin: e.target.value })} />
              </Space>
            </>
          )}
          <Space size={10} wrap>
            <Button type="primary" loading={saving} onClick={save}>保存配置</Button>
            <Button loading={testing === 'docker'} onClick={() => test('docker')} disabled={mode === 'inprocess'}>测试 Docker</Button>
            <Button loading={testing === 'k8s'} onClick={() => test('k8s')} disabled={mode === 'inprocess'}>测试 K8s</Button>
            {testResult.docker && <Tag color={testResult.docker.ok ? 'green' : 'red'} style={{ margin: 0 }}>Docker：{testResult.docker.ok ? '可达' : testResult.docker.detail}</Tag>}
            {testResult.k8s && <Tag color={testResult.k8s.ok ? 'green' : 'red'} style={{ margin: 0 }}>K8s：{testResult.k8s.ok ? '可达' : testResult.k8s.detail}</Tag>}
          </Space>
        </Space>
      </Card>

      <div className="settings-head" style={{ marginTop: 20 }}>
        <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>本体引擎 · 执行方式</Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          本体运行方案的引擎执行方式（系统级配置，对所有方案生效；REQ-179/M-O16，D-O20）。切换后对下一次方案启动生效。
        </Typography.Paragraph>
      </div>
      <EngineExecMethodCard />
    </>
  )
}

/** 本体引擎执行方式（自「全局参数」分区迁入，REQ-179/M-O16 交付组件；runtime_config 存储 API 不变）。 */
function EngineExecMethodCard() {
  const [cfg, setCfg] = useState<{ execution_method: 'docker' | 'native' | 'k8s'; docker_available: boolean } | null>(null)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    api.runtimeConfig().then(setCfg).catch((e: any) => setErr(e?.message ?? '加载失败'))
  }, [])
  const set = (v: 'docker' | 'native' | 'k8s') => {
    setSaving(true)
    api
      .setRuntimeConfig(v)
      .then((r) => setCfg((c) => ({ execution_method: r.execution_method as any, docker_available: c?.docker_available ?? false })))
      .catch((e: any) => setErr(e?.message ?? '保存失败'))
      .finally(() => setSaving(false))
  }
  return (
    <>
      {err && <Alert type="error" showIcon style={{ marginTop: 12 }} title={err} closable onClose={() => setErr(null)} />}
      {!cfg ? (
        <div style={{ marginTop: 16 }}>{err ? null : <Spin />}</div>
      ) : (
        <Card size="small" style={{ marginTop: 12, maxWidth: 720 }}>
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space size={10} wrap>
              <Typography.Text strong>执行方式</Typography.Text>
              <Select
                style={{ width: 240 }}
                value={cfg.execution_method}
                onChange={(v) => set(v as 'docker' | 'native' | 'k8s')}
                options={[
                  { value: 'docker', label: 'docker 容器' },
                  { value: 'native', label: '内置二进制' },
                  { value: 'k8s', label: 'k8s（接口预留）' },
                ]}
              />
              {saving && <Spin size="small" />}
            </Space>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {cfg.execution_method === 'docker' && <>容器执行（oxigraph 官方镜像，数据目录挂载 + 端口映射）。当前 docker 可用性：<b style={{ color: cfg.docker_available ? '#16a34a' : '#dc2626' }}>{cfg.docker_available ? '可用' : '不可用（PATH 无 docker CLI 或守护进程未启动）'}</b>——不可用时方案启动会失败。</>}
              {cfg.execution_method === 'native' && <>内置二进制子进程执行（REQ-146 一键安装的引擎二进制，按方案端口本机监听）。引擎未安装时到「本体运行」栏一键安装。</>}
              {cfg.execution_method === 'k8s' && <>接口预留（复用 M10 10d K8sBackend 模式，随集群环境落地）——当前选择 k8s 时方案启动将报错提示，请在有集群环境前切换 docker 容器或内置二进制。</>}
            </Typography.Text>
          </Space>
        </Card>
      )}
    </>
  )
}
