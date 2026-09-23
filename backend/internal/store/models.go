package store

// 领域模型（与 §5 表清单一一对应；JSON 字段以文本存储）。

// Agent 智能体配置实体。
type Agent struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	Instruction    string      `json:"instruction"`
	ModelConnID    *string     `json:"model_conn_id"` // 空 = 跟随全局默认（P1）
	Temperature    *float64    `json:"temperature"`
	MaxTokens      *int        `json:"max_tokens"`
	MaxIteration   int         `json:"max_iteration"`
	Tools          []string    `json:"tools"`
	Skills         []string    `json:"skills"`      // P2 生效
	MCPServers     []MCPServer `json:"mcp_servers"` // P2 生效
	RuntimeBackend   string      `json:"runtime_backend"`
	InferenceBackend string      `json:"inference_backend"` // M13 §6.16：空 = eino-adk 自研默认
	CreatedAt      string      `json:"created_at"`
	UpdatedAt      string      `json:"updated_at"`
}

// MCPServer Agent 级 MCP 端点（P2）。
type MCPServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ProjectFile 项目文件与对话产物元数据（M11 §5.2 project_file）。
type ProjectFile struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Name           string `json:"name"`
	Path           string `json:"path"` // 项目目录内相对路径
	Size           int64  `json:"size"`
	Mime           string `json:"mime,omitempty"`
	Source         string `json:"source"` // upload | artifact
	CreatedAt      string `json:"created_at,omitempty"`
}

// Project 多 Agent 项目。
type Project struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	CollabMode   string   `json:"collab_mode"`   // single | agent_as_tool | transfer
	WorkflowMode string   `json:"workflow_mode"` // free | sequential | parallel | loop (P1)
	Constraints  string   `json:"constraints"`   // 项目级统一约束（P1）
	LocalDir     string   `json:"local_dir"`     // 绑定的本地目录绝对路径（REQ-101 v0.17；空=未绑定）
	AgentIDs     []string `json:"agent_ids"`     // 成员 Agent
	Coordinator  string   `json:"coordinator"`   // 主 Agent（role=coordinator）
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// Conversation 对话（scope=agent 直聊 / scope=project 项目对话）。
type Conversation struct {
	ID               string  `json:"id"`
	Scope            string  `json:"scope"`
	AgentID          *string `json:"agent_id"`
	ProjectID        *string `json:"project_id"`
	Title            string  `json:"title"`
	KBID             *string `json:"kb_id"`
	EnableKB         bool    `json:"enable_kb"`
	RuntimeProfileID *string `json:"runtime_profile_id"` // 本体运行方案（外部引用，O-6）
	OntologyEnabled  bool    `json:"ontology_enabled"`
	// EnableSkills 会话级技能开关（nil=未指定：创建默认开、更新保留原值）
	EnableSkills *bool   `json:"enable_skills,omitempty"`
	TopK         int     `json:"top_k"`
	MinScore     float64 `json:"min_score"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// Message 消息（role: user/assistant/system/tool）。
type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	Meta           string `json:"meta,omitempty"` // JSON 字符串：agent 名、usage 等
	CreatedAt      string `json:"created_at"`
}

// RunEvent 过程事件（SSE 事件持久化，用于历史还原）。
type RunEvent struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	Type           string `json:"type"`
	Data           string `json:"data,omitempty"` // JSON 字符串
	CreatedAt      string `json:"created_at"`
}

// ModelConnection 模型连接（chat / embedding）。
type ModelConnection struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ConnType   string `json:"conn_type"` // chat | embedding
	Protocol   string `json:"protocol"`  // openai_compat
	BaseURL    string `json:"base_url"`
	ModelName  string `json:"model_name"`
	APIKeyHint string `json:"api_key_hint"` // 掩码，如 sk-****ab12
	HasKey     bool   `json:"has_key"`      // 是否已存 key（不回传明文）
	Enabled    bool   `json:"enabled"`
	IsDefault  bool   `json:"is_default"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	// 请求体携带、不落库不回显
	// ---- write-only 字段（请求可携带，响应不回传明文） ----
	APIKey string `json:"api_key,omitempty"`
	// CopyKeyFrom 指定源连接 ID：创建/更新时若未携带明文 api_key，则复用源连接已存密文。
	// 支撑「供应商 → 多模型」语义：Key 归属供应商，组内模型连接经此共享同一密文。
	CopyKeyFrom string `json:"copy_key_from,omitempty"`
}
