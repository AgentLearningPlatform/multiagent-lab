// Package skill 内：load_skill 工具（REQ-203/M38 技能渐进披露的正文懒加载面）。
// 只允许加载本 agent 已启用技能（装配期注入清单）——越权名回执拒绝；业务错误走回执文本
// 不炸 run（REQ-202 回执口径）。
package skill

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

type loadSkillIn struct {
	Name string `json:"name" jsonschema:"required"`
}

type loadSkillOut struct {
	Error   string `json:"error,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// NewLoadSkillTool 构造 load_skill：从装配期固化的已启用技能清单按名取正文（指令全文）。
func NewLoadSkillTool(skills []*store.Skill) (einotool.BaseTool, error) {
	byName := map[string]*store.Skill{}
	for _, sk := range skills {
		if sk != nil && strings.TrimSpace(sk.Name) != "" {
			byName[sk.Name] = sk
		}
	}
	return utils.InferTool("load_skill",
		"加载已挂载技能的完整指令正文（系统提示词中只列技能目录）。需要执行某技能的流程/规范时，先调用本工具取正文再照做。",
		func(_ context.Context, in loadSkillIn) (*loadSkillOut, error) {
			name := strings.TrimSpace(in.Name)
			sk, ok := byName[name]
			if !ok {
				avail := make([]string, 0, len(byName))
				for n := range byName {
					avail = append(avail, n)
				}
				return &loadSkillOut{Error: fmt.Sprintf("技能 %q 不在本 agent 已挂载清单内（可用: %v）", name, avail)}, nil
			}
			content := strings.TrimSpace(sk.Instruction)
			if content == "" {
				return &loadSkillOut{Error: fmt.Sprintf("技能 %q 正文为空", name)}, nil
			}
			return &loadSkillOut{Name: sk.Name, Content: content}, nil
		})
}
