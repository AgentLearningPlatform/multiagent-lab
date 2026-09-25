import { useEffect, useRef } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { EditorView } from '@codemirror/view'
import { json, jsonParseLinter } from '@codemirror/lang-json'
import { linter } from '@codemirror/lint'

// ---------------------------------------------------------------------------
// REQ-145（M22 一批 A1/A2）：本体模块 JSON 编辑统一 CodeMirror——JSON 语言模式
// 语法高亮 + jsonParseLinter 行内错误标记。组件只管呈现；合法性判定仍由调用方
// 在保存路径上 try/catch 兜底（GraphEditor 属性编辑防崩红线不变）。
// 同步模型：内部输入经 onChange 上抛（半受控）；仅当外部 value 显式变化
// （重新加载 / 切换本体）时整体替换 doc——@uiw 的 value prop 同步在打字锁
// 挂起下会丢外部更新，这里用 view 引用显式 dispatch，行为确定。
// ---------------------------------------------------------------------------

export default function JsonEditor({
  value,
  onChange,
  readOnly = false,
  height = '440px',
  placeholder,
}: {
  /** Form.Item 受控注入时可省略（CodeMirror 需非 undefined） */
  value?: string
  onChange?: (v: string) => void
  readOnly?: boolean
  height?: string
  placeholder?: string
}) {
  const viewRef = useRef<EditorView | null>(null)
  const lastExternalRef = useRef(value)

  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const isExternalChange = value !== lastExternalRef.current
    lastExternalRef.current = value
    if (isExternalChange && value !== view.state.doc.toString()) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value ?? '' },
      })
    }
  }, [value])

  const extensions = [json(), linter(jsonParseLinter()), EditorView.lineWrapping]
  return (
    <div className="onto-cm-wrap">
      <CodeMirror
        value={value ?? ''}
        height={height}
        readOnly={readOnly}
        editable={!readOnly}
        placeholder={placeholder}
        basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: !readOnly }}
        extensions={extensions}
        onChange={onChange}
        onCreateEditor={(view) => {
          viewRef.current = view
          lastExternalRef.current = value
        }}
      />
    </div>
  )
}
