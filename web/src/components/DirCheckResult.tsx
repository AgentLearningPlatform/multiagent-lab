import { Alert, Space, Tag } from 'antd'
import type { DirValidation } from '../api/types'

/**
 * REQ-133：validate-dir 分字段直连展示（路径格式 / 后端可达 / 存在 / 目录 / Git）。
 * 前端不按本机 OS 规则推断路径形态，一切以后端分字段结果为准；
 * 后端不可达（远程部署：Linux 后端 + Windows 本机目录）时存在性未知，
 * 不再显示「不存在/非目录」误报，提示按远程目录口径理解。
 */
export default function DirCheckResult({ result }: { result: DirValidation }) {
  if (!result.format_ok) {
    return <Alert type="error" showIcon message="目录检测未通过" description={result.error || '路径不是绝对路径'} />
  }
  return (
    <div className="dir-check">
      {result.error && (
        <Alert
          type="warning"
          showIcon
          message="部分信息获取失败"
          description={result.error}
          style={{ marginBottom: 6 }}
        />
      )}
      <Space size={6} wrap>
        <Tag color="green" style={{ margin: 0 }}>
          路径格式合法
        </Tag>
        {!result.reachable && (
          <>
            <Tag color="blue" style={{ margin: 0 }}>
              后端不可达（远程目录）
            </Tag>
            <span className="dir-check-hint">
              该目录不在部署主机上：存在性未核实；文件工具将按此路径在部署主机寻址（挂载/共享后可用）。
            </span>
          </>
        )}
        {result.reachable && (
          <>
            <Tag color={result.exists ? 'green' : 'red'} style={{ margin: 0 }}>
              {result.exists ? '存在' : '不存在'}
            </Tag>
            {result.exists && (
              <Tag color={result.is_dir ? 'green' : 'red'} style={{ margin: 0 }}>
                {result.is_dir ? '目录' : '非目录'}
              </Tag>
            )}
            {result.exists && result.is_dir && (
              <>
                {result.is_git ? (
                  <>
                    <Tag color="blue" style={{ margin: 0 }}>
                      分支 {result.git_branch || '—'}
                    </Tag>
                    <Tag style={{ margin: 0 }}>{(result.git_commit || '').slice(0, 10) || '—'}</Tag>
                    {result.git_dirty ? (
                      <Tag color="orange" style={{ margin: 0 }}>
                        已修改
                      </Tag>
                    ) : (
                      <Tag color="green" style={{ margin: 0 }}>
                        干净
                      </Tag>
                    )}
                  </>
                ) : (
                  <Tag style={{ margin: 0 }}>非 Git 仓库</Tag>
                )}
              </>
            )}
          </>
        )}
      </Space>
    </div>
  )
}
