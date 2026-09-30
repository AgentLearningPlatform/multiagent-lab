//go:build eval

// REQ-194/M34 批次二④：伴生召回评估跑分器（手动跑，不进 CI 硬门禁——依赖 embedding
// 连接与 oxigraph 二进制在位）。抽取环节用固定 stub 事实（隔离抽取质量变量，只测召回）。
//
// 运行（仓库 backend/ 目录下）：
//
//	go test -tags eval ./internal/companion/evaldata/ -run TestRecallEval -v
//
// 前置：①全局默认 embedding 连接已配置且可用（设置-模型连接，conn_type=embedding）；
// ②oxigraph 二进制在 data/bin/（引擎拉起于独立数据目录 companion-graph-eval/，端口 9196）。
// 口径：Recall@5 =（检索命中实体 ∩ 期望实体）/|期望实体|；三组对比：
// 词法基线（双向包含）vs 向量（余弦 topK≤5 阈值 0.35）vs 向量+2跳（命中实体邻域扩展）。
package evaldata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func TestRecallEval(t *testing.T) {
	// ── 前置检查（不满足即 Skip，给明确指引）──
	bin := filepath.Join("..", "..", "..", "..", "data", "bin", "oxigraph")
	if _, err := os.Stat(bin); err != nil {
		bin = "data/bin/oxigraph"
		if _, err2 := os.Stat(bin); err2 != nil {
			t.Skip("本机无 oxigraph 二进制（data/bin/oxigraph），跳过评估")
		}
	}
	dbPath := filepath.Join("..", "..", "..", "data", "platform.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("开发库不在位（%s），跳过评估", dbPath)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("打开开发库失败: %v", err)
	}
	defer st.Close()
	box, err := secrets.LoadKeyFile(filepath.Join("..", "..", "..", "data", ".secret"))
	if err != nil {
		t.Skipf("密钥文件不在位，跳过评估: %v", err)
	}
	emb := &kb.Embedder{Store: st, Box: box}
	probe, err := emb.EmbedOne(context.Background(), "连通性探针")
	if err != nil {
		t.Skipf("全局默认 embedding 连接不可用（请先到设置-模型连接配置），跳过评估: %v", err)
	}
	if len(probe) == 0 {
		t.Skip("embedding 返回空向量，跳过评估")
	}

	// ── 引擎拉起（独立数据目录+端口，与运行平面/冒烟隔离）——REQ-216 起评测面=裸 SPARQL
	// 端点（伴生读写已归一方案引擎，本评测以直连 oxigraph 模拟引擎端点）──
	dir := filepath.Join("..", "..", "..", "..", "data", "companion-graph-eval")
	_ = exec.Command("pkill", "-f", "companion-graph-eval").Run()
	time.Sleep(300 * time.Millisecond)
	_ = os.RemoveAll(dir)
	defer func() {
		_ = exec.Command("pkill", "-f", "companion-graph-eval").Run()
	}()
	cmd := exec.Command(bin, "serve", "--location", dir, "--bind", "127.0.0.1:9196")
	if err := cmd.Start(); err != nil {
		t.Fatalf("oxigraph 启动失败: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	endpoint := "http://127.0.0.1:9196/query"
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	waitSPARQL(t, endpoint)
	if err := sparqlUpdate(ctx, endpoint, companion.SeedSchema()); err != nil {
		t.Fatalf("种子 schema 失败: %v", err)
	}
	now := time.Now()
	for _, sess := range Sessions {
		for i, f := range sess.Facts {
			cid := fmt.Sprintf("eval-%s-%d", sess.ID, i)
			var q string
			if f.Kind == "relation" {
				q = companion.InsertRelationTriples(sess.ID, cid, f.RelName, f.Name, f.RelTarget, f.Definition, f.Confidence, "evalmsg", now)
			} else {
				q = companion.InsertNodeTriples(sess.ID, cid, f.Kind, f.Name, f.Definition, f.Confidence, "evalmsg", now)
			}
			if err := sparqlUpdate(ctx, endpoint, q); err != nil {
				t.Fatalf("事实入图失败（%s）: %v", f.Name, err)
			}
		}
	}

	// ── 逐会话逐问题跑分 ──
	type modeStat struct {
		hit, total float64
	}
	lexicalStat, vectorStat, bothStat := modeStat{}, modeStat{}, modeStat{}
	vecErrCount := 0
	t.Log("题号 | 词法@5 | 向量@5 | 向量+2跳@5 | 期望实体 | 意图")
	for _, q := range Questions {
		raw, err := sparqlQuery(ctx, endpoint, companion.SelectLabels(q.Conv))
		if err != nil {
			t.Fatalf("标签查询失败: %v", err)
		}
		labels := companion.EvalParseLabels(raw)
		lexHits := companion.EvalLexicalRecall(labels, q.Input)

		vecHits, neighbors, verr := evalVector(ctx, emb, endpoint, q, labels)
		if verr != nil {
			vecErrCount++
		}

		rLex := recallAtK(lexHits, q.Expected)
		rVec := recallAtK(vecHits, q.Expected)
		rBoth := recallAtKExpanded(vecHits, neighbors, q.Expected)
		lexicalStat.hit += rLex
		lexicalStat.total++
		vectorStat.hit += rVec
		vectorStat.total++
		bothStat.hit += rBoth
		bothStat.total++
		t.Logf("%s | %.2f | %.2f | %.2f | %v | %s", q.ID, rLex, rVec, rBoth, q.Expected, q.Intent)
	}

	avg := func(s modeStat) float64 {
		if s.total == 0 {
			return 0
		}
		return s.hit / s.total
	}
	t.Logf("════ Recall@5 三组对比（%d 题）════", int(lexicalStat.total))
	t.Logf("词法基线   : %.3f", avg(lexicalStat))
	t.Logf("向量       : %.3f", avg(vectorStat))
	t.Logf("向量+2跳   : %.3f", avg(bothStat))
	if vecErrCount > 0 {
		t.Logf("⚠ %d 题 embedding 失败（向量组按 0 计）", vecErrCount)
	}

	// 结构性断言（硬）：三组数值均在 [0,1]
	for name, v := range map[string]float64{"词法": avg(lexicalStat), "向量": avg(vectorStat), "向量+2跳": avg(bothStat)} {
		if v < 0 || v > 1 {
			t.Errorf("%s recall 越界: %f", name, v)
		}
	}
	// 对照性断言（软）：同义改述为主的问题集上向量组应不低于词法基线——
	// 不达标不判失败，如实输出数字供方案迭代参考（收益以落档迭代表为准）。
	if avg(vectorStat) < avg(lexicalStat) {
		t.Logf("⚠ 对照结论：向量组（%.3f）低于词法基线（%.3f）——检查 embedding 模型质量或阈值 0.35", avg(vectorStat), avg(lexicalStat))
	} else {
		t.Logf("✓ 对照结论：向量组（%.3f）≥ 词法基线（%.3f）", avg(vectorStat), avg(lexicalStat))
	}
}

// evalVector 向量召回 + 命中实体 2 跳邻域扩展（独立实现，不复用 Service 内部——评测只依赖公开纯函数与 SPARQL 面）。
func evalVector(ctx context.Context, emb *kb.Embedder, endpoint string, q Question, labels []string) (hits []string, neighbors map[string][]string, err error) {
	if len(labels) == 0 {
		return nil, map[string][]string{}, nil
	}
	labelVecs, err := emb.EmbedTexts(ctx, labels)
	if err != nil {
		return nil, map[string][]string{}, err
	}
	inVec, err := emb.EmbedOne(ctx, q.Input)
	if err != nil {
		return nil, map[string][]string{}, err
	}
	type scored struct {
		label string
		sim   float64
	}
	var cands []scored
	for i, l := range labels {
		if i >= len(labelVecs) {
			break
		}
		if sim := companion.EvalCosine(inVec, labelVecs[i]); sim >= 0.35 {
			cands = append(cands, scored{l, sim})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].sim > cands[j].sim })
	if len(cands) > 5 {
		cands = cands[:5]
	}
	neighbors = map[string][]string{}
	for _, c := range cands {
		hits = append(hits, c.label)
		raw, err := sparqlQuery(ctx, endpoint, companion.SelectEntityNeighborhood(q.Conv, c.label))
		if err != nil {
			continue
		}
		neighbors[c.label] = companion.EvalParseNeighbors(raw)
	}
	return hits, neighbors, nil
}

// waitSPARQL 健康等待（评测引擎就绪）。
func waitSPARQL(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := sparqlUpdate(ctx, endpoint, "SELECT * WHERE {} LIMIT 1")
		cancel()
		if err == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("评测引擎健康等待超时（%s）", endpoint)
}

// sparqlUpdate SPARQL UPDATE（/update 端点）。
func sparqlUpdate(ctx context.Context, queryEndpoint, sparql string) error {
	base := strings.TrimSuffix(queryEndpoint, "/query")
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/update", strings.NewReader(sparql))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sparql-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("update %s: %s", resp.Status, string(b))
	}
	return nil
}

// sparqlQuery SPARQL SELECT（/query 端点）。
func sparqlQuery(ctx context.Context, endpoint, sparql string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(sparql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("query %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// recallAtK Recall@K：命中实体 ∩ 期望实体 / |期望实体|。
func recallAtK(hits, expected []string) float64 {
	if len(expected) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, h := range hits {
		set[h] = true
	}
	hit := 0
	for _, e := range expected {
		if set[e] {
			hit++
		}
	}
	return float64(hit) / float64(len(expected))
}

// recallAtKExpanded 向量+2跳口径：期望实体出现在主命中或任一命中实体的邻域（含链式边文本）均计命中。
func recallAtKExpanded(hits []string, neighbors map[string][]string, expected []string) float64 {
	if len(expected) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, h := range hits {
		set[h] = true
		for _, n := range neighbors[h] {
			set[n] = true
		}
	}
	hit := 0
	for _, e := range expected {
		if set[e] {
			hit++
		}
	}
	return float64(hit) / float64(len(expected))
}
