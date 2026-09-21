package seed

// 内置学习示例本体（docs/04 v0.8 §4.8.3，REQ-96 P2a 前置资产）。
// 每个示例是完整可校验的 spec_json；seed-learning API 按 key 灌装入库。

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

//go:embed examples/*.json
var examplesFS embed.FS

// LearningExample 内置学习示例元信息。
type LearningExample struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LearningExamples 列出内置学习示例（key 字典序）。
func LearningExamples() []LearningExample {
	ents, err := examplesFS.ReadDir("examples")
	if err != nil {
		return nil
	}
	keys := []string{}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			keys = append(keys, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(keys)
	out := make([]LearningExample, 0, len(keys))
	for _, k := range keys {
		sp, err := LoadLearningExample(k)
		if err != nil {
			continue // 内容资产损坏时跳过，不阻塞其他示例
		}
		out = append(out, LearningExample{Key: k, Name: sp.Name, Description: sp.Description})
	}
	return out
}

// LoadLearningExample 按 key 加载并校验内置学习示例。
func LoadLearningExample(key string) (*pkgspec.Spec, error) {
	if key == "" || strings.ContainsAny(key, `/\.`) {
		return nil, fmt.Errorf("非法示例 key: %s", key)
	}
	bts, err := examplesFS.ReadFile("examples/" + key + ".json")
	if err != nil {
		return nil, fmt.Errorf("示例 %s 不存在", key)
	}
	var sp pkgspec.Spec
	if err := json.Unmarshal(bts, &sp); err != nil {
		return nil, fmt.Errorf("示例 %s 解析失败: %w", key, err)
	}
	if errs := sp.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("示例 %s 校验失败: %v", key, errs[0])
	}
	return &sp, nil
}
