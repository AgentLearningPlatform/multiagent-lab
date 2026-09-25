import { Handle, Position } from '@xyflow/react'
import type { NodeProps, NodeTypes } from '@xyflow/react'
import type { EditorFlowNode } from './model'

// ---------------------------------------------------------------------------
// GraphEditor 画布节点渲染（B1 拆分，REQ-145）
// ---------------------------------------------------------------------------

function EditorConceptNode({ data, selected }: NodeProps<EditorFlowNode>) {
  return (
    <div className={`onto-flow-node${selected ? ' selected' : ''}`} title={data.definition || data.label}>
      <Handle type="target" position={Position.Top} className="onto-flow-handle" />
      <span className="onto-flow-node-label">{data.label}</span>
      {data.count > 0 && <span className="onto-flow-node-badge">{data.count}</span>}
      <Handle type="source" position={Position.Bottom} className="onto-flow-handle" />
    </div>
  )
}

/** 实例节点：紫色调 + 所属概念脚标，可连线建实例关系 */
function EditorInstanceNode({ data, selected }: NodeProps<EditorFlowNode>) {
  return (
    <div className={`onto-flow-node onto-flow-node-inst${selected ? ' selected' : ''}`} title={`实例 · ${data.conceptName}`}>
      <Handle type="target" position={Position.Top} className="onto-flow-handle" />
      <span className="onto-flow-node-label">{data.label}</span>
      <span className="onto-flow-node-badge inst">{data.conceptName}</span>
      <Handle type="source" position={Position.Bottom} className="onto-flow-handle" />
    </div>
  )
}

export const nodeTypes: NodeTypes = { concept: EditorConceptNode, instance: EditorInstanceNode }
