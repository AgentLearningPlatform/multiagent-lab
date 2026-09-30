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

// ComposeInstruction 技能渐进披露注入（REQ-203/M38 B6）：系统提示词只注入「目录」——
// 技能名与一句话描述，正文不常驻（多技能全量注入会撑爆系统提示词，且上下文预算档位下
// 属昂贵常驻）；正文经装配期注册的 load_skill 工具按需加载。
// 变更注：原形态为 <skill> 正文全量拼接（§6.12 v0.6 口径），REQ-203 拍板改为目录+懒加载。
func (c *Composer) ComposeInstruction(a *store.Agent) string {
	if a == nil {
		return ""
	}
	base := a.Instruction
	skills := c.LoadedSkills(a)
	if len(skills) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n# 启用技能（正文按需加载）\n")
	b.WriteString("以下技能已挂载；正文不常驻本提示词——需要执行该技能时，调用 load_skill 工具（参数 name=技能名）获取完整指令后再照做：\n")
	for _, sk := range skills {
		desc := strings.TrimSpace(sk.Description)
		if desc == "" { // 无描述回退：正文首行截断，保目录可用性
			desc = firstLine(sk.Instruction, 60)
		}
		b.WriteString(fmt.Sprintf("- 「%s」：%s\n", sk.Name, desc))
	}
	return b.String()
}

// firstLine 取文本首行并截断（目录回退描述用）。
func firstLine(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
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
