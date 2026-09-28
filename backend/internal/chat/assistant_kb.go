package chat

// ---------------------------------------------------------------------------
// REQ-186 阶段一/阶段三（M-O14 流水线队列）：平台知识 KB 化 + L1 写提案两段式。
//
// 阶段一 KB 化：EnsurePlatformKB（专用 rag 库，按名称幂等）+ SyncPlatformKB（扫描
// platform-knowledge/**/*.md，mtime 增量对账：文件新于文档→重导入，文件消失→删文档）+
// L0 工具 sync_platform_kb / search_platform_kb（KB 向量检索 topK=5；embedding 未配置时
// 如实报错降级——doc_read 词法精读不受影响）。
//
// 阶段三 L1 提案：propose_assistant_config 只产出提案（内存暂存 10 分钟 TTL），不直接写；
// 确认动作在设置页「平台助手」分区（GET /api/assistant/proposal 拉取 → 应用/忽略）——
// 两段式语义：确认前不落库；后端重启丢失未确认提案为安全默认。
// ---------------------------------------------------------------------------

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

const platformKBName = "platform-knowledge 平台知识库"

// EnsurePlatformKB 按名称取/建专用 rag 库（幂等）。
func EnsurePlatformKB(ctx context.Context, st *store.Store, kbSvc *kb.Service) (*store.KnowledgeBase, error) {
	if kbs, err := st.ListKnowledgeBases(); err == nil {
		for _, k := range kbs {
			if k.Name == platformKBName {
				return k, nil
			}
		}
	}
	k := &store.KnowledgeBase{Name: platformKBName, Description: "平台知识目录（platform-knowledge/）向量化镜像——search_platform_kb 检索源；由 sync_platform_kb 增量对账维护", Mode: "rag"}
	if _, err := st.CreateKnowledgeBase(k); err != nil {
		return nil, err
	}
	return st.GetKnowledgeBase(k.ID)
}

// SyncResult 增量对账结果（诚实呈现部分失败）。
type SyncResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Deleted  int      `json:"deleted"`
	Skipped  int      `json:"skipped"`
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors,omitempty"`
	Total    int      `json:"total"`
}

// SyncPlatformKB 扫描 platform-knowledge/**/*.md 与 KB 文档对账（mtime 增量）。
// 返回对账结果；个别文件索引失败（如向量化不可用）计入 Failed/Errors 不阻断其余。
func SyncPlatformKB(ctx context.Context, st *store.Store, kbSvc *kb.Service, knowledgeRoot string) (*SyncResult, error) {
	kbRow, err := EnsurePlatformKB(ctx, st, kbSvc)
	if err != nil {
		return nil, err
	}
	res := &SyncResult{}

	// ① 扫描文件（relpath → mtime）
	type entry struct {
		rel   string
		mtime time.Time
	}
	var files []entry
	walkErr := filepath.WalkDir(knowledgeRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(knowledgeRoot, path)
		if rerr != nil {
			return nil
		}
		info, _ := d.Info()
		mt := time.Time{}
		if info != nil {
			mt = info.ModTime()
		}
		files = append(files, entry{rel: "platform-knowledge/" + filepath.ToSlash(rel), mtime: mt})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	res.Total = len(files)

	// ② 现有文档（title=relpath）
	docs, _ := st.ListKnowledgeDocs(kbRow.ID)
	byTitle := map[string]*store.KnowledgeDoc{}
	for _, d := range docs {
		byTitle[d.Title] = d
	}

	// ③ 对账：新增/更新/跳过/删除
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.rel] = true
		cur := byTitle[f.rel]
		if cur != nil && f.mtime.Before(parseTime(cur.UpdatedAt)) {
			res.Skipped++
			continue
		}
		content, rerr := os.ReadFile(filepath.Join(knowledgeRoot, strings.TrimPrefix(f.rel, "platform-knowledge/")))
		if rerr != nil {
			res.Failed++
			res.Errors = append(res.Errors, f.rel+": "+rerr.Error())
			continue
		}
		if cur != nil {
			_ = kbSvc.DeleteDoc(ctx, kbRow.ID, cur.ID)
			res.Deleted++
		}
		if _, ierr := kbSvc.Import(ctx, kbRow.ID, f.rel, string(content)); ierr != nil {
			res.Failed++
			res.Errors = append(res.Errors, f.rel+": "+ierr.Error())
			continue
		}
		if cur != nil {
			res.Updated++
		} else {
			res.Imported++
		}
	}
	// 文件已删除的文档清退
	for title, d := range byTitle {
		if !seen[title] {
			if derr := kbSvc.DeleteDoc(ctx, kbRow.ID, d.ID); derr == nil {
				res.Deleted++
			}
		}
	}
	sort.Strings(res.Errors)
	return res, nil
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// assistantSyncPlatformKB L0：平台知识库增量对账（repo 派生、幂等——非用户数据写入）。
func assistantSyncPlatformKB(deps AssistantDeps, kbSvc *kb.Service) *tool.Entry {
	return &tool.Entry{
		ID:          "sync_platform_kb",
		Name:        "sync_platform_kb",
		Description: "对 platform-knowledge/ 平台知识库做增量对账（扫描 md 文件与向量库差异并同步，幂等只镜像仓库内容）——回答平台知识类问题前建议先同步一次",
		Source:      tool.SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			fn := func(ctx context.Context, _ struct{}) (string, error) {
				res, err := SyncPlatformKB(ctx, deps.Store, kbSvc, deps.KnowledgeRoot)
				if err != nil {
					return "", err
				}
				msg := fmt.Sprintf("平台知识库对账完成：新增 %d / 更新 %d / 删除 %d / 未变 %d / 失败 %d（共 %d 个 md）", res.Imported, res.Updated, res.Deleted, res.Skipped, res.Failed, res.Total)
				if len(res.Errors) > 0 {
					msg += "；失败明细：" + strings.Join(res.Errors[:minInt(3, len(res.Errors))], "；")
				}
				return msg, nil
			}
			return utils.InferTool("sync_platform_kb", "对 platform-knowledge/ 平台知识库做增量对账（幂等只镜像仓库内容）——回答平台知识类问题前建议先同步一次", fn)
		},
	}
}

