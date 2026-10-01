package rest

// REQ-207/M43 本体自进化 REST 面（候选 vN-cK 状态机；一期人工触发）：
//   GET  /api/ontologies/{id}/evolution/candidates?status=          候选列表
//   POST /api/ontologies/{id}/evolution/candidates                  提交候选补丁（归因产出；含 Evidence）
//   POST /api/ontologies/{id}/evolution/candidates/{cid}/gate       配对门控（补丁前后同题对照：结构+质量双信号）
//   POST /api/ontologies/{id}/evolution/candidates/{cid}/accept     采纳（应用补丁→SaveVersion 升正式版本）
//   POST /api/ontologies/{id}/evolution/candidates/{cid}/reject     拒绝（留档冻结）
// 门控语义（43 号 §5.1 消融：门控最承重 −11.2）：apply 前 base 信号 vs apply 后信号对比，
// 质量分下降或结构破坏 → 门控不过 → 拒绝采纳（候选留档）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/evolution"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/qualitygate"
)

// evoStore 惰性初始化进化候选存储。
func (s *Server) evoStore() *evolution.Store {
	if s.evoDB == nil {
		s.evoDB = evolution.New(s.Store.DB())
	}
	return s.evoDB
}

// getSpecMap 读当前 spec 为 map 形态（进化补丁操作面）。
func (s *Server) getSpecMap(ontologyID string) (map[string]any, int, error) {
	raw, _, err := s.Store.GetArtifact(ontologyID, "spec_json")
	if err != nil {
		return nil, 0, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, 0, err
	}
	o, err := s.Store.GetOntology(ontologyID)
	if err != nil {
		return nil, 0, err
	}
	return m, o.Version, nil
}

