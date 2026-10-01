// Package evolution REQ-207/M43 本体自进化受控生长闭环（43 号 §5.1 + 46 号 OaK）：
// 诊断→归因→补丁→配对门控四步；候选 vN-cK 状态机（proposed→accepted/rejected）；
// Evidence 锚定；轮次预算；一期人工触发（无人值守自动循环不做）。
//
// 数据面：Store 扩展进化候选 CRUD；门控双信号=qualitygate.Check（save=false 内存评分）
// + structCheck（evaldata 同口径）。采纳=类型化补丁应用到 spec 后走既有 SaveVersion
// 链升正式版本——候选永不直接覆盖正式版本。
package evolution

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MaxRoundsPerPass 轮次预算上限（一次进化 pass 内最多尝试轮数；防无限迭代）。
const MaxRoundsPerPass = 3

// ErrNotFound 未找到。
var ErrNotFound = errors.New("not found")

// Candidate 进化候选补丁（vN-cK）。
type Candidate struct {
	ID          string `json:"id"`
	OntologyID  string `json:"ontology_id"`
	Label       string `json:"label"` // v{base}-c{K}
	BaseVersion int    `json:"base_version"`
	Layer       string `json:"layer"`   // content | tool | schema
	Summary     string `json:"summary"` // 归因摘要
	Evidence    string `json:"evidence"`
	PatchJSON   string `json:"patch_json"`
	GateReport  string `json:"gate_report"`
	Status      string `json:"status"` // proposed | accepted | rejected
	Round       int    `json:"round"`
	CreatedAt   string `json:"created_at"`
	DecidedAt   *string `json:"decided_at,omitempty"`
}

// Patch 类型化编辑（补丁形态；应用顺序：remove→add/update，防同名冲突）。
type Patch struct {
	AddConcepts    []map[string]any `json:"add_concepts,omitempty"`
	UpdateConcepts []map[string]any `json:"update_concepts,omitempty"` // 按 name 定位
	RemoveConcepts []string         `json:"remove_concepts,omitempty"`
	AddRelations   []map[string]any `json:"add_relations,omitempty"`
	RemoveRelations []map[string]any `json:"remove_relations,omitempty"` // name+from+to 定位
}

// Store 进化候选存储（复用 ontology-service 主库）。
type Store struct {
	db *sql.DB
}

// New 构造。
func New(db *sql.DB) *Store { return &Store{db: db} }

// Create 插入候选（label 由 base_version+同 base 序号 K 生成）。
func (s *Store) Create(c *Candidate) (*Candidate, error) {
	if c.ID == "" {
		c.ID = fmt.Sprintf("evo_%d", time.Now().UnixNano())
	}
	var k int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM evolution_candidate WHERE ontology_id=? AND base_version=?`, c.OntologyID, c.BaseVersion).Scan(&k); err != nil {
		return nil, err
	}
	c.Label = fmt.Sprintf("v%d-c%d", c.BaseVersion, k+1)
	if c.Status == "" {
		c.Status = "proposed"
	}
	if c.Round == 0 {
		c.Round = 1
	}
	_, err := s.db.Exec(`INSERT INTO evolution_candidate (id,ontology_id,label,base_version,layer,summary,evidence,patch_json,gate_report,status,round) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.OntologyID, c.Label, c.BaseVersion, c.Layer, c.Summary, c.Evidence, c.PatchJSON, c.GateReport, c.Status, c.Round)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Get 按 ID 查询。
func (s *Store) Get(id string) (*Candidate, error) {
	row := s.db.QueryRow(`SELECT id,ontology_id,label,base_version,layer,summary,evidence,patch_json,gate_report,status,round,created_at,decided_at FROM evolution_candidate WHERE id=?`, id)
	return scanOne(row)
}

// ListByOntology 本体候选列表（新→旧；status 过滤可选）。
func (s *Store) ListByOntology(ontologyID, status string) ([]*Candidate, error) {
	q := `SELECT id,ontology_id,label,base_version,layer,summary,evidence,patch_json,gate_report,status,round,created_at,decided_at FROM evolution_candidate WHERE ontology_id=?`
	var args []any
	args = append(args, ontologyID)
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Candidate
	for rows.Next() {
		c, err := scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Decide 裁决（accepted/rejected；仅 proposed 可裁决）。
func (s *Store) Decide(id, status string) (*Candidate, error) {
	if status != "accepted" && status != "rejected" {
		return nil, fmt.Errorf("status 须为 accepted|rejected")
	}
	res, err := s.db.Exec(`UPDATE evolution_candidate SET status=?, decided_at=CURRENT_TIMESTAMP WHERE id=? AND status='proposed'`, status, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.Get(id)
}

// CountRounds 本体已用轮次（诊断步预算控制）。
func (s *Store) CountRounds(ontologyID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(round),0) FROM evolution_candidate WHERE ontology_id=?`, ontologyID).Scan(&n)
	return n, err
}

func scanOne(row interface{ Scan(...any) error }) (*Candidate, error) {
	c := &Candidate{}
	var decided sql.NullString
	if err := row.Scan(&c.ID, &c.OntologyID, &c.Label, &c.BaseVersion, &c.Layer, &c.Summary, &c.Evidence, &c.PatchJSON, &c.GateReport, &c.Status, &c.Round, &c.CreatedAt, &decided); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if decided.Valid {
		c.DecidedAt = &decided.String
	}
	return c, nil
}

// ApplyPatch 类型化编辑应用到 spec（纯函数；remove→add/update 顺序）。返回应用计数。
func ApplyPatch(sp any, patchJSON string) (int, error) {
	// sp 以 map 形态操作（避免 import spec 包形成反向依赖——调用方断言回自己的类型）
	var p Patch
	if err := json.Unmarshal([]byte(patchJSON), &p); err != nil {
		return 0, err
	}
	m, ok := sp.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("spec 须为 map 形态")
	}
	applied := 0
	concepts, _ := m["concepts"].([]any)
	// remove concepts
	for _, name := range p.RemoveConcepts {
		kept := concepts[:0]
		for _, c := range concepts {
			cm := c.(map[string]any)
			if cm["name"] != name {
				kept = append(kept, c)
			}
		}
		concepts = kept
		applied++
	}
	// add concepts
	for _, c := range p.AddConcepts {
		concepts = append(concepts, c)
		applied++
	}
	// update concepts（按 name 定位）
	for _, uc := range p.UpdateConcepts {
		name, _ := uc["name"].(string)
		for i, c := range concepts {
			cm := c.(map[string]any)
			if cm["name"] == name {
				for k, v := range uc {
					cm[k] = v
				}
				concepts[i] = cm
				applied++
				break
			}
		}
	}
	m["concepts"] = concepts
	relations, _ := m["relations"].([]any)
	// remove relations（name+from+to 全匹配）
	for _, rr := range p.RemoveRelations {
		name, _ := rr["name"].(string)
		from, _ := rr["from"].(string)
		to, _ := rr["to"].(string)
		kept := relations[:0]
		for _, r := range relations {
			rm := r.(map[string]any)
			if rm["name"] != name || rm["from"] != from || rm["to"] != to {
				kept = append(kept, r)
			}
		}
		relations = kept
		applied++
	}
	// add relations
	for _, r := range p.AddRelations {
		relations = append(relations, r)
		applied++
	}
	m["relations"] = relations
	return applied, nil
}

// UpdateGateReport 刷新门控对照报告（状态不变）。
func (s *Store) UpdateGateReport(id, gateReport string) error {
	_, err := s.db.Exec(`UPDATE evolution_candidate SET gate_report=? WHERE id=?`, gateReport, id)
	return err
}
