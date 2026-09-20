import App from 'antd/es/app'
import type { MessageInstance } from 'antd/es/message/interface'
import type { ModalStaticFunctions } from 'antd/es/modal/confirm'

/** antd App 上下文桥（App.useApp 的 message/modal 提升为模块级，供非组件层使用） */
export const antdHolder: { message: MessageInstance | null; modal: Omit<ModalStaticFunctions, 'warn'> | null } = {
  message: null,
  modal: null,
}

/** 挂载在 main.tsx 的 AntdApp 内部，把上下文实例写入 antdHolder */
export function AntdBridge() {
  const { message, modal } = App.useApp()
  antdHolder.message = message
  antdHolder.modal = modal
  return null
}

/** 轻提示（成功/错误） */
export function toast(msg: string, kind: 'ok' | 'err' = 'ok') {
  if (kind === 'ok') antdHolder.message?.success(msg)
  else antdHolder.message?.error(msg)
}

/** 危险操作二次确认 */
export function confirmAction(title: string, content: string | undefined, onOk: () => void) {
  antdHolder.modal?.confirm({
    title,
    content,
    okText: '确定',
    okButtonProps: { danger: true },
    cancelText: '取消',
    onOk,
  })
}