// listEvolutionCandidates GET 候选列表。
func (s *Server) listEvolutionCandidates(w http.ResponseWriter, r *http.Request) {
	list, err := s.evoStore().ListByOntology(r.PathValue("id"), r.URL.Query().Get("status"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []*evolution.Candidate{}
	}
	writeJSON(w, http.StatusOK, list)
}

// proposeEvolutionCandidate POST 提交候选（归因产出的类型化补丁+Evidence；轮次预算校验）。
func (s *Server) proposeEvolutionCandidate(w http.ResponseWriter, r *http.Request) {
	ontologyID := r.PathValue("id")
	if _, _, err := s.getSpecMap(ontologyID); err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Layer    string `json:"layer"`
		Summary  string `json:"summary"`
		Evidence string `json:"evidence"`
		Patch    json.RawMessage `json:"patch"`
		Round    int    `json:"round"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.Summary) == "" || len(in.Patch) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "summary 与 patch 必填"})
		return
	}
	switch in.Layer {
	case "content", "tool", "schema":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "layer 须为 content|tool|schema（归因三层，每次只动一层）"})
		return
	}
	// 轮次预算（REQ-207②）
	rounds, _ := s.evoStore().CountRounds(ontologyID)
	round := in.Round
	if round == 0 {
		round = rounds + 1
	}
	if round > evolution.MaxRoundsPerPass {
		writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("轮次预算已用尽（%d/%d）——留档后重开进化 pass", rounds, evolution.MaxRoundsPerPass)})
		return
	}
	_, version, err := s.getSpecMap(ontologyID)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.evoStore().Create(&evolution.Candidate{
		OntologyID:  ontologyID,
		BaseVersion: version,
		Layer:       in.Layer,
		Summary:     in.Summary,
		Evidence:    in.Evidence,
		PatchJSON:   string(in.Patch),
		Round:       round,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// gateSignal 门控信号（结构+质量双维度）。
type gateSignal struct {
	StructOK  bool    `json:"struct_ok"`
	Overall   float64 `json:"overall"`
	Concepts  int     `json:"concepts"`
	Relations int     `json:"relations"`
}

// signalOf spec map → 双信号。
func signalOf(m map[string]any) gateSignal {
	b, _ := json.Marshal(m)
	var sp pkgspec.Spec
	_ = json.Unmarshal(b, &sp)
	sig := gateSignal{Concepts: len(sp.Concepts), Relations: len(sp.Relations)}
	// 结构断言（evaldata 同口径内联：空名概念/关系端点存在）
	idx := map[string]bool{}
	for _, c := range sp.Concepts {
		idx[c.Name] = true
	}
	sig.StructOK = len(sp.Concepts) > 0
	for _, rel := range sp.Relations {
		if !idx[rel.From] || !idx[rel.To] {
			sig.StructOK = false
		}
	}
	rep := qualitygate.Check(&sp, qualitygate.DefaultConfig())
	sig.Overall = rep.Score.Overall
	return sig
}

// gateEvolutionCandidate POST 配对门控：base spec vs apply(patch) spec 双信号对照。
// 质量分下降>5 或结构破坏 → 不过（候选留 proposed 可改；对照报告落 gate_report）。
func (s *Server) gateEvolutionCandidate(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("cid")
	c, err := s.evoStore().Get(cid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if c.Status != "proposed" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "仅 proposed 候选可门控"})
		return
	}
	base, _, err := s.getSpecMap(c.OntologyID)
	if err != nil {
		writeErr(w, err)
		return
	}
	baseSig := signalOf(base)
	// 深拷贝 base → 应用补丁
	patched := deepCopyMap(base)
	if _, err := evolution.ApplyPatch(patched, c.PatchJSON); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "补丁应用失败: " + err.Error()})
		return
	}
	patchSig := signalOf(patched)
	regressed := !patchSig.StructOK || patchSig.Overall < baseSig.Overall-5.0
	report, _ := json.Marshal(map[string]any{
		"base": baseSig, "patched": patchSig,
		"regressed": regressed,
		"passed":    !regressed,
		"verdict":   map[bool]string{true: "门控不过（质量回退或结构破坏）", false: "门控通过"}[regressed],
		"note":      "配对门控：补丁前后同题对照（qualitygate 三维加权 + 结构断言双信号）",
	})
	c.GateReport = string(report)
	if err := s.evoStore().UpdateGateReport(c.ID, c.GateReport); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"candidate": c, "gate_report": json.RawMessage(c.GateReport), "passed": !regressed,
	})
}

// acceptEvolutionCandidate POST 采纳：应用补丁 → SaveVersion 升正式版本（候选永不动正式版本，
// 采纳动作才产生新正式版）；门控未过或未跑时拒绝采纳（验收：候选补丁未经门控不得转正）。
func (s *Server) acceptEvolutionCandidate(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("cid")
	c, err := s.evoStore().Get(cid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if c.Status != "proposed" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "仅 proposed 候选可采纳"})
		return
	}
	if c.GateReport == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "门控未执行——候选补丁未经门控不得转正（REQ-207 验收①）"})
		return
	}
	var report struct {
		Passed bool `json:"passed"`
	}
	_ = json.Unmarshal([]byte(c.GateReport), &report)
	if !report.Passed {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "门控未通过——不得采纳（对照报告见 gate_report）"})
		return
	}
	spec, curVersion, err := s.getSpecMap(c.OntologyID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := evolution.ApplyPatch(spec, c.PatchJSON); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "补丁应用失败: " + err.Error()})
		return
	}
	newJSON, _ := json.Marshal(spec)
	newVersion := curVersion + 1
	if err := s.Store.PutArtifact(c.OntologyID, "spec_json", string(newJSON), true); err != nil {
		writeErr(w, err)
		return
	}
	_ = s.Store.SaveVersion(c.OntologyID, newVersion, string(newJSON), "", "")
	if _, err := s.Store.BumpVersion(c.OntologyID); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.evoStore().Decide(c.ID, "accepted"); err != nil {
		writeErr(w, err)
		return
	}
	o, _ := s.Store.GetOntology(c.OntologyID)
	writeJSON(w, http.StatusOK, map[string]any{"candidate": c, "ontology": o, "new_version": newVersion})
}

// rejectEvolutionCandidate POST 拒绝（留档冻结：预算与对照报告可追溯）。
func (s *Server) rejectEvolutionCandidate(w http.ResponseWriter, r *http.Request) {
	c, err := s.evoStore().Decide(r.PathValue("cid"), "rejected")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// deepCopyMap JSON 往返深拷贝。
func deepCopyMap(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
