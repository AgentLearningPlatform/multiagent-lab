package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// REQ-155/M-O15 阶段二：本体方案生命周期（Terraform 式 plan/apply，monitor=plan 的只读形态）。
//   期望态 = 方案配置本身（runtime_profile 声明的引擎与本体集合，全部声明为期望运行）；
//   实际态 = 运行平面回报的 status + 启动/重载时记录的加载版本快照（loaded_versions）；
//   plan   = 期望-实际差异 → 动作清单（start=未运行 / reload=运行中但任一本体 spec 版本超前于加载快照=漂移）；
//   apply  = 逐项执行动作（经主平台反代调运行平面 start/reload），stop 不入 plan（声明式语义：摘除方案=删配置）。
// 漂移信号依赖 loaded_versions（M-O15 阶段二随本交付引入；未记录快照的旧运行方案标注 unknown，不误报）。
// ---------------------------------------------------------------------------

// LifecycleAction 单条计划动作。
type LifecycleAction struct {
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Engine      string `json:"engine,omitempty"`
	Action      string `json:"action"` // start | reload
	Reason      string `json:"reason"` // 人类可读原因（ drifted: spec vX → vY 等）
}

// LifecyclePlan 生命周期计划。
type LifecyclePlan struct {
	GeneratedAt string            `json:"generated_at"`
	Profiles    []LifecycleStatus `json:"profiles"`
	Actions     []LifecycleAction `json:"actions"`
}

// LifecycleStatus 单方案状态行（面板渲染）。
type LifecycleStatus struct {
	ProfileID    string         `json:"profile_id"`
	ProfileName  string         `json:"profile_name"`
	Engine       string         `json:"engine"`
	Status       string         `json:"status"` // running | stopped | error | starting | created
	Ontologies   []string       `json:"ontologies"`
	Loaded       map[string]int `json:"loaded"`            // 加载快照（可能缺）
	Current      map[string]int `json:"current"`           // 当前 spec 版本
	Drifted      []string       `json:"drifted,omitempty"` // 漂移本体名（版本超前）
	UnknownDrift bool           `json:"unknown_drift"`     // 运行中但无快照（旧数据，建议 reload 建立基线）
	LastError    string         `json:"last_error,omitempty"`
}

type profileDTO struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Engine         string   `json:"engine"`
	OntologyIDs    []string `json:"ontology_ids"`
	Status         string   `json:"status"`
	LastError      string   `json:"last_error,omitempty"`
	LoadedVersions string   `json:"loaded_versions,omitempty"`
}

// Plan 拉取运行平面方案清单并与当前 spec 版本对比，产出计划（HTTP 只读）。
func Plan(ctx context.Context, platformURL string, currentVersions map[string]int) (*LifecyclePlan, error) {
	profiles, err := fetchProfiles(ctx, platformURL)
	if err != nil {
		return nil, err
	}
	plan := &LifecyclePlan{GeneratedAt: time.Now().UTC().Format(time.RFC3339), Profiles: []LifecycleStatus{}, Actions: []LifecycleAction{}}
	for _, p := range profiles {
		st := LifecycleStatus{
			ProfileID: p.ID, ProfileName: p.Name, Engine: p.Engine, Status: p.Status,
			Ontologies: p.OntologyIDs, Current: map[string]int{}, Loaded: map[string]int{},
			LastError: p.LastError,
		}
		for _, oid := range p.OntologyIDs {
			st.Current[oid] = currentVersions[oid]
		}
		if p.LoadedVersions != "" {
			_ = json.Unmarshal([]byte(p.LoadedVersions), &st.Loaded)
		}
		switch p.Status {
		case "running":
			for _, oid := range p.OntologyIDs {
				loaded, ok := st.Loaded[oid]
				if !ok {
					st.UnknownDrift = true // 无基线快照（旧运行），不误报版本漂移
					continue
				}
				if cur := st.Current[oid]; cur != loaded {
					st.Drifted = append(st.Drifted, oid)
				}
			}
			if len(st.Drifted) > 0 {
				plan.Actions = append(plan.Actions, LifecycleAction{
					ProfileID: p.ID, ProfileName: p.Name, Engine: p.Engine, Action: "reload",
					Reason: fmt.Sprintf("spec 版本漂移：%s（加载快照落后于当前版本）", strings.Join(st.Drifted, "、")),
				})
			} else if st.UnknownDrift {
				plan.Actions = append(plan.Actions, LifecycleAction{
					ProfileID: p.ID, ProfileName: p.Name, Engine: p.Engine, Action: "reload",
					Reason: "运行中但无加载版本基线（旧数据）——重载建立快照",
				})
			}
		case "stopped", "created", "error":
			if len(p.OntologyIDs) > 0 {
				plan.Actions = append(plan.Actions, LifecycleAction{
					ProfileID: p.ID, ProfileName: p.Name, Engine: p.Engine, Action: "start",
					Reason: map[string]string{
						"stopped": "已声明未运行", "created": "已创建未启动",
						"error": "上次启动失败（" + p.LastError + "）",
					}[p.Status],
				})
			}
		}
		plan.Profiles = append(plan.Profiles, st)
	}
	return plan, nil
}

// ApplyAction 执行一条动作（start/reload 经主平台反代调运行平面）。
func ApplyAction(ctx context.Context, platformURL string, a LifecycleAction) error {
	verb := a.Action
	if verb != "start" && verb != "reload" {
		return fmt.Errorf("不支持的动作 %q（仅 start|reload）", verb)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(platformURL, "/")+fmt.Sprintf("/api/runtime-profiles/%s/%s", a.ProfileID, verb),
		strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("运行平面不可达: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s 失败: %s", verb, a.ProfileID, strings.TrimSpace(string(b)))
	}
	return nil
}

func fetchProfiles(ctx context.Context, platformURL string) ([]profileDTO, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(platformURL, "/")+"/api/runtime-profiles", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("运行平面不可达（经主平台）: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("运行平面 %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out []profileDTO
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("方案清单解析失败: %w", err)
	}
	return out, nil
}
