//go:build eval

// REQ-207/M43（52 号 E5 并入）：本体域 eval 基准跑分器——双信号：
//   ①结构校验（spec 完整性：概念数>0/孤立概念/关系端点存在等轻量断言）
//   ②质量门禁评分（qualitygate.Check 三维加权 Overall）
// 对种子 5 份（internal/seed/examples）+ 运行平面真实本体 3 份（onto_k8s_ops/med_common/gene_core，
// 经构建平面 /api/ontologies/{id}/spec 拉取）出表。build tag `eval` 手动跑分，沿 agenteval/evaldata 先例。
//
// 运行（仓库 ontology-service/ 目录下）：
//	go test -tags eval ./internal/evaldata/ -run TestOntoEval -v
// 进化诊断步直接消费同一套双信号（internal/evolution 包）。
package evaldata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/qualitygate"
)

type evalResult struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"` // seed | real
	Overall  float64 `json:"overall"`
	Concepts int     `json:"concepts"`
	Relations int    `json:"relations"`
	StructOK bool    `json:"struct_ok"`
	Issues   string  `json:"issues,omitempty"`
}

// structCheck 结构轻量断言（进化「诊断」信号①）。
func structCheck(sp *pkgspec.Spec) (bool, []string) {
	var issues []string
	if len(sp.Concepts) == 0 {
		issues = append(issues, "无概念")
	}
	idx := map[string]bool{}
	for _, c := range sp.Concepts {
		if strings.TrimSpace(c.Name) == "" {
			issues = append(issues, "存在空名概念")
		}
		idx[c.Name] = true
	}
	for _, r := range sp.Relations {
		if !idx[r.From] {
			issues = append(issues, fmt.Sprintf("关系 %s 的 from %s 不存在", r.Name, r.From))
		}
		if !idx[r.To] {
			issues = append(issues, fmt.Sprintf("关系 %s 的 to %s 不存在", r.Name, r.To))
		}
	}
	return len(issues) == 0, issues
}

func evalSpec(t *testing.T, id, source string, specJSON []byte) evalResult {
	t.Helper()
	var sp pkgspec.Spec
	if err := json.Unmarshal(specJSON, &sp); err != nil {
		return evalResult{ID: id, Source: source, Issues: "spec 解析失败: " + err.Error()}
	}
	ok, issues := structCheck(&sp)
	rep := qualitygate.Check(&sp, qualitygate.DefaultConfig())
	sort.Strings(issues)
	return evalResult{ID: id, Source: source, Overall: rep.Score.Overall, Concepts: len(sp.Concepts),
		Relations: len(sp.Relations), StructOK: ok, Issues: strings.Join(issues, "; ")}
}

func TestOntoEval(t *testing.T) {
	var results []evalResult

	// ① 种子（embed 形态从 examples 目录读——测试 cwd=ontology-service/internal/evaldata）
	entries, err := os.ReadDir("../../internal/seed/examples")
	if err == nil {
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, rerr := os.ReadFile("../../internal/seed/examples/" + e.Name())
			if rerr != nil {
				t.Logf("seed %s 读取失败: %v", e.Name(), rerr)
				continue
			}
			results = append(results, evalSpec(t, strings.TrimSuffix(e.Name(), ".json"), "seed", b))
		}
	}

	// ② 真实三份（构建平面直拉；未启动则跳过并注记）
	buildURL := os.Getenv("BUILD_SVC_URL")
	if buildURL == "" {
		buildURL = "http://127.0.0.1:8091"
	}
	for _, oid := range []string{"onto_k8s_ops", "onto_med_common", "onto_gene_core"} {
		resp, err := http.Get(buildURL + "/api/ontologies/" + oid + "/spec")
		if err != nil {
			t.Logf("真实本体 %s 拉取失败（构建平面未启动即跳过）: %v", oid, err)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Logf("真实本体 %s 返回 %d，跳过", oid, resp.StatusCode)
				return
			}
			buf := make([]byte, 0, 1<<16)
			tmp := make([]byte, 4096)
			for {
				n, rerr := resp.Body.Read(tmp)
				buf = append(buf, tmp[:n]...)
				if rerr != nil {
					break
				}
			}
			results = append(results, evalSpec(t, oid, "real", buf))
		}()
	}

	if len(results) == 0 {
		t.Skip("无可用评测对象（种子目录缺失且构建平面不可达）")
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Source != results[j].Source {
			return results[i].Source == "seed"
		}
		return results[i].ID < results[j].ID
	})
	t.Logf("%-28s %-6s %8s %6s %6s  %s", "ID", "来源", "Overall", "概念", "关系", "结构/问题")
	failCount := 0
	for _, r := range results {
		status := "✓"
		if !r.StructOK {
			status = "✗ " + r.Issues
			failCount++
		}
		t.Logf("%-28s %-6s %8.2f %6d %6d  %s", r.ID, r.Source, r.Overall, r.Concepts, r.Relations, status)
	}
	// 硬断言：全部对象结构校验必须通过（结构信号）；质量分只报告不做硬门禁（阈值校准后续轮）
	if failCount > 0 {
		t.Errorf("%d/%d 个本体结构校验未通过", failCount, len(results))
	}
}
