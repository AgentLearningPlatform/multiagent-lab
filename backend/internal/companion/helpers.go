package companion

import (
	"encoding/json"
	"os"
	"strings"
)

// 辅助函数（环境读取 / 文本处理 / SPARQL JSON 结果解析）。

// osBinary 显式引擎二进制（环境变量覆盖）。
func osBinary() string { return os.Getenv("COMPANION_OXIGRAPH_BIN") }

// roleLabel 消息角色中文标注（语料可读性）。
func roleLabel(role string) string {
	if role == "user" {
		return "用户"
	}
	return "助手"
}

// truncate 按字符截断（CJK 安全，不切 rune）。
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// parseEdgeURI 从 FindActiveEdge 的 SPARQL JSON 结果提取 ?edge 绑定值（空 = 无旧边）。
func parseEdgeURI(raw []byte) string {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return ""
	}
	for _, b := range res.Results.Bindings {
		if v, ok := b["edge"]; ok {
			return v.Value
		}
	}
	return ""
}

// extractLabelsJSON 从 SelectLabels 结果提取 label 值数组（透传给前端状态）。
func extractLabelsJSON(raw []byte) string {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "[]"
	}
	out := make([]string, 0, len(res.Results.Bindings))
	for _, b := range res.Results.Bindings {
		if v, ok := b["label"]; ok {
			out = append(out, v.Value)
		}
	}
	j, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(j)
}
