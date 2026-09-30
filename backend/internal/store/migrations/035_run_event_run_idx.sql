-- REQ-217③/M48：run_event(run_id) 索引——轨迹面板按运行过滤与「重放此运行」取数
--（存量事件此前全量裸奔按 conversation 扫，补运行维度索引 + 分页保护）
CREATE INDEX IF NOT EXISTS idx_run_event_run ON run_event(run_id, created_at);
