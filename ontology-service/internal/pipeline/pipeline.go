// Package pipeline 工具链配置层（REQ-75/76，04 §4.6）：
// 七阶段候选工具清单（tools.json 数据驱动）+ 默认工具链模板 + 引导清单生成。
// pipeline_profile 存储在 repo.Store（004_pipeline.sql），本包只管静态数据与纯函数。
package pipeline

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed tools.json
var toolsJSON []byte

// Guide guided 模式执行指引（安装/启动命令、数据交接点、入口链接）。
type Guide struct {
	Install string `json:"install,omitempty"`
	Handoff string `json:"handoff,omitempty"`
	Entry   string `json:"entry,omitempty"`
}

// StageTool 候选工具统一描述（REQ-77 开放性：声明吃什么/吐什么形态即可入列）。
type StageTool struct {
	ID       string   `json:"id"`
	Stage    string   `json:"stage"` // s1..s7
	Name     string   `json:"name"`
	License  string   `json:"license,omitempty"`
	Mode     string   `json:"mode"` // builtin | guided | managed
	Eats     []string `json:"eats,omitempty"`
	Gives    []string `json:"gives,omitempty"`
	Guide    *Guide   `json:"guide,omitempty"`
	Learning string   `json:"learning,omitempty"`
}

type toolsFile struct {
	Version string      `json:"version"`
	Note    string      `json:"note"`
	Tools   []StageTool `json:"tools"`
}

// Catalog 全部候选工具（按 stage 分组，stage 内按 mode 排序保证稳定渲染）。
func Catalog() map[string][]StageTool {
	var f toolsFile
	if err := json.Unmarshal(toolsJSON, &f); err != nil {
		return map[string][]StageTool{} // 内容资产损坏时不 panic，返回空清单
	}
	byStage := map[string][]StageTool{}
	for _, t := range f.Tools {
		byStage[t.Stage] = append(byStage[t.Stage], t)
	}
	for s := range byStage {
		ts := byStage[s]
		sort.Slice(ts, func(i, j int) bool {
			mi, mj := modeRank(ts[i].Mode), modeRank(ts[j].Mode)
			if mi != mj {
				return mi < mj
			}
			return ts[i].ID < ts[j].ID
		})
		byStage[s] = ts
	}
	return byStage
}

func modeRank(m string) int {
	switch m {
	case "builtin":
		return 0
	case "managed":
		return 1
	default:
		return 2 // guided 最后（需要外部安装）
	}
}

// StageIDs 七阶段固定顺序。
var StageIDs = []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7"}

// DefaultStages 默认工具链：每阶段预置 builtin 项（S5 交给运行方案，打开即用）。
func DefaultStages() map[string]StageSelection {
	return map[string]StageSelection{
		"s1": {Tool: "builtin_import", Mode: "builtin"},
		"s2": {Tool: "builtin_editor", Mode: "builtin"},
		"s3": {Tool: "builtin_validate", Mode: "builtin"},
		"s4": {Tool: "builtin_reactflow", Mode: "builtin"},
		"s5": {Tool: "builtin_oxigraph", Mode: "managed"},
		"s6": {Tool: "builtin_sparql", Mode: "builtin"},
		"s7": {Tool: "builtin_agent_mount", Mode: "builtin"},
	}
}

// StageSelection 单阶段选择（stages JSON 的值）。
type StageSelection struct {
	Tool    string         `json:"tool"`
	Mode    string         `json:"mode"`
	Params  map[string]any `json:"params,omitempty"`
}

// ChecklistItem 引导清单条目（guided 阶段聚合 + 任务卡引用）。
type ChecklistItem struct {
	Key      string `json:"key"`      // tool:<stage>:<tool_id> 或 task:<task_id>
	Kind     string `json:"kind"`     // tool | task
	Stage    string `json:"stage"`    // tool 类有；task 类空
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	EntryURL string `json:"entry_url,omitempty"`
}

// BuildChecklist 按当前配置生成引导清单：guided 阶段聚合其 Guide 三项（安装/交接/入口）。
// 任务卡（task: 空间）归学习中心渲染，此处只出 tool: 条目。
func BuildChecklist(stages map[string]StageSelection) []ChecklistItem {
	catalog := Catalog()
	items := []ChecklistItem{}
	for _, sid := range StageIDs {
		sel, ok := stages[sid]
		if !ok {
			continue
		}
		for _, t := range catalog[sid] {
			if t.ID != sel.Tool || t.Mode != "guided" {
				continue
			}
			g := t.Guide
			if g == nil {
				g = &Guide{}
			}
			detail := g.Handoff
			if g.Install != "" {
				detail = "安装/获取：" + g.Install
				if g.Handoff != "" {
					detail += "；" + g.Handoff
				}
			}
			items = append(items, ChecklistItem{
				Key:      fmt.Sprintf("tool:%s:%s", sid, t.ID),
				Kind:     "tool",
				Stage:    sid,
				Title:    t.Name + "（" + sid + "）",
				Detail:   detail,
				EntryURL: g.Entry,
			})
		}
	}
	return items
}
