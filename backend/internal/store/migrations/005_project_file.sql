-- 005 项目文件与对话产物（v0.6 §5.2，M11 P3）
CREATE TABLE IF NOT EXISTS project_file (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES project(id) ON DELETE CASCADE,
  conversation_id TEXT,                   -- 对话产物归属（可空=项目文件）
  name TEXT NOT NULL, path TEXT NOT NULL, -- 本地项目目录相对路径
  size INTEGER, mime TEXT, source TEXT,   -- upload|artifact
  created_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_project_file_project ON project_file(project_id);
