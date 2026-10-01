package agenteval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client 评测客户端：对真实部署的 backend 走 HTTP（评测的就是用户动线本身）。
type Client struct {
	Base  string // 如 http://127.0.0.1:8080
	Token string // REQ-209 PLATFORM_TOKEN 启用时必填；loopback 部署可空
	HTTP  *http.Client
}

func NewClient(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 10 * time.Minute}}
}

func (c *Client) do(method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return b, fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, truncate(string(b), 300))
	}
	return b, nil
}

// CreateConversation 建临时评估会话，返回会话 id。
func (c *Client) CreateConversation(agentID, title string) (string, error) {
	body := map[string]any{"scope": "agent", "agent_id": agentID, "title": title}
	b, err := c.do("POST", "/api/conversations", body)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("create conversation: %w（raw=%s）", err, truncate(string(b), 200))
	}
	return out.ID, nil
}

func (c *Client) DeleteConversation(id string) error {
	_, err := c.do("DELETE", "/api/conversations/"+id, nil)
	return err
}

// RunConversation 发起一次运行并收集全部 SSE 事件（信封 {type,run_id,ts,data}）。
func (c *Client) RunConversation(convID, input string) ([]EventRecord, error) {
	body := map[string]any{"input": input}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/conversations/%s/runs", c.Base, convID), strings.NewReader(mustJSON(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("run: HTTP %d: %s", resp.StatusCode, truncate(string(b), 300))
	}
	var events []EventRecord
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	evType := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			evType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if raw == "" {
				continue
			}
			var envelope struct {
				Type string         `json:"type"`
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
				continue // 非信封行跳过（meta 之外不该有）
			}
			t := envelope.Type
			if t == "" {
				t = evType
			}
			if t == "" || t == "meta" {
				evType = ""
				continue
			}
			events = append(events, EventRecord{Type: t, Data: envelope.Data})
			evType = ""
		}
	}
	return events, sc.Err()
}

// TaskResult 单任务评测结果。
type TaskResult struct {
	Task      Task         `json:"task"`
	Answer    string       `json:"answer"`
	Tools     []string     `json:"tools"`
	Warnings  []string     `json:"warnings"`
	TotalTok  int          `json:"total_tokens"`
	ElapsedMS int64        `json:"elapsed_ms"`
	Findings  []Finding    `json:"findings"`
	TraceOK   bool         `json:"trace_ok"`
	Judge     *JudgeResult `json:"judge,omitempty"`
	Err       string       `json:"err,omitempty"`
}

// SuiteReport 一组（一个 agent 配置）全任务集结果。
type SuiteReport struct {
	AgentID  string       `json:"agent_id"`
	Results  []TaskResult `json:"results"`
	PassN    int          `json:"pass"`
	PartialN int          `json:"partial"`
	FailN    int          `json:"fail"`
	ScoredN  int          `json:"scored"`
}

// RunTask 执行单任务：建临时会话 → run → 轨迹断言（Judge 由调用方注入以解耦进程内依赖）。
func RunTask(ctx context.Context, c *Client, agentID string, t Task, judge func(Task, TraceSummary) (*JudgeResult, error)) TaskResult {
	res := TaskResult{Task: t}
	convID, err := c.CreateConversation(agentID, "agenteval-"+t.ID)
	if err != nil {
		res.Err = "create conversation: " + err.Error()
		return res
	}
	defer func() { _ = c.DeleteConversation(convID) }()
	start := time.Now()
	events, err := c.RunConversation(convID, t.Input)
	res.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Err = "run: " + err.Error()
		return res
	}
	sum := SummarizeEvents(events)
	res.Answer, res.Tools, res.Warnings, res.TotalTok = sum.Answer, sum.Tools, sum.Warnings, sum.TotalTokens
	res.Findings = CheckTrace(sum, t)
	res.TraceOK = TracePassed(res.Findings)
	if judge != nil {
		jr, jerr := judge(t, sum)
		if jerr != nil {
			res.Err = "judge: " + jerr.Error()
		} else {
			res.Judge = jr
		}
	}
	return res
}

// SummarizeSuite 汇总通过/部分/失败计数（Judge 缺失按轨迹断言兜底计）。
func SummarizeSuite(rep *SuiteReport) {
	for i := range rep.Results {
		r := &rep.Results[i]
		switch {
		case r.Judge != nil:
			rep.ScoredN++
			switch r.Judge.Score {
			case 2:
				rep.PassN++
			case 1:
				rep.PartialN++
			default:
				rep.FailN++
			}
		case r.Err == "" && r.TraceOK:
			rep.ScoredN++
			rep.PassN++
		case r.Err != "":
			rep.FailN++
		default:
			rep.ScoredN++
			rep.FailN++
		}
	}
}

// PrintReport 可读报表（跑分器 stdout）。
func PrintReport(w io.Writer, rep SuiteReport) {
	fmt.Fprintf(w, "\n===== agenteval 报告（agent=%s）=====\n", rep.AgentID)
	fmt.Fprintf(w, "%-22s %-10s %-6s %-6s %s\n", "任务", "类别", "轨迹", "Judge", "说明")
	for _, r := range rep.Results {
		judge := "-"
		if r.Judge != nil {
			judge = r.Judge.Verdict
		}
		trace := "PASS"
		if !r.TraceOK {
			trace = "FAIL"
		}
		note := ""
		if r.Err != "" {
			note = "err=" + truncate(r.Err, 80)
		} else if r.Judge != nil {
			note = truncate(r.Judge.Reason, 100)
		}
		fmt.Fprintf(w, "%-22s %-10s %-6s %-6s %s\n", r.Task.ID, r.Task.Category, trace, judge, note)
	}
	fmt.Fprintf(w, "合计：pass=%d partial=%d fail=%d（评分 %d/%d）\n", rep.PassN, rep.PartialN, rep.FailN, rep.ScoredN, len(rep.Results))
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
