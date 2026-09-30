package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ---------------------------------------------------------------------------
// REQ-216/M47 批次一④ 存量迁移（启动幂等一次性，两段）：
//   A. 绑定回填——companion_ontology=1（旧 bool 列）但未绑定本体的 agent，经构建平面
//      创建空本体「{agent 名}的伴生本体」并绑定（companion_ontology_id）。
//   B. 图跨实例复制——旧 :9199 实例 agt-{agentID} 图 SPARQL 层复制到宿主方案引擎
//      ont-{ontologyID} 图，随后旧图 DROP、旧实例停止（内置实例退役）。
// companion_meta 标记幂等；构建平面/引擎不在位 → 返回错误由调用方日志告警，下次启动重试
// （REQ-211 迁移同手法：候选数据在 SQLite 无损，图面推迟可见）。
// ---------------------------------------------------------------------------

const (
	metaBinding216 = "bindings_migrated_216"
	metaGraphs216  = "agtgraphs_migrated_216"
	// legacyCopyBatch 图复制分批 INSERT 大小（三元组/批——SQL 语句体积可控）。
	legacyCopyBatch = 200
)

// migrateTriple 旧图复制行（s/p/o 原文——IRI 或字面量的 Turtle 序列化在写入侧重排）。
type migrateTriple struct{ S, P, O string }

// MigrateAgentGraphsToOntology REQ-216 存量迁移入口（NewServer 启动调用；幂等三段）：
// A 绑定回填 → B agent 图跨实例复制 → C conv 图收敛（REQ-211 标记缺失的存量库兜底，
// 直落本体伴生子图）。
func (s *Service) MigrateAgentGraphsToOntology(ctx context.Context) error {
	if s == nil || s.Store == nil || s.Plans == nil {
		return nil
	}
	if err := s.migrateBindings(ctx); err != nil {
		return err
	}
	if err := s.migrateGraphs(ctx); err != nil {
		return err
	}
	return s.migrateConvGraphs(ctx)
}

// migrateConvGraphs 段 C（REQ-211 兼容兜底）：旧 conv-{id} 图收敛到所属 agent 的本体伴生子图
// （标记 convgraphs_migrated 已落 = 空转；agent 已删/未绑定的孤儿图按无主清理 DROP）。
func (s *Service) migrateConvGraphs(ctx context.Context) error {
	if done, _ := s.Store.GetCompanionMeta("convgraphs_migrated"); done == "1" {
		return nil
	}
	sources, err := s.Store.ListCompanionGraphSources()
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		_ = s.Store.SetCompanionMeta("convgraphs_migrated", "1")
		return nil
	}
	legacy := NewLegacyInstance("", 0)
	if !legacy.Reachable(ctx) {
		if legacyDirEmpty(legacy.DataDir) {
			_ = s.Store.SetCompanionMeta("convgraphs_migrated", "1")
			return nil
		}
		if _, err := legacy.Ensure(ctx); err != nil {
			return fmt.Errorf("伴生引擎不可用，conv 图收敛推迟（下次启动重试）: %w", err)
		}
	}
	ep, err := legacy.Ensure(ctx)
	if err != nil {
		return err
	}
	migrated := 0
	var firstErr error
	for _, src := range sources {
		from := LegacyConvGraphURI(src.ConversationID)
		agent, aerr := s.Store.GetAgent(src.AgentID)
		ontID := ""
		if aerr == nil {
			ontID = boundOntology(agent)
		}
		if ontID == "" {
			// 无主/未绑定：旧图按无主清理（候选数据随 agent 删除级联，无可迁移归属）
			if err := legacy.Update(ctx, fmt.Sprintf("DROP SILENT GRAPH <%s>", from)); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}
			continue
		}
		if err := s.copyGraphInto(ctx, legacy, ep, from, GraphURI(ontID), ontID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("[companion] conv 图收敛失败（%s）: %v", from, err)
			continue
		}
		migrated++
	}
	legacy.Stop()
	if firstErr != nil {
		return firstErr
	}
	if err := s.Store.SetCompanionMeta("convgraphs_migrated", "1"); err != nil {
		return err
	}
	log.Printf("[companion] conv 图收敛完成：%d 个会话图已并入本体伴生子图（DROP 旧 conv 图）", migrated)
	return nil
}

