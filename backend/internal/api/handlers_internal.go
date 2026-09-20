package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// manifest 沙箱配置下发载荷（§6.3，M10）。
// agentd 启动时凭一次性 token 拉取，在容器内完成 ADK 装配（与主平台共用 Assembler）。
type manifest struct {
	Agent      *store.Agent        `json:"agent"`
	ModelConns []manifestModelConn `json:"model_conns,omitempty"` // 装配所需连接（含明文 key，容器内解密边界）
	Skills     []*store.Skill      `json:"skills,omitempty"`      // 挂载技能（详情）
}

// manifestModelConn 连接下发条目：基本字段 + 明文 key。
type manifestModelConn struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ConnType  string `json:"conn_type"`
	Protocol  string `json:"protocol"`
	BaseURL   string `json:"base_url"`
	ModelName string `json:"model_name"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"is_default"`
	APIKey    string `json:"api_key,omitempty"`
}

// ---- 一次性 manifest token（内存登记，5 分钟过期，用后作废）----

type manifestGrant struct {
	AgentID string
	Expires time.Time
}

var (
	manifestMu     sync.Mutex
	manifestTokens = map[string]manifestGrant{}
)

// IssueManifestToken 为沙箱启动签发一次性 token。
func (s *Server) IssueManifestToken(agentID string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	manifestMu.Lock()
	defer manifestMu.Unlock()
	manifestTokens[tok] = manifestGrant{AgentID: agentID, Expires: time.Now().Add(5 * time.Minute)}
	return tok, nil
}

// validateManifestToken 校验并作废（一次性）。
func validateManifestToken(tok, agentID string) bool {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	g, ok := manifestTokens[tok]
	if !ok {
		return false
	}
	delete(manifestTokens, tok) // 一次性：校验即作废
	if time.Now().After(g.Expires) {
		return false
	}
	return g.AgentID == agentID
}

// getManifest GET /api/internal/agents/{id}/manifest?token=...（内部端点，仅沙箱回调）。
func (s *Server) getManifest(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	token := r.URL.Query().Get("token")
	if agentID == "" || !validateManifestToken(token, agentID) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired manifest token"})
		return
	}
	ag, err := s.Store.GetAgent(agentID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found: " + err.Error()})
		return
	}
	mf := manifest{Agent: ag}

	// 模型连接：agent 指定连接 + 全局默认（装配解析顺序一致）
	connIDs := []string{}
	if ag.ModelConnID != nil && *ag.ModelConnID != "" {
		connIDs = append(connIDs, *ag.ModelConnID)
	} else if def, derr := s.Store.GetDefaultConnection("chat"); derr == nil && def != nil {
		connIDs = append(connIDs, def.ID)
	}
	seen := map[string]bool{}
	for _, cid := range connIDs {
		if seen[cid] {
			continue
		}
		seen[cid] = true
		rec, rerr := s.Store.GetConnectionRecord(cid)
		if rerr != nil {
			continue
		}
		key := ""
		if len(rec.Encrypted) > 0 {
			if k, derr := s.Box.Decrypt(rec.Encrypted); derr == nil {
				key = k
			}
		}
		mf.ModelConns = append(mf.ModelConns, manifestModelConn{
			ID: rec.Conn.ID, Name: rec.Conn.Name, ConnType: rec.Conn.ConnType,
			Protocol: rec.Conn.Protocol, BaseURL: rec.Conn.BaseURL, ModelName: rec.Conn.ModelName,
			Enabled: rec.Conn.Enabled, IsDefault: rec.Conn.IsDefault, APIKey: key,
		})
	}

	// 技能（仅启用）
	for _, sid := range ag.Skills {
		if sk, serr := s.Store.GetSkill(sid); serr == nil && sk != nil && sk.Enabled {
			mf.Skills = append(mf.Skills, sk)
		}
	}

	writeJSON(w, http.StatusOK, mf)
}
