-- 007 项目绑定本地目录（REQ-101 v0.17）：local_dir 存绝对路径，空字符串 = 未绑定。
-- 绑定后 M11 文件 API 与 save_file 工具的作用根从 FilesRoot/{projectID} 切换为该目录。
ALTER TABLE project ADD COLUMN local_dir TEXT NOT NULL DEFAULT '';
