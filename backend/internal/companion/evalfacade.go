package companion

import "encoding/json"

// REQ-194/M34 批次二④：评估跑分器专用导出门面——召回评测（evaldata 包，build tag `eval`）
// 只依赖公开纯函数与 SPARQL 面，不触达 Service 内部编排。零逻辑，仅导出转发。

// EvalLexicalRecall 词法召回（双向包含）——评测词法基线组。
func EvalLexicalRecall(labels []string, input string) []string { return recallEntities(labels, input) }

// EvalCosine 余弦相似度——评测向量组。
func EvalCosine(a, b []float32) float64 { return cosineSim(a, b) }

// EvalParseLabels SelectLabels 结果 → 标签数组。
func EvalParseLabels(raw []byte) []string { return parseLabelValues(raw) }

// EvalParseNeighbors SelectEntityNeighborhood 结果 → 邻居标签数组（含 2 跳链式边文本中的 otherLabel）。
func EvalParseNeighbors(raw []byte) []string {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, b := range res.Results.Bindings {
		if v, ok := b["otherLabel"]; ok && !seen[v.Value] {
			seen[v.Value] = true
			out = append(out, v.Value)
		}
	}
	return out
}