// copyGraph 旧实例 from 图全量复制到宿主方案引擎 to 图（分批 INSERT；空图跳写）。
func (s *Service) copyGraphInto(ctx context.Context, legacy *LegacyInstance, ep, from, to, ontologyID string) error {
	raw, err := legacy.Query(ctx, ep, fmt.Sprintf(`SELECT ?s ?p ?o WHERE { GRAPH <%s> { ?s ?p ?o } } LIMIT 100000`, from))
	if err != nil {
		return fmt.Errorf("旧图读取失败（%s）: %w", from, err)
	}
	triples, err := parseTriples(raw)
	if err != nil {
		return fmt.Errorf("旧图结果解析失败（%s）: %w", from, err)
	}
	if len(triples) == 0 {
		return nil
	}
	base, err := s.Plans.EnsureHostPlan(ctx, ontologyID)
	if err != nil {
		return fmt.Errorf("宿主方案确保失败: %w", err)
	}
	for i := 0; i < len(triples); i += legacyCopyBatch {
		end := i + legacyCopyBatch
		if end > len(triples) {
			end = len(triples)
		}
		if err := s.Plans.Update(ctx, base, insertTriplesData(to, triples[i:end])); err != nil {
			return fmt.Errorf("新图写入失败（%s）: %w", to, err)
		}
	}
	log.Printf("[companion] 图已复制 %d 三元组（%s → %s）", len(triples), from, to)
	return nil
}

// migrateBindings 段 A：绑定回填（构建平面可达才做；全量完成落 meta 标记）。
func (s *Service) migrateBindings(ctx context.Context) error {
	if done, _ := s.Store.GetCompanionMeta(metaBinding216); done == "1" {
		return nil
	}
	legacy, err := s.Store.ListLegacyCompanionAgents()
	if err != nil {
		return err
	}
	bound := 0
	for _, a := range legacy {
		// 幂等：同名伴生本体已建过（上次中断）→ 复用；否则创建
		oid, err := s.findOrCreateCompanionOntology(ctx, a.Name)
		if err != nil {
			return fmt.Errorf("绑定回填失败（agent %s，构建平面不可达或创建受阻；下次启动重试）: %w", a.ID, err)
		}
		if err := s.Store.SetAgentCompanionBinding(a.ID, oid); err != nil {
			return err
		}
		bound++
	}
	if err := s.Store.SetCompanionMeta(metaBinding216, "1"); err != nil {
		return err
	}
	if bound > 0 {
		log.Printf("[companion] REQ-216 绑定回填完成：%d 个开启伴生的 agent 已绑定伴生本体", bound)
	}
	return nil
}

// findOrCreateCompanionOntology 按名查找既有伴生本体（上次中断复用），无则创建空本体。
func (s *Service) findOrCreateCompanionOntology(ctx context.Context, agentName string) (string, error) {
	name := CompanionOntologyName(agentName)
	if oid, err := s.Plans.FindOntologyByName(ctx, name); err == nil && oid != "" {
		// 增量轮⑤：同名复用收紧——须同时命中迁移创建的描述标记，防误绑用户同名本体
		if _, desc, derr := s.Plans.OntologyInfo(ctx, oid); derr == nil && strings.Contains(desc, "伴生本体（REQ-216") {
			return oid, nil
		}
	}
	return s.Plans.CreateEmptyOntology(ctx, name, "伴生本体（REQ-216 绑定回填自动创建；对话生长产物归属容器）")
}

// companionOntologyName 伴生本体建议名（绑定交互同口径；导出供 api 层复用）。
func CompanionOntologyName(agentName string) string {
	n := strings.TrimSpace(agentName)
	if n == "" {
		n = "未命名智能体"
	}
	return n + "的伴生本体"
}

// migrateGraphs 段 B：旧实例 agt 图 → 宿主方案 ont 图（逐 agent；全部完成后旧实例停止+落标记）。
func (s *Service) migrateGraphs(ctx context.Context) error {
	if done, _ := s.Store.GetCompanionMeta(metaGraphs216); done == "1" {
		retireLegacyProcess() // 幂等清扫：标记已落仍回收可能残留的孤儿实例（每次启动廉价 no-op）
		return nil
	}
	agents, err := s.Store.ListCompanionBoundAgents()
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		_ = s.Store.SetCompanionMeta(metaGraphs216, "1")
		return nil
	}
	legacy := NewLegacyInstance("", 0)
	// 旧实例不存活且数据目录为空 = 无存量可迁（内置实例退役后常态）→ 直接落标记。
	if !legacy.Reachable(ctx) {
		if legacyDirEmpty(legacy.DataDir) {
			retireLegacyProcess()
			_ = s.Store.SetCompanionMeta(metaGraphs216, "1")
			return nil
		}
		if _, err := legacy.Ensure(ctx); err != nil {
			return fmt.Errorf("旧伴生实例不可用，图跨实例迁移推迟（下次启动重试）: %w", err)
		}
	}
	ep, err := legacy.Ensure(ctx)
	if err != nil {
		return err
	}
	migrated := 0
	var firstErr error
	touched := map[string]string{} // REQ-216 增量①：迁移落图的本体 → 引擎基址（迁移后刷快照）
	for _, a := range agents {
		if err := s.migrateOneAgentGraph(ctx, legacy, ep, a.ID, a.CompanionOntologyID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("[companion] REQ-216 图迁移失败（agent %s）: %v", a.ID, err)
			continue
		}
		if base, berr := s.Plans.EnsureHostPlan(ctx, a.CompanionOntologyID); berr == nil {
			touched[a.CompanionOntologyID] = base
		}
		migrated++
	}
	legacy.Stop() // 迁移即退役：无论成败不再持有旧实例（失败的 agent 下次启动重试）
	retireLegacyProcess()
	for oid, base := range touched {
		s.snapshotRefresh(ctx, oid, base) // 迁移复制的数据同样入快照（持久性保障）
	}
	if firstErr != nil {
		return firstErr
	}
	if err := s.Store.SetCompanionMeta(metaGraphs216, "1"); err != nil {
		return err
	}
	log.Printf("[companion] REQ-216 图跨实例迁移完成：%d 个 agent 图已复制到宿主方案引擎伴生子图并清空旧图", migrated)
	return nil
}