// assistantSearchPlatformKB L0：平台知识库向量检索 topK=5（embedding 未配置时如实报错降级）。
func assistantSearchPlatformKB(deps AssistantDeps, kbSvc *kb.Service) *tool.Entry {
	type in struct {
		Query string `json:"query"`
	}
	newFn := func(ctx context.Context) (einotool.BaseTool, error) {
		fn := func(ctx context.Context, args in) (string, error) {
		q := strings.TrimSpace(args.Query)
		if q == "" {
			return "", fmt.Errorf("query 必填")
		}
		kbRow, err := EnsurePlatformKB(ctx, deps.Store, kbSvc)
		if err != nil {
			return "", err
		}
		hits, serr := kbSvc.Search(ctx, kbRow, q, 5, 0)
		if serr != nil {
			return "", fmt.Errorf("平台知识库检索不可用（embedding 未配置或索引为空——可先调 sync_platform_kb 同步，或用 doc_read 词法精读）: %w", serr)
		}
		if len(hits) == 0 {
			return "平台知识库无命中——可尝试其他关键词，或用 doc_read 直接精读 platform-knowledge/ 下文档。", nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("平台知识库命中 %d 条（topK=5）：\n", len(hits)))
		for i, h := range hits {
			b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, h.Doc, truncateForPrompt(h.Excerpt, 300)))
		}
		b.WriteString("（可对上述文档路径调 doc_read 精读全文）")
		return b.String(), nil
		}
		return utils.InferTool("search_platform_kb", "检索 platform-knowledge/ 平台知识库（向量 topK=5），回答平台使用/功能/设计问题前的首选知识源。入参 query 为检索问题", fn)
	}
	return &tool.Entry{
		ID:          "search_platform_kb",
		Name:        "search_platform_kb",
		Description: "检索 platform-knowledge/ 平台知识库（向量 topK=5）——平台使用/功能/设计问题的首选知识源",
		Source:      tool.SourceBuiltin,
		New:         newFn,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateForPrompt(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// ---------------------------------------------------------------------------
// L1 提案暂存（阶段三）：内存 map + 10 分钟 TTL；确认在设置页平台助手分区完成。
// ---------------------------------------------------------------------------

type AssistantProposal struct {
	ID        string               `json:"proposal_id"`
	Changes   []map[string]string  `json:"changes"`
	Current   *store.AssistantConfig `json:"current"`
	Proposed  *store.AssistantConfig `json:"proposed"`
	CreatedAt string               `json:"created_at"`
}

var (
	proposalMu       sync.Mutex
	proposalRegistry = map[string]*AssistantProposal{}
	proposalExpiry   = map[string]time.Time{}
	proposalTTL      = 10 * time.Minute
)

// StageAssistantProposal 暂存提案（至少一项变更；幂等替换同内容提案）。
func StageAssistantProposal(cur *store.AssistantConfig, prop *store.AssistantConfig) *AssistantProposal {
	proposalMu.Lock()
	defer proposalMu.Unlock()
	// 清过期
	now := time.Now()
	for id, exp := range proposalExpiry {
		if now.After(exp) {
			delete(proposalRegistry, id)
			delete(proposalExpiry, id)
		}
	}
	var changes []map[string]string
	add := func(field, from, to string) {
		changes = append(changes, map[string]string{"field": field, "from": from, "to": to})
	}
	if prop.SystemPrompt != "" && prop.SystemPrompt != cur.SystemPrompt {
		add("system_prompt", truncateForPrompt(cur.SystemPrompt, 60), truncateForPrompt(prop.SystemPrompt, 60))
	}
	if prop.ModelConnID != "" && prop.ModelConnID != cur.ModelConnID {
		add("model_conn_id", cur.ModelConnID, prop.ModelConnID)
	}
	if prop.Temperature != nil && (cur.Temperature == nil || *prop.Temperature != *cur.Temperature) {
		add("temperature", fmt.Sprintf("%v", cur.Temperature), fmt.Sprintf("%v", *prop.Temperature))
	}
	id := fmt.Sprintf("prop-%x", md5.Sum([]byte(fmt.Sprintf("%v|%v|%v|%d", prop.SystemPrompt, prop.ModelConnID, prop.Temperature, now.Unix()))))
	p := &AssistantProposal{ID: id, Changes: changes, Current: cur, Proposed: prop, CreatedAt: now.UTC().Format(time.RFC3339)}
	proposalRegistry[id] = p
	proposalExpiry[id] = now.Add(proposalTTL)
	return p
}

// TakeAssistantProposal 取出并删除提案（一次性——应用/忽略后不可复用）；不存在/过期返回 nil。
func TakeAssistantProposal(id string) *AssistantProposal {
	proposalMu.Lock()
	defer proposalMu.Unlock()
	p := proposalRegistry[id]
	if p == nil {
		return nil
	}
	if time.Now().After(proposalExpiry[id]) {
		delete(proposalRegistry, id)
		delete(proposalExpiry, id)
		return nil
	}
	delete(proposalRegistry, id)
	delete(proposalExpiry, id)
	return p
}

// PeekAssistantProposal 只读查看最新提案（设置页拉取；不消费）。
func PeekAssistantProposal() *AssistantProposal {
	proposalMu.Lock()
	defer proposalMu.Unlock()
	var latest *AssistantProposal
	var latestTs time.Time
	for _, p := range proposalRegistry {
		if time.Now().After(proposalExpiry[p.ID]) {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, p.CreatedAt)
		if latest == nil || ts.After(latestTs) {
			latest, latestTs = p, ts
		}
	}
	return latest
}

// assistantProposeConfig L1：只产出提案（不落库）——确认在设置页平台助手分区完成。
func assistantProposeConfig(deps AssistantDeps) *tool.Entry {
	type in struct {
		SystemPrompt string   `json:"system_prompt,omitempty"`
		ModelConnID  string   `json:"model_conn_id,omitempty"`
		Temperature  *float64 `json:"temperature,omitempty"`
	}
	newFn := func(ctx context.Context) (einotool.BaseTool, error) {
		fn := func(ctx context.Context, args in) (string, error) {
		if strings.TrimSpace(args.SystemPrompt) == "" && args.ModelConnID == "" && args.Temperature == nil {
			return "", fmt.Errorf("至少提供一项变更（system_prompt / model_conn_id / temperature）")
		}
		if args.Temperature != nil && (*args.Temperature < 0 || *args.Temperature > 2) {
			return "", fmt.Errorf("temperature 取值 0~2")
		}
		cur, err := deps.Store.GetAssistantConfig()
		if err != nil {
			return "", err
		}
		prop := &store.AssistantConfig{SystemPrompt: args.SystemPrompt, ModelConnID: args.ModelConnID, Temperature: args.Temperature}
		p := StageAssistantProposal(cur, prop)
		if len(p.Changes) == 0 {
			return "提案与现行配置无差异——无需变更。", nil
		}
		return fmt.Sprintf("配置提案已暂存（proposal_id=%s，10 分钟内有效，确认前不落库）。变更项：%v。请到「设置 → 平台助手」分区查看并确认应用；用户确认后才会写入配置。", p.ID, p.Changes), nil
		}
		return utils.InferTool("propose_assistant_config", "L1 两段式提案：产出平台助手配置变更提案（system_prompt/model_conn_id/temperature 任一或多项），暂存 10 分钟且不落库；用户需到「设置 → 平台助手」分区确认应用。用户要求调整助手配置时用此工具，绝不可声称已直接修改配置", fn)
	}
	return &tool.Entry{
		ID:          "propose_assistant_config",
		Name:        "propose_assistant_config",
		Description: "L1 两段式提案：产出平台助手配置变更提案（暂存不落库），用户在设置页平台助手分区确认应用",
		Source:      tool.SourceBuiltin,
		New:         newFn,
	}
}

// RegisterAssistantL1Tools 注册 L1 提案与平台知识工具（阶段一/三；在 L0 注册后调用，幂等）。
func RegisterAssistantL1Tools(r *tool.Registry, deps AssistantDeps, kbSvc *kb.Service) error {
	entries := []*tool.Entry{
		assistantSyncPlatformKB(deps, kbSvc),
		assistantSearchPlatformKB(deps, kbSvc),
		assistantProposeConfig(deps),
	}
	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}
