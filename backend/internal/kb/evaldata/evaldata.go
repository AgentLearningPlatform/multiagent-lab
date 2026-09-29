// Package evaldata KB-14（M35/37 号方案）评测基准静态资产：种子语料 + 问题集。
// 消费方：internal/kb 的 build tag kbeval 测试（go:embed 不能落在 _test.go，故独立成包）。
package evaldata

import (
	_ "embed"
	"encoding/json"
)

//go:embed corpus.md
var Corpus string

//go:embed questions.json
var questionsRaw []byte

// Question 评测问题：type=keyword（词法臂）/ multihop（图臂）/ community（全局臂）。
type Question struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Q         string      `json:"q"`
	Gold      []string    `json:"gold"`       // keyword：gold 内容子串（topK 命中含子串即记中）
	Seed      string      `json:"seed_entity"` // multihop：种子实体
	Hops      int         `json:"hops"`        // multihop：跳数
	GoldEdges [][3]string `json:"gold_edges"`  // multihop：gold 边（source,rel,target）
	GoldLabel string      `json:"gold_label"`  // community：__any__ = 命中任意社区即可
}

// Questions 解析问题集。
func Questions() ([]Question, error) {
	var out struct {
		Questions []Question `json:"questions"`
	}
	if err := json.Unmarshal(questionsRaw, &out); err != nil {
		return nil, err
	}
	return out.Questions, nil
}
