// M16 阶段二（REQ-130）社区持久化：kg_community 表（重建即全量替换 = 缓存失效语义）。
package store

import "strings"

// KGCommunity 一个社区：label 为代表实体名，members 为成员实体名（含 label）。
type KGCommunity struct {
	ID          string   `json:"id"`
	KBID        string   `json:"kb_id"`
	Label       string   `json:"label"`
	Summary     string   `json:"summary"`
	Method      string   `json:"method,omitempty"` // llm | skeleton（摘要生成方式）
	Members     []string `json:"members"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

const kgCommunityCols = `id,kb_id,label,summary,method,members_json,created_at,updated_at`

func scanKGCommunity(row interface{ Scan(...any) error }) (*KGCommunity, error) {
	var c KGCommunity
	var members string
	if err := row.Scan(&c.ID, &c.KBID, &c.Label, &c.Summary, &c.Method, &members, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.Members = splitMembers(members)
	return &c, nil
}

func splitMembers(s string) []string {
	s = strings.Trim(s, "[]")
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ReplaceKGCommunities 全量替换某 KB 的社区（重建语义 = 摘要缓存失效）。
func (s *Store) ReplaceKGCommunities(kbID string, communities []*KGCommunity) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM kg_community WHERE kb_id=?`, kbID); err != nil {
		return err
	}
	ins, err := tx.Prepare(`INSERT INTO kg_community(` + kgCommunityCols + `) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	for _, c := range communities {
		if c.ID == "" {
			c.ID = NewID()
		}
		members := `["` + strings.Join(c.Members, `","`) + `"]`
		if _, err := ins.Exec(c.ID, kbID, c.Label, c.Summary, c.Method, members, now(), now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListKGCommunities 某 KB 的社区列表（按成员数降序，label 升序稳定排序）。
func (s *Store) ListKGCommunities(kbID string) ([]*KGCommunity, error) {
	rows, err := s.DB.Query(`SELECT ` + kgCommunityCols + ` FROM kg_community WHERE kb_id=?`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KGCommunity{}
	for rows.Next() {
		c, err := scanKGCommunity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sortCommunities(out)
	return out, rows.Err()
}

// DeleteKGCommunities 清空某 KB 的社区（KG 重建联动失效）。
func (s *Store) DeleteKGCommunities(kbID string) error {
	_, err := s.DB.Exec(`DELETE FROM kg_community WHERE kb_id=?`, kbID)
	return err
}

func sortCommunities(cs []*KGCommunity) {
	// 简单插入排序（学习尺度条目少，避免引入 sort 依赖 churn）
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			if len(b.Members) > len(a.Members) || (len(b.Members) == len(a.Members) && b.Label < a.Label) {
				cs[j-1], cs[j] = cs[j], cs[j-1]
			} else {
				break
			}
		}
	}
}