// migrateOneAgentGraph 单 agent：SELECT 旧图全量 → 分批 INSERT 新图 → DROP 旧图。
// 空图跳过写入只 DROP（幂等：ADD 语义由显式复制承担，重复启动因标记不重入）。
func (s *Service) migrateOneAgentGraph(ctx context.Context, legacy *LegacyInstance, ep, agentID, ontologyID string) error {
	from, to := LegacyAgentGraphURI(agentID), GraphURI(ontologyID)
	if err := s.copyGraphInto(ctx, legacy, ep, from, to, ontologyID); err != nil {
		return err
	}
	if err := legacy.Update(ctx, fmt.Sprintf("DROP SILENT GRAPH <%s>", from)); err != nil {
		return fmt.Errorf("旧图清理失败（%s）: %w", from, err)
	}
	return nil
}

// parseTriples SPARQL JSON → 三元组行（term 原样序列化为 Turtle：IRI <>、字面量带引号转义）。
func parseTriples(raw []byte) ([]migrateTriple, error) {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Type     string `json:"type"`
				Value    string `json:"value"`
				Datatype string `json:"datatype"`
				XMLLang  string `json:"xml:lang"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	out := make([]migrateTriple, 0, len(res.Results.Bindings))
	for _, b := range res.Results.Bindings {
		t := migrateTriple{S: termToTurtle(b["s"]), P: termToTurtle(b["p"]), O: termToTurtle(b["o"])}
		if t.S == "" || t.P == "" || t.O == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// termToTurtle SPARQL JSON term → Turtle 片段（IRI 加尖括号；字面量引号转义+类型/语言标注）。
type sparqlTerm struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Datatype string `json:"datatype"`
	XMLLang  string `json:"xml:lang"`
}

func termToTurtle(t sparqlTerm) string {
	switch t.Type {
	case "uri":
		return "<" + t.Value + ">"
	case "bnode":
		return "_:" + t.Value
	case "literal", "typed-literal":
		esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(t.Value)
		out := `"` + esc + `"`
		if t.XMLLang != "" {
			out += "@" + t.XMLLang
		} else if t.Datatype != "" && t.Datatype != "http://www.w3.org/2001/XMLSchema#string" {
			out += "^^<" + t.Datatype + ">"
		}
		return out
	}
	return ""
}

// insertTriplesData 分批 INSERT DATA（纯 Turtle 三元组入 named graph）。
func insertTriplesData(graphURI string, triples []migrateTriple) string {
	var b strings.Builder
	b.WriteString("INSERT DATA {\n  GRAPH <" + graphURI + "> {\n")
	for _, t := range triples {
		b.WriteString("    " + t.S + " " + t.P + " " + t.O + " .\n")
	}
	b.WriteString("  }\n}")
	return b.String()
}

// legacyDirEmpty 旧实例数据目录缺失或无内容（无存量可迁的常态判定）。
func legacyDirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

// retireLegacyProcess 退役收尾：清理数据目录签名匹配的旧伴生实例孤儿进程（含跨重启
// 遗留的领养对象——非本进程子进程，legacy.Stop() 够不着）。签名限定 companion-graph
// 数据目录，不伤及运行平面方案引擎（data/engines/）。
func retireLegacyProcess() {
	if err := exec.Command("pkill", "-f", "oxigraph serve --location data/companion-graph").Run(); err == nil {
		log.Printf("[companion] 旧伴生实例孤儿进程已退役（:9199，内置实例正式下线）")
	}
}
