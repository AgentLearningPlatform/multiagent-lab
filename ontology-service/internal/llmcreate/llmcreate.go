// Package llmcreate LLM 辅助创建（REQ-82）：生成-校验循环（最多 3 轮），草稿必须经用户预览确认。
// 模型能力归主平台（/api/ontology-llm/generate 代理），校验归构建平面（与人工编辑共用 Validate）。
package llmcreate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

const specSchemaHint = `{
  "type": "object",
  "required": ["name", "concepts", "relations", "instances"],
  "properties": {
    "name": {"type": "string"},
    "description": {"type": "string"},
    "concepts": {"type": "array", "items": {"type": "object", "required": ["name"], "properties": {
      "name": {"type": "string"}, "label": {"type": "string"}, "definition": {"type": "string"},
      "parents": {"type": "array", "items": {"type": "string"}}}}},
    "relations": {"type": "array", "items": {"type": "object", "required": ["name", "from", "to"], "properties": {
      "name": {"type": "string"}, "label": {"type": "string"}, "definition": {"type": "string"},
      "from": {"type": "string"}, "to": {"type": "string"}}}},
    "instances": {"type": "array", "items": {"type": "object", "required": ["name", "concept"], "properties": {
      "name": {"type": "string"}, "concept": {"type": "string"},
      "attributes": {"type": "object"},
      "relations": {"type": "array", "items": {"type": "object", "required": ["rel", "target"], "properties": {
        "rel": {"type": "string"}, "target": {"type": "string"}}}}}}}
  }
}`

type Creator struct {
	PlatformURL string // 主平台地址（/api/ontology-llm/generate）
	HTTP        *http.Client
	MaxRounds   int
}

func New(platformURL string) *Creator {
	return &Creator{PlatformURL: platformURL, HTTP: &http.Client{Timeout: 120 * time.Second}, MaxRounds: 3}
}

// GenerateResult 生成-校验循环结果。
type GenerateResult struct {
	Spec   *pkgspec.Spec `json:"spec"`
	Rounds int           `json:"rounds"`
	Usage  any           `json:"usage,omitempty"`
}

// Draft 领域描述 → spec_json 草稿；校验失败把错误列表回喂模型修正。
func (c *Creator) Draft(description, extraHint string) (*GenerateResult, error) {
	prompt := buildPrompt(description, extraHint, nil)
	var usage any
	for round := 1; round <= c.MaxRounds; round++ {
		draftRaw, u, err := c.callGenerate(prompt)
		if err != nil {
			return nil, err
		}
		if u != nil {
			usage = u
		}
		var sp pkgspec.Spec
		if err := json.Unmarshal([]byte(draftRaw), &sp); err != nil {
			// 结构坏：把解析错误回喂
			prompt = buildPrompt(description, extraHint, []string{"输出不是合法 spec_json: " + err.Error() + "。请只输出 JSON 本体，不要多余文本。"})
			continue
		}
		errs := sp.Validate()
		if len(errs) == 0 {
			return &GenerateResult{Spec: &sp, Rounds: round, Usage: usage}, nil
		}
		if round == c.MaxRounds {
			return &GenerateResult{Spec: &sp, Rounds: round, Usage: usage},
				fmt.Errorf("已达最大修正轮数，仍有 %d 处校验问题，草稿供预览参考", len(errs))
		}
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		prompt = buildPrompt(description, extraHint, msgs)
	}
	return nil, fmt.Errorf("生成循环异常退出")
}

func buildPrompt(description, extraHint string, fixErrors []string) string {
	var b strings.Builder
	b.WriteString("你是本体建模专家。请根据领域描述生成一个本体 spec_json，严格遵循以下 JSON Schema：\n")
	b.WriteString(specSchemaHint)
	b.WriteString("\n\n规则：\n")
	b.WriteString("- concepts[].name 唯一且非空；relations[].from/to 必须引用已定义概念；instances[].concept 必须引用已定义概念；instances[].relations[].rel/target 必须引用已定义关系/实例。\n")
	b.WriteString("- 只输出 JSON，不要 markdown 代码块或其他文本。\n\n领域描述：\n")
	b.WriteString(description)
	if extraHint != "" {
		b.WriteString("\n\n补充要求：\n" + extraHint)
	}
	if len(fixErrors) > 0 {
		b.WriteString("\n\n上一稿存在以下校验错误，请修正后重新输出完整 spec_json：\n")
		for _, e := range fixErrors {
			b.WriteString("- " + e + "\n")
		}
	}
	return b.String()
}

// RawChat 自由文本对话（REQ-103 模式 A 补全轮归纳用）：同一平台代理，不做 schema 约束。
func (c *Creator) RawChat(prompt string) (reply string, usage any, err error) {
	body, _ := json.Marshal(map[string]any{"prompt": prompt, "schema": `{"type":"object","properties":{"reply":{"type":"string"}},"required":["reply"]}`})
	resp, err := c.HTTP.Post(strings.TrimRight(c.PlatformURL, "/")+"/api/ontology-llm/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("调用主平台模型代理失败: %w", err)
	}
	defer resp.Body.Close()
	var res struct {
		DraftJSON string `json:"draft_json"`
		Usage     any    `json:"usage"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", nil, fmt.Errorf("主平台响应解析失败: %w", err)
	}
	if res.Error != "" {
		return "", nil, fmt.Errorf("主平台模型代理错误: %s", res.Error)
	}
	// 期望 {"reply": "..."}；容忍模型直接输出纯文本
	var wrapped struct {
		Reply string `json:"reply"`
	}
	if json.Unmarshal([]byte(res.DraftJSON), &wrapped) == nil && strings.TrimSpace(wrapped.Reply) != "" {
		return wrapped.Reply, res.Usage, nil
	}
	return res.DraftJSON, res.Usage, nil
}

func (c *Creator) callGenerate(prompt string) (draft string, usage any, err error) {
	body, _ := json.Marshal(map[string]any{"prompt": prompt, "schema": specSchemaHint})
	resp, err := c.HTTP.Post(strings.TrimRight(c.PlatformURL, "/")+"/api/ontology-llm/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("调用主平台模型代理失败: %w", err)
	}
	defer resp.Body.Close()
	var res struct {
		DraftJSON string `json:"draft_json"`
		Usage     any    `json:"usage"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", nil, fmt.Errorf("主平台响应解析失败: %w", err)
	}
	if res.Error != "" {
		return "", nil, fmt.Errorf("主平台模型代理错误: %s", res.Error)
	}
	return res.DraftJSON, res.Usage, nil
}
