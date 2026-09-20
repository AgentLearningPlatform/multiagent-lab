// Package skill 技能注入与工具合并（方案 §6.12，装配期 LG-16）。
// P1（M7）：库 + 注入预览；挂载生效（M9）时由 chat.Assembler 调用本包。
package skill

import (
	"fmt"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Composer 技能注入器。
type Composer struct {
	Store *store.Store
}

// LoadedSkills 返回 Agent 挂载且已启用的技能（供 skill.loaded 事件）。
func (c *Composer) LoadedSkills(a *store.Agent) []*store.Skill {
	if c == nil || a == nil {
		return nil
	}
	var out []*store.Skill
	for _, sid := range a.Skills {
		sk, err := c.Store.GetSkill(sid)
		if err != nil || sk == nil || !sk.Enabled {
			continue
		}
		out = append(out, sk)
	}
	return out
}

// ComposeInstruction 把启用技能的指令段注入系统提示词（§6.12 合并格式）。
func (c *Composer) ComposeInstruction(a *store.Agent) string {
	if a == nil {
		return ""
	}
	base := a.Instruction
	var extra []string
	for _, sk := range c.LoadedSkills(a) {
		extra = append(extra, fmt.Sprintf("<skill name=%q>%s</skill>", sk.Name, sk.Instruction))
	}
	if len(extra) == 0 {
		return base
	}
	return base + "\n\n# 启用技能\n" + strings.Join(extra, "\n\n")
}

// SkillTools 计算 skills[].tools 白名单并集（去重，保持出现顺序）。
// 调用方再与 agent.tools 合并并在工具注册表校验存在性（M9 装配期生效）。
func (c *Composer) SkillTools(a *store.Agent) []string {
	seen := map[string]bool{}
	var out []string
	for _, sk := range c.LoadedSkills(a) {
		for _, t := range sk.Tools {
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}
