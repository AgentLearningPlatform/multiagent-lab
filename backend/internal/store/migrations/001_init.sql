-- 001 主平台基础表：agent / project / project_agent / conversation / message / run_event
CREATE TABLE IF NOT EXISTS agent (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  instruction TEXT NOT NULL DEFAULT '',
  model_conn_id TEXT,
  temperature REAL,
  max_tokens INTEGER,
  max_iteration INTEGER NOT NULL DEFAULT 25,
  tools TEXT NOT NULL DEFAULT '[]',
  skills TEXT NOT NULL DEFAULT '[]',
  mcp_servers TEXT NOT NULL DEFAULT '[]',
  runtime_backend TEXT NOT NULL DEFAULT 'inprocess',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS project (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  collab_mode TEXT NOT NULL DEFAULT 'agent_as_tool',
  workflow_mode TEXT NOT NULL DEFAULT 'free',
  constraints TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS project_agent (
  project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'member',
  PRIMARY KEY (project_id, agent_id)
);

CREATE TABLE IF NOT EXISTS conversation (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL CHECK(scope IN ('agent','project')),
  agent_id TEXT REFERENCES agent(id) ON DELETE CASCADE,
  project_id TEXT REFERENCES project(id) ON DELETE CASCADE,
  title TEXT NOT NULL DEFAULT '新对话',
  kb_id TEXT,
  enable_kb INTEGER NOT NULL DEFAULT 0,
  runtime_profile_id TEXT,
  ontology_enabled INTEGER NOT NULL DEFAULT 0,
  top_k INTEGER NOT NULL DEFAULT 4,
  min_score REAL NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS message (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  meta TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_message_conv ON message(conversation_id, created_at);

CREATE TABLE IF NOT EXISTS run_event (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL,
  type TEXT NOT NULL,
  data TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_run_event_conv ON run_event(conversation_id, created_at);
