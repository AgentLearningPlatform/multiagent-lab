import { useEffect, useState } from 'react'
import { Input, Modal } from 'antd'

/** 通用「输入名称」小弹窗（替代 window.prompt，原型 06 §5 对话框规范） */
export default function NameModal({
  open,
  title,
  placeholder,
  okText = '创建',
  onCancel,
  onSubmit,
}: {
  open: boolean
  title: string
  placeholder?: string
  okText?: string
  onCancel: () => void
  onSubmit: (name: string) => void
}) {
  const [name, setName] = useState('')

  useEffect(() => {
    if (open) setName('')
  }, [open])

  const submit = () => {
    const v = name.trim()
    if (!v) return
    onSubmit(v)
  }

  return (
    <Modal
      open={open}
      title={title}
      okText={okText}
      cancelText="取消"
      okButtonProps={{ disabled: !name.trim() }}
      onOk={submit}
      onCancel={onCancel}
      destroyOnHidden
      width={400}
    >
      <Input
        autoFocus
        value={name}
        placeholder={placeholder ?? '请输入名称'}
        onChange={(e) => setName(e.target.value)}
        onPressEnter={submit}
        maxLength={60}
        showCount
      />
    </Modal>
  )
}
