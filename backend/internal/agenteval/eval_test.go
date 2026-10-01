//go:build agenteval

// REQ-223/M51：Agent 行为评估跑分器（真机手动跑，不进 CI 硬门禁——依赖 run-dev 栈与默认 chat 模型连接）。
//
//	运行（仓库 backend/ 目录下，backend 栈在跑）：
//	  go test -tags agenteval ./internal/agenteval/ -run TestAgentEval -v
//
//	前置：①backend :8080 在跑（run-dev.sh）；②设置-模型连接有 enabled 的默认 chat 连接（Judge 与被评 agent 共用）；
//	③AGENTEVAL_DB 指向平台库（默认 backend/data/eino.db），AGENTEVAL_SECRET 指向密钥文件（默认 backend/data/.secret）。
//	被评 agent：优先 AGENTEVAL_AGENT 指定存量 id；否则自动创建/复用「agenteval-suite」评估 agent
//	（勾选内置六工具 current_time/todo_write/grep/glob/write_file/read_file），跑完保留供复用。
//	A/B 对照：AGENTEVAL_AGENT_A 与 AGENTEVAL_AGENT_B 都设置时各跑全量出对照。
package agenteval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func TestAgentEval(t *testing.T) {
	base := envOr("AGENTEVAL_BASE_URL", "http://127.0.0.1:8080")
	// ── 前置检查：backend 在跑 ──
	c := NewClient(base, os.Getenv("AGENTEVAL_TOKEN"))
	if _, err := c.do("GET", "/healthz", nil); err != nil {
		t.Skipf("backend 不可达（%s）：%v——先 run-dev.sh 启动栈", base, err)
	}
	// ── Judge 依赖：平台库 + 密钥（进程内 GenerateText 走默认 chat 连接）──
	dbPath := envOr("AGENTEVAL_DB", filepath.Join("..", "..", "data", "platform.db"))
	st, err := store.Open(dbPath)
	if err != nil {
		t.Skipf("打开平台库失败（%s）：%v——AGENTEVAL_DB 指向运行栈同库", dbPath, err)
	}
	defer st.Close()
	secretPath := envOr("AGENTEVAL_SECRET", filepath.Join("..", "..", "data", ".secret"))
	box, err := secrets.LoadKeyFile(secretPath)
	if err != nil {
		t.Skipf("加载密钥失败（%s）：%v", secretPath, err)
	}
	if def, err := st.GetDefaultConnection("chat"); err != nil || def == nil {
		t.Skipf("无默认 chat 模型连接（err=%v）——设置页先配置", err)
	}
	judgeConn := os.Getenv("AGENTEVAL_JUDGE_CONN") // 空=默认连接
	ctx := context.Background()

	tasks := DefaultTasks()
	if only := os.Getenv("AGENTEVAL_ONLY"); only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		filtered := tasks[:0]
		for _, task := range tasks {
			if want[task.ID] {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}
	runSuite := func(agentID string) SuiteReport {
		rep := SuiteReport{AgentID: agentID}
		judge := func(task Task, sum TraceSummary) (*JudgeResult, error) {
			jr, err := JudgeAnswer(ctx, st, box, judgeConn, task, sum)
			if err != nil {
				return nil, err
			}
			return &jr, nil
		}
		for _, task := range tasks {
			res := RunTask(ctx, c, agentID, task, judge)
			rep.Results = append(rep.Results, res)
			if res.Err != "" {
				t.Logf("[%s] err=%s", task.ID, res.Err)
			}
		}
		SummarizeSuite(&rep)
		PrintReport(os.Stdout, rep)
		return rep
	}

	agentA := os.Getenv("AGENTEVAL_AGENT")
	if agentA == "" {
		agentA, err = ensureEvalAgent(c)
		if err != nil {
			t.Fatalf("准备评估 agent 失败：%v", err)
		}
	}
	repA := runSuite(agentA)
	saveReport(t, "agenteval-report", repA)

	if abB := os.Getenv("AGENTEVAL_AGENT_B"); abB != "" {
		repB := runSuite(abB)
		saveReport(t, "agenteval-report-b", repB)
		fmt.Printf("\n===== A/B 对照（A=%s vs B=%s）=====\nA：pass=%d partial=%d fail=%d\nB：pass=%d partial=%d fail=%d\n",
			repA.AgentID, repB.AgentID, repA.PassN, repA.PartialN, repA.FailN, repB.PassN, repB.PartialN, repB.FailN)
		for i := range repA.Results {
			for j := range repB.Results {
				if repA.Results[i].Task.ID == repB.Results[j].Task.ID {
					a, b := repA.Results[i], repB.Results[j]
					if scoreOf(a) != scoreOf(b) {
						fmt.Printf("  分差 [%s] A=%s B=%s\n", a.Task.ID, verdictOf(a), verdictOf(b))
					}
				}
			}
		}
	}
}

// ensureEvalAgent 找/建「agenteval-suite」评估 agent（内置六工具勾选）。
func ensureEvalAgent(c *Client) (string, error) {
	b, err := c.do("GET", "/api/agents", nil)
	if err != nil {
		return "", err
	}
	var agents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &agents); err == nil {
		for _, a := range agents {
			if a.Name == "agenteval-suite" {
				return a.ID, nil
			}
		}
	}
	// work_dir 必须给绝对路径：装配期 resolveWorkRoot 空=四件文件工具不装配（REQ-202 口径）
	// ——首跑基线（2026-10-01）抓到的真问题：无 work_dir 时 agent「只见三个工具」拒绝一切文件任务。
	cwd, _ := os.Getwd()
	workDir := filepath.Join(cwd, "data", "agenteval-workdir")
	_ = os.MkdirAll(workDir, 0o755)
	payload := map[string]any{
		"name":        "agenteval-suite",
		"description": "REQ-223 评估专用 agent（跑分器自动创建，可删除）",
		"instruction": "你是被评测的助手。如实回答：能做的认真做完（含工具确认动作），做不到或不知道的如实声明，不编造。",
		"tools":       []string{"current_time", "todo_write", "grep", "glob", "write_file", "read_file"},
		"work_dir":    workDir,
	}
	b2, err := c.do("POST", "/api/agents", payload)
	if err != nil {
		return "", fmt.Errorf("create eval agent: %w（raw=%s）", err, truncate(string(b2), 300))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b2, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("create eval agent: 解析失败 raw=%s", truncate(string(b2), 300))
	}
	time.Sleep(300 * time.Millisecond)
	return out.ID, nil
}

func saveReport(t *testing.T, name string, rep SuiteReport) {
	dir := envOr("AGENTEVAL_OUT", filepath.Join("..", "..", "..", "smoke", "agenteval"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("报告目录创建失败：%v", err)
		return
	}
	p := filepath.Join(dir, name+"-"+time.Now().Format("20060102-150405")+".json")
	b, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Logf("报告写入失败：%v", err)
	} else {
		fmt.Printf("报告已写 %s\n", p)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func scoreOf(r TaskResult) int {
	if r.Judge != nil {
		return r.Judge.Score
	}
	if r.TraceOK {
		return 2
	}
	return 0
}

func verdictOf(r TaskResult) string {
	if r.Judge != nil {
		return r.Judge.Verdict
	}
	if r.TraceOK {
		return "pass(轨迹)"
	}
	return "fail"
}
