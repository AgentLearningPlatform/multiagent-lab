package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// REQ-216 增量轮①（2026-09-30 复查）：伴生图持久化——结果集快照 + 方案重建回灌。
// 风险面：运行平面方案每次 Start 全量重建引擎数据目录（oxigraph RemoveAll + 重灌本体
// TTL），伴生子图随宿主方案重建丢失。方案：伴生图以「引擎查询结果集原样快照」（JSON，
// 非候选推导——invalidAt 失效化标记/autoConfirmed 等派生三元组一并保留）持久化在伴生
// 自有目录（data/companion-snapshots/，运行平面不可触达）；写路径（确认入图/迁移复制）
// 同步刷新快照；读/写路径 Ensure 后做重建检测——子图为空而快照非空 → 分批 INSERT DATA
// 回灌（RDF 集合语义幂等）。快照缺失/空图零开销（一次 stat）。
// 诚实边界：进程在「引擎写入成功、快照落盘前」崩溃的毫秒级窗口可能丢最近一次确认
// （伴生写路径为低频人工确认，接受并注记）。
// ---------------------------------------------------------------------------

// snapshotDir 快照目录（COMPANION_SNAPSHOT_DIR 覆盖；backend cwd=backend/）。
func snapshotDir() string {
	if v := os.Getenv("COMPANION_SNAPSHOT_DIR"); v != "" {
		return v
	}
	return filepath.Join("data", "companion-snapshots")
}

func snapshotPath(ontologyID string) string {
	return filepath.Join(snapshotDir(), "ont-"+ontologyID+".sparql.json")
}

// snapshotRefresh 全量导出子图 → 快照文件（原子写：tmp + rename）。
// REQ-227④ 对账头：文件为信封 {"version":1,"count":N,"results":<SPARQL JSON>}——回灌前
// 校验 count 与 bindings 数一致，不一致告警不回灌（防半截写/损坏静默空图）。
// 引擎查询失败/写盘失败仅日志（快照是持久性保障不是功能正确性依赖——下次写路径会再刷）。
func (s *Service) snapshotRefresh(ctx context.Context, ontologyID, base string) {
	raw, err := s.Plans.Query(ctx, base, SelectGraphAllTriples(ontologyID))
	if err != nil {
		log.Printf("[companion] 快照导出查询失败（本体 %s，下次写路径重试）: %v", ontologyID, err)
		return
	}
	var probe struct {
		Results struct {
			Bindings []json.RawMessage `json:"bindings"`
		} `json:"results"`
	}
	_ = json.Unmarshal(raw, &probe)
	envelope, err := json.Marshal(map[string]any{"version": 1, "count": len(probe.Results.Bindings), "results": json.RawMessage(raw)})
	if err != nil {
		log.Printf("[companion] 快照信封编码失败（本体 %s）: %v", ontologyID, err)
		return
	}
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[companion] 快照目录创建失败: %v", err)
		return
	}
	tmp := snapshotPath(ontologyID) + ".tmp"
	if err := os.WriteFile(tmp, envelope, 0o644); err != nil {
		log.Printf("[companion] 快照写盘失败（本体 %s）: %v", ontologyID, err)
		return
	}
	if err := os.Rename(tmp, snapshotPath(ontologyID)); err != nil {
		log.Printf("[companion] 快照原子替换失败（本体 %s）: %v", ontologyID, err)
	}
}

// snapshotDelete 快照删除（本体级清空伴生图 / 本体删除后调用）。
func snapshotDelete(ontologyID string) {
	_ = os.Remove(snapshotPath(ontologyID))
}

// ensureInflated 方案重建检测与回灌。触发条件：快照存在 && 该本体未在当前引擎基址上
// 完成过校验（进程重启或 Ensure 解析到新基址/新方案后首访一次）；子图为空且快照非空
// → 回灌。非空 = 数据在位（含首次 Ensure 刚建好的空宿主——快照为空时 stat 已拦截）。
func (s *Service) ensureInflated(ctx context.Context, ontologyID, base string) {
	if _, err := os.Stat(snapshotPath(ontologyID)); err != nil {
		return // 无快照（该本体从未确认过入图，或已被本体级清空）——零开销返回
	}
	s.infMu.Lock()
	if s.inflatedBase[ontologyID] == base {
		s.infMu.Unlock()
		return
	}
	s.infMu.Unlock()

	n := s.countGraphTriples(ctx, ontologyID, base)
	if n == 0 {
		raw, err := os.ReadFile(snapshotPath(ontologyID))
		if err == nil {
			triples, perr := parseSnapshotFile(raw)
			if perr != nil {
				// REQ-227④：快照损坏告警不静默（此前 parseTriples 失败静默跳过=图空无感知）
				log.Printf("[companion] 快照解析失败（本体 %s，疑似损坏不回灌）: %v", ontologyID, perr)
			} else if len(triples) > 0 {
				for i := 0; i < len(triples); i += legacyCopyBatch {
					end := i + legacyCopyBatch
					if end > len(triples) {
						end = len(triples)
					}
					if err := s.Plans.Update(ctx, base, insertTriplesData(GraphURI(ontologyID), triples[i:end])); err != nil {
						log.Printf("[companion] 快照回灌失败（本体 %s，下次重检测）: %v", ontologyID, err)
						return // 不标记——下次访问重试
					}
				}
				log.Printf("[companion] 伴生子图自快照回灌 %d 三元组（宿主方案重建检测，本体 %s）", len(triples), ontologyID)
			}
		}
	}
	s.infMu.Lock()
	s.inflatedBase[ontologyID] = base
	s.infMu.Unlock()
}

// countGraphTriples 子图三元组计数（重建检测探针）。
func (s *Service) countGraphTriples(ctx context.Context, ontologyID, base string) int {
	raw, err := s.Plans.Query(ctx, base, CountGraphTriples(ontologyID))
	if err != nil {
		return -1 // 查询失败按「未知」处理：不回灌也不标记（下次重检测）
	}
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &res) != nil || len(res.Results.Bindings) == 0 {
		return 0
	}
	n := 0
	_, _ = fmt.Sscanf(res.Results.Bindings[0]["n"].Value, "%d", &n)
	return n
}

// OnOntologyDeleted 本体删除后的伴生侧清理（快照 + 端点缓存；api 删除拦截成功路径调用）。
func (s *Service) OnOntologyDeleted(ontologyID string) {
	snapshotDelete(ontologyID)
	s.Plans.Invalidate(ontologyID)
	s.invalidateLabelCache(ontologyID)
}


// parseSnapshotFile 快照文件 → 三元组（REQ-227④ 信封格式带 count 对账；兼容旧裸数组格式）。
func parseSnapshotFile(raw []byte) ([]migrateTriple, error) {
	var envelope struct {
		Version int             `json:"version"`
		Count   int             `json:"count"`
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Version != 1 || envelope.Results == nil {
		// 旧格式（裸 SPARQL JSON）兼容
		return parseTriples(raw)
	}
	var probe struct {
		Results struct {
			Bindings []json.RawMessage `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(envelope.Results, &probe); err != nil {
		return nil, fmt.Errorf("信封 results 解析失败: %w", err)
	}
	if len(probe.Results.Bindings) != envelope.Count {
		return nil, fmt.Errorf("快照对账不一致：count=%d 实际=%d（疑似半截写/损坏）", envelope.Count, len(probe.Results.Bindings))
	}
	return parseTriples(envelope.Results)
}
