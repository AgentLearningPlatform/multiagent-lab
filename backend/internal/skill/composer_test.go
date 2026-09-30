package skill

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func seededComposer(t *testing.T, skills ...*store.Skill) (*Composer, *store.Agent) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ag, err := st.CreateAgent(&store.Agent{Name: "host", Instruction: "你是运维助手"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sk := range skills {
		created, err := st.CreateSkill(sk)
		if err != nil {
			t.Fatal(err)
		}
		ag.Skills = append(ag.Skills, created.ID)
	}
	if _, err := st.UpdateAgent(ag); err != nil {
		t.Fatal(err)
	}
	return &Composer{Store: st}, ag
}

// REQ-203 B6：目录注入——含名称与描述，不含正文全文（渐进披露）。
func TestComposeInstructionCatalogNotFullBody(t *testing.T) {
	c, ag := seededComposer(t,
		&store.Skill{Name: "k8s-triage", Description: "K8s 排障三步法", Instruction: "第一步……\n第二步……\n第三步……（很长的正文）", Enabled: true},
		&store.Skill{Name: "no-desc", Instruction: "首行描述\n正文第二行", Enabled: true},
	)
	out := c.ComposeInstruction(ag)
	if !strings.Contains(out, "k8s-triage") || !strings.Contains(out, "K8s 排障三步法") {
		t.Fatalf("目录应含名称与描述: %s", out)
	}
	if strings.Contains(out, "第二步") || strings.Contains(out, "正文第二行") {
		t.Fatal("正文不应常驻提示词（渐进披露）")
	}
	if !strings.Contains(out, "load_skill") || !strings.Contains(out, "首行描述") {
		t.Fatalf("目录应指路 load_skill 且无描述技能回退首行: %s", out)
	}
	// 无技能 → base 原样
	c2, ag2 := seededComposer(t)
	if got := c2.ComposeInstruction(ag2); got != "你是运维助手" {
		t.Fatalf("无技能应原样: %q", got)
	}
}

// load_skill：已挂载取全文；未知回执拒绝（不炸 run 口径）。
func TestLoadSkillTool(t *testing.T) {
	bt, err := NewLoadSkillTool([]*store.Skill{{Name: "k8s-triage", Instruction: "完整正文 A", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	it := bt.(einotool.InvokableTool)
	if res, err := it.InvokableRun(context.Background(), `{"name":"k8s-triage"}`); err != nil || !strings.Contains(res, "完整正文 A") {
		t.Fatalf("应取到全文: %v %s", err, res)
	}
	if res, err := it.InvokableRun(context.Background(), `{"name":"unknown"}`); err != nil || !strings.Contains(res, "不在本 agent 已挂载清单") {
		t.Fatalf("未知技能应回执拒绝: %v %s", err, res)
	}
}
