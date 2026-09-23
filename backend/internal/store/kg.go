package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ---- KG 自存 + 审计决策（D-O15/REQ-110：去-semantica 化，KG 一等数据落主平台库）----
//
// 三表 = 教学口径「实体 / 关系 / claim」；claim 带 chunk_id 溯源，
// 支撑 GraphRAG 一跳扩展拼上下文与 PROV-O 导出（REQ-101 审计学习视图）。

// KGEntity KG 实体（name 库内业务键；同 KB 跨 doc 同名实体视为同一实体，重建按 doc 覆盖）。
type KGEntity struct {
	ID          string `json:"id"`
	KBID        string `json:"kb_id"`
	DocID       string `json:"doc_id,omitempty"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// KGRelationship KG 关系（source/target 引用实体 name；type 教学口径用大写短语，如 IS_A/具有/引发）。
type KGRelationship struct {
	ID        string `json:"id"`
	KBID      string `json:"kb_id"`
	DocID     string `json:"doc_id,omitempty"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Type      string `json:"type,omitempty"`
	// Status 审核状态（M16/REQ-129：approved|rejected；rejected 不参与检索）
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// KGClaim claim：实体的一条可溯源陈述（原文句），chunk_id 指向出处片段。
type KGClaim struct {
	ID        string `json:"id"`
	KBID      string `json:"kb_id"`
	DocID     string `json:"doc_id,omitempty"`
	ChunkID   string `json:"chunk_id,omitempty"`
	Subject   string `json:"subject"`
	Text      string `json:"text"`
	// Status 审核状态（M16/REQ-129：approved|rejected；rejected 不参与检索）
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// OntoDecision 审计决策留痕（derived_from 指向前置决策，构成溯源链）。
type OntoDecision struct {
	ID          string `json:"id"`
	SubjectKind string `json:"subject_kind"` // kg | ontology | kb | manual
	SubjectID   string `json:"subject_id"`
	Title       string `json:"title"`
	Rationale   string `json:"rationale,omitempty"`
	DerivedFrom string `json:"derived_from,omitempty"`
	MetaJSON    string `json:"meta_json,omitempty"`
	CreatedAt   string `json:"created_at"`
}

const kgEntityCols = `id,kb_id,doc_id,name,type,description,created_at`
const kgRelCols = `id,kb_id,doc_id,source,target,rel_type,status,created_at`
const kgClaimCols = `id,kb_id,doc_id,chunk_id,subject,text,status,created_at`
const decisionCols = `id,subject_kind,subject_id,title,rationale,derived_from,meta_json,created_at`

func scanKGEntity(row interface{ Scan(...any) error }) (*KGEntity, error) {
	var e KGEntity
	if err := row.Scan(&e.ID, &e.KBID, &e.DocID, &e.Name, &e.Type, &e.Description, &e.CreatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

func scanKGRel(row interface{ Scan(...any) error }) (*KGRelationship, error) {
	var r KGRelationship
	if err := row.Scan(&r.ID, &r.KBID, &r.DocID, &r.Source, &r.Target, &r.Type, &r.Status, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func scanKGClaim(row interface{ Scan(...any) error }) (*KGClaim, error) {
	var c KGClaim
	if err := row.Scan(&c.ID, &c.KBID, &c.DocID, &c.ChunkID, &c.Subject, &c.Text, &c.Status, &c.CreatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func scanDecision(row interface{ Scan(...any) error }) (*OntoDecision, error) {
	var d OntoDecision
	if err := row.Scan(&d.ID, &d.SubjectKind, &d.SubjectID, &d.Title, &d.Rationale, &d.DerivedFrom, &d.MetaJSON, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// ReplaceKGForDoc 事务替换 KG 抽取结果（重索引/重建幂等）：
//   - docID 非空：只替换该 doc 的行；同 KB 跨 doc 同名实体先清他 doc 同名再插入（实体归一）；
//   - docID 为空（KB 级重建）：清空该 KB 全部 KG 行后插入。
func (s *Store) ReplaceKGForDoc(kbID, docID string, entities []*KGEntity, rels []*KGRelationship, claims []*KGClaim) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	entityWhere, relWhere, claimWhere := `kb_id = ? AND doc_id = ?`, `kb_id = ? AND doc_id = ?`, `kb_id = ? AND doc_id = ?`
	delArgs := []any{kbID, docID}
	if docID == "" { // KB 级重建：清空该库全部 KG 行
		entityWhere, relWhere, claimWhere = `kb_id = ?`, `kb_id = ?`, `kb_id = ?`
		delArgs = []any{kbID}
	}
	if _, err := tx.Exec(`DELETE FROM kg_entity WHERE `+entityWhere, delArgs...); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_relationship WHERE `+relWhere, delArgs...); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_claim WHERE `+claimWhere, delArgs...); err != nil {
		return err
	}
	if docID != "" && len(entities) > 0 { // 同 KB 跨 doc 同名实体归一：先清他 doc 同名，再插本 doc 版本
		if _, err := tx.Exec(`DELETE FROM kg_entity WHERE kb_id = ? AND doc_id != ? AND name IN (`+
			strings.TrimRight(strings.Repeat("?,", len(entities)), ",")+`)`,
			append([]any{kbID, docID}, kgNames(entities)...)...); err != nil {
			return err
		}
	}
	ie, err := tx.Prepare(`INSERT INTO kg_entity(` + kgEntityCols + `) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ie.Close()
	for _, e := range entities {
		if _, err := ie.Exec(e.ID, e.KBID, e.DocID, e.Name, e.Type, e.Description, now()); err != nil {
			return err
		}
	}
	ir, err := tx.Prepare(`INSERT INTO kg_relationship(` + kgRelCols + `) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ir.Close()
	for _, r := range rels {
		if _, err := ir.Exec(r.ID, r.KBID, r.DocID, r.Source, r.Target, r.Type, "approved", now()); err != nil {
			return err
		}
	}
	ic, err := tx.Prepare(`INSERT INTO kg_claim(` + kgClaimCols + `) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ic.Close()
	for _, c := range claims {
		if _, err := ic.Exec(c.ID, c.KBID, c.DocID, c.ChunkID, c.Subject, c.Text, "approved", now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func kgNames(entities []*KGEntity) []any {
	out := make([]any, 0, len(entities))
	for _, e := range entities {
		out = append(out, e.Name)
	}
	return out
}

// KGByKB 某 KB 的全量 KG 子图（O13 策略 B/C 数据源 + 消费页图谱展示）。
func (s *Store) KGByKB(kbID string) ([]*KGEntity, []*KGRelationship, error) {
	rows, err := s.DB.Query(`SELECT `+kgEntityCols+` FROM kg_entity WHERE kb_id = ? ORDER BY name`, kbID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	entities := []*KGEntity{}
	for rows.Next() {
		e, err := scanKGEntity(rows)
		if err != nil {
			return nil, nil, err
		}
		entities = append(entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows2, err := s.DB.Query(`SELECT `+kgRelCols+` FROM kg_relationship WHERE kb_id = ? ORDER BY source, target`, kbID)
	if err != nil {
		return nil, nil, err
	}
	defer rows2.Close()
	rels := []*KGRelationship{}
	for rows2.Next() {
		r, err := scanKGRel(rows2)
		if err != nil {
			return nil, nil, err
		}
		rels = append(rels, r)
	}
	return entities, rels, rows2.Err()
}

// KGCounts KB 的 KG 规模（选择器徽标 / kg_ready 判定）。
func (s *Store) KGCounts(kbID string) (entities, relationships int, err error) {
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM kg_entity WHERE kb_id = ?`, kbID).Scan(&entities); err != nil {
		return 0, 0, err
	}
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM kg_relationship WHERE kb_id = ?`, kbID).Scan(&relationships)
	return entities, relationships, err
}

// KGMethod 抽取方式不落 KG 表：记录在重建时写入的审计决策行（subject_kind='kg'）meta_json，
// 由 kg 包读取（教学口径：抽取方式本身即一条决策）。

// KGSubjectsByChunks 命中 chunk → 提及实体名（GraphRAG 一跳扩展第一步）。
func (s *Store) KGSubjectsByChunks(kbID string, chunkIDs []string) ([]string, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}
	q := `SELECT DISTINCT subject FROM kg_claim WHERE kb_id = ? AND status = 'approved' AND chunk_id IN (` +
		strings.TrimRight(strings.Repeat("?,", len(chunkIDs)), ",") + `)`
	args := make([]any, 0, len(chunkIDs)+1)
	args = append(args, kbID)
	for _, id := range chunkIDs {
		args = append(args, id)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// KGNeighbors 一跳扩展：与 seed 实体相关的关系（source 或 target 命中）。
func (s *Store) KGNeighbors(kbID string, subjects []string) ([]*KGRelationship, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	q := `SELECT ` + kgRelCols + ` FROM kg_relationship WHERE kb_id = ? AND status = 'approved' AND (source IN (` +
		strings.TrimRight(strings.Repeat("?,", len(subjects)), ",") + `) OR target IN (` +
		strings.TrimRight(strings.Repeat("?,", len(subjects)), ",") + `)) ORDER BY source, target`
	args := []any{kbID}
	for _, n := range subjects {
		args = append(args, n)
	}
	for _, n := range subjects {
		args = append(args, n)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KGRelationship{}
	for rows.Next() {
		r, err := scanKGRel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// KGClaimsForSubjects 实体的 claims（一跳上下文材料；限 limit 防 prompt 失控）。
func (s *Store) KGClaimsForSubjects(kbID string, subjects []string, limit int) ([]*KGClaim, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 24
	}
	q := `SELECT ` + kgClaimCols + ` FROM kg_claim WHERE kb_id = ? AND status = 'approved' AND subject IN (` +
		strings.TrimRight(strings.Repeat("?,", len(subjects)), ",") + `) ORDER BY created_at LIMIT ?`
	args := []any{kbID}
	for _, n := range subjects {
		args = append(args, n)
	}
	args = append(args, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KGClaim{}
	for rows.Next() {
		c, err := scanKGClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// KGEntitiesByNames 实体详情（desc/type 回填一跳结果）。
func (s *Store) KGEntitiesByNames(kbID string, names []string) (map[string]*KGEntity, error) {
	out := map[string]*KGEntity{}
	if len(names) == 0 {
		return out, nil
	}
	q := `SELECT ` + kgEntityCols + ` FROM kg_entity WHERE kb_id = ? AND name IN (` +
		strings.TrimRight(strings.Repeat("?,", len(names)), ",") + `)`
	args := []any{kbID}
	for _, n := range names {
		args = append(args, n)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanKGEntity(rows)
		if err != nil {
			return nil, err
		}
		out[e.Name] = e
	}
	return out, rows.Err()
}

// ---- M16 阶段一（REQ-127/128）：图谱统计、实体搜索、带溯源 claims ----

// KGClaimTrace 带 chunk 溯源的 claim（doc_id/seq 定位原文片段）。
type KGClaimTrace struct {
	KGClaim
	DocID    string `json:"trace_doc_id,omitempty"`
	ChunkSeq int    `json:"trace_seq,omitempty"`
}

// KGStats 图谱统计卡数据（REQ-127）。
type KGStats struct {
	Entities       int            `json:"entities"`
	Relationships  int            `json:"relationships"`
	Claims         int            `json:"claims"`
	EntityTypeDist map[string]int `json:"entity_type_dist"`
	RelTypeDist    map[string]int `json:"rel_type_dist"`
	DocsTotal      int            `json:"docs_total"`
	DocsWithKG     int            `json:"docs_with_kg"`
	DegradedDocs   int            `json:"degraded_docs"`
}

// KGStatsForKB 统计：三表计数 + 类型分布 + 文档覆盖（无 KG 行的文档即 degraded/未覆盖）。
func (s *Store) KGStatsForKB(kbID string) (*KGStats, error) {
	st := &KGStats{EntityTypeDist: map[string]int{}, RelTypeDist: map[string]int{}}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_entity WHERE kb_id = ?`, kbID).Scan(&st.Entities); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_relationship WHERE kb_id = ?`, kbID).Scan(&st.Relationships); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_claim WHERE kb_id = ?`, kbID).Scan(&st.Claims); err != nil {
		return nil, err
	}
	erows, err := s.DB.Query(`SELECT COALESCE(type,''), COUNT(*) FROM kg_entity WHERE kb_id = ? GROUP BY type`, kbID)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var t string
		var n int
		if err := erows.Scan(&t, &n); err != nil {
			return nil, err
		}
		st.EntityTypeDist[t] = n
	}
	erows.Err()
	rrows, err := s.DB.Query(`SELECT COALESCE(rel_type,''), COUNT(*) FROM kg_relationship WHERE kb_id = ? GROUP BY rel_type`, kbID)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var t string
		var n int
		if err := rrows.Scan(&t, &n); err != nil {
			return nil, err
		}
		st.RelTypeDist[t] = n
	}
	rrows.Err()
	// 文档覆盖：kb_doc 全集 vs kg_entity 出现过的 doc_id
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM knowledge_doc WHERE kb_id = ?`, kbID).Scan(&st.DocsTotal); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COUNT(DISTINCT doc_id) FROM kg_entity WHERE kb_id = ? AND doc_id != ''`, kbID).Scan(&st.DocsWithKG); err != nil {
		return nil, err
	}
	return st, nil
}

// KGSearchEntities 实体名模糊搜索（图谱页搜索框；LIKE 转义由调用方保证，%/_ 通配在此转义）。
func (s *Store) KGSearchEntities(kbID, q string, limit int) ([]*KGEntity, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q = strings.ReplaceAll(q, "%", "\\%")
	q = strings.ReplaceAll(q, "_", "\\_")
	rows, err := s.DB.Query(
		`SELECT `+kgEntityCols+` FROM kg_entity WHERE kb_id = ? AND name LIKE '%' || ? || '%' ESCAPE '\' ORDER BY name LIMIT ?`,
		kbID, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KGEntity{}
	for rows.Next() {
		e, err := scanKGEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// KGClaimsWithChunks claims 附 chunk 溯源（doc_id/seq，图谱页点击 claim 定位原文）。
func (s *Store) KGClaimsWithChunks(kbID string, subjects []string, limit int) ([]*KGClaimTrace, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 24
	}
	q := `SELECT c.id,c.kb_id,c.doc_id,c.chunk_id,c.subject,c.text,c.status,c.created_at, COALESCE(ch.doc_id,''), COALESCE(ch.seq,0)
		FROM kg_claim c
		LEFT JOIN knowledge_chunk ch ON ch.id = c.chunk_id
		WHERE c.kb_id = ? AND c.status = 'approved' AND c.subject IN (` +
		strings.TrimRight(strings.Repeat("?,", len(subjects)), ",") + `)
		ORDER BY c.created_at LIMIT ?`
	args := []any{kbID}
	for _, n := range subjects {
		args = append(args, n)
	}
	args = append(args, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KGClaimTrace{}
	for rows.Next() {
		var t KGClaimTrace
		var doc sql.NullString
		if err := rows.Scan(&t.ID, &t.KBID, &t.DocID, &t.ChunkID, &t.Subject, &t.Text, &t.Status, &t.CreatedAt, &doc, &t.ChunkSeq); err != nil {
			return nil, err
		}
		if doc.Valid {
			t.DocID = doc.String
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// deleteKG 级联删除 KG 行（docID 空 = 整库）。审计决策保留（决策史不随数据删除，教学口径）。
func (s *Store) deleteKG(kbID, docID string) {
	if docID == "" {
		s.DB.Exec(`DELETE FROM kg_entity WHERE kb_id = ?`, kbID)
		s.DB.Exec(`DELETE FROM kg_relationship WHERE kb_id = ?`, kbID)
		s.DB.Exec(`DELETE FROM kg_claim WHERE kb_id = ?`, kbID)
		return
	}
	s.DB.Exec(`DELETE FROM kg_entity WHERE kb_id = ? AND doc_id = ?`, kbID, docID)
	s.DB.Exec(`DELETE FROM kg_relationship WHERE kb_id = ? AND doc_id = ?`, kbID, docID)
	s.DB.Exec(`DELETE FROM kg_claim WHERE kb_id = ? AND doc_id = ?`, kbID, docID)
}

// ---- 审计决策（REQ-101 学习视图，D-O15 消费/审计页）----

// InsertDecision 记录决策（id 空自动生成）。
func (s *Store) InsertDecision(d *OntoDecision) (*OntoDecision, error) {
	if strings.TrimSpace(d.Title) == "" {
		return nil, &HTTPError{Status: 400, Msg: "title is required"}
	}
	if d.ID == "" {
		d.ID = NewID()
	}
	if d.SubjectKind == "" {
		d.SubjectKind = "manual"
	}
	if d.MetaJSON == "" {
		d.MetaJSON = "{}"
	}
	_, err := s.DB.Exec(`INSERT INTO onto_decision(`+decisionCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		d.ID, d.SubjectKind, d.SubjectID, d.Title, d.Rationale, d.DerivedFrom, d.MetaJSON, now())
	if err != nil {
		return nil, err
	}
	return s.GetDecision(d.ID)
}

// GetDecision 单条。
func (s *Store) GetDecision(id string) (*OntoDecision, error) {
	d, err := scanDecision(s.DB.QueryRow(`SELECT `+decisionCols+` FROM onto_decision WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// ListDecisions 决策列表（subjectKind/subjectID 可选过滤；新→旧）。
func (s *Store) ListDecisions(subjectKind, subjectID string, limit int) ([]*OntoDecision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + decisionCols + ` FROM onto_decision`
	var args []any
	var conds []string
	if subjectKind != "" {
		conds = append(conds, `subject_kind = ?`)
		args = append(args, subjectKind)
	}
	if subjectID != "" {
		conds = append(conds, `subject_id = ?`)
		args = append(args, subjectID)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*OntoDecision{}
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DecisionChain 溯源链：沿 derived_from 向前回溯（环防御，最多 32 跳）。
func (s *Store) DecisionChain(id string) ([]*OntoDecision, error) {
	out := []*OntoDecision{}
	seen := map[string]bool{}
	cur := id
	for i := 0; i < 32 && cur != ""; i++ {
		if seen[cur] {
			break
		}
		seen[cur] = true
		d, err := s.GetDecision(cur)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, d)
		cur = d.DerivedFrom
	}
	return out, nil
}

// ---- M16 阶段二（REQ-129）：抽取治理与人工反馈 ----

// SetKGRelStatus 关系审核状态（approved|rejected；rejected 不参与检索）。
func (s *Store) SetKGRelStatus(kbID, id, status string) error {
	if status != "approved" && status != "rejected" {
		return &HTTPError{Status: 400, Msg: "status must be approved|rejected"}
	}
	res, err := s.DB.Exec(`UPDATE kg_relationship SET status=? WHERE kb_id=? AND id=?`, status, kbID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetKGClaimStatus claim 审核状态（approved|rejected）。
func (s *Store) SetKGClaimStatus(kbID, id, status string) error {
	if status != "approved" && status != "rejected" {
		return &HTTPError{Status: 400, Msg: "status must be approved|rejected"}
	}
	res, err := s.DB.Exec(`UPDATE kg_claim SET status=? WHERE kb_id=? AND id=?`, status, kbID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MergeKGEntities 实体消歧合并（REQ-129②，仅人工触发）：把 merge 实体的关系边与 claims
// 迁移到 keep 实体（关系按 source+type+target 去重），随后删除 merge 实体行。
func (s *Store) MergeKGEntities(kbID, keep string, merge []string) (movedRels, movedClaims int, err error) {
	if keep == "" || len(merge) == 0 {
		return 0, 0, &HTTPError{Status: 400, Msg: "keep 与 merge 均必填"}
	}
	for _, m := range merge {
		if m == keep {
			return 0, 0, &HTTPError{Status: 400, Msg: "merge 列表不能包含 keep 实体"}
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	ph := strings.TrimRight(strings.Repeat("?,", len(merge)), ",")
	base := append([]any{kbID}, toAny(merge)...)
	// 关系迁移：source/target 指向 merge 实体的改指 keep
	res, err := tx.Exec(`UPDATE kg_relationship SET source=? WHERE kb_id=? AND source IN (`+ph+`)`, append([]any{keep, kbID}, toAny(merge)...)...)
	if err != nil {
		return 0, 0, err
	}
	movedRels += mustCount(res)
	res, err = tx.Exec(`UPDATE kg_relationship SET target=? WHERE kb_id=? AND target IN (`+ph+`)`, append([]any{keep, kbID}, toAny(merge)...)...)
	if err != nil {
		return 0, 0, err
	}
	movedRels += mustCount(res)
	// 自环清理（merge 的两端都被改到 keep）
	if _, err = tx.Exec(`DELETE FROM kg_relationship WHERE kb_id=? AND source=target`, kbID); err != nil {
		return 0, 0, err
	}
	// claims 迁移（同 subject+text 去重由唯一业务语义粗判：仅迁移 subject 命中）
	res, err = tx.Exec(`UPDATE kg_claim SET subject=? WHERE kb_id=? AND subject IN (`+ph+`)`, append([]any{keep, kbID}, toAny(merge)...)...)
	if err != nil {
		return 0, 0, err
	}
	movedClaims += mustCount(res)
	// 删除 merge 实体
	if _, err = tx.Exec(`DELETE FROM kg_entity WHERE kb_id=? AND name IN (`+ph+`)`, base...); err != nil {
		return 0, 0, err
	}
	return movedRels, movedClaims, tx.Commit()
}

// mustCount 取 RowsAffected 的计数值（忽略 error——教学口径，删除/更新失败由上游 err 把守）。
func mustCount(res sql.Result) int {
	n, _ := res.RowsAffected()
	return int(n)
}

func toAny(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, v := range ss {
		out = append(out, v)
	}
	return out
}

// KGQuality 质量面板数据（REQ-129④）：method 分布（审计决策 meta_json）、孤儿实体数、
// 高频关系类型 TopN、rejected 计数（关系/claims）。
type KGQuality struct {
	MethodDist   map[string]int   `json:"method_dist"`
	OrphanEntity int              `json:"orphan_entity"`
	TopRelTypes  []RelTypeCount   `json:"top_rel_types"`
	RejectedRels int              `json:"rejected_rels"`
	RejectedClms int              `json:"rejected_claims"`
	Entities     int              `json:"entities"`
	Relationships int             `json:"relationships"`
}

type RelTypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

func (s *Store) KGQuality(kbID string) (*KGQuality, error) {
	q := &KGQuality{MethodDist: map[string]int{}}
	// method 分布：最近 kg 决策行 meta_json 的 method 字段
	rows, err := s.DB.Query(`SELECT meta_json FROM onto_decision WHERE subject_kind='kg' AND subject_id=? ORDER BY created_at DESC LIMIT 50`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var meta string
		if err := rows.Scan(&meta); err != nil {
			return nil, err
		}
		var m struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(meta), &m) == nil && m.Method != "" {
			q.MethodDist[m.Method]++
		}
	}
	rows.Err()
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_entity WHERE kb_id=?`, kbID).Scan(&q.Entities); err != nil {
		return nil, err
	}
	// 孤儿实体：不出现在任何关系边的实体
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_entity e WHERE kb_id=? AND NOT EXISTS (
		SELECT 1 FROM kg_relationship r WHERE r.kb_id=? AND (r.source=e.name OR r.target=e.name))`, kbID, kbID).Scan(&q.OrphanEntity); err != nil {
		return nil, err
	}
	trows, err := s.DB.Query(`SELECT rel_type, COUNT(*) n FROM kg_relationship WHERE kb_id=? GROUP BY rel_type ORDER BY n DESC LIMIT 5`, kbID)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var t string
		var n int
		if err := trows.Scan(&t, &n); err != nil {
			return nil, err
		}
		q.TopRelTypes = append(q.TopRelTypes, RelTypeCount{Type: t, Count: n})
	}
	trows.Err()
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_relationship WHERE kb_id=? AND status='rejected'`, kbID).Scan(&q.RejectedRels); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_claim WHERE kb_id=? AND status='rejected'`, kbID).Scan(&q.RejectedClms); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kg_relationship WHERE kb_id=?`, kbID).Scan(&q.Relationships); err != nil {
		return nil, err
	}
	return q, nil
}

// MergeSuggestion 系统合并建议（别名消歧粗规则：名称互相包含且类型相同 → 建议合并，仅提示不自动执行）。
type MergeSuggestion struct {
	Keep   string `json:"keep"`
	Merge  string `json:"merge"`
	Reason string `json:"reason"`
}

// KGMergeSuggestions 合并建议（教学口径：包含关系的同类型实体对，短名为 keep）。
func (s *Store) KGMergeSuggestions(kbID string) ([]MergeSuggestion, error) {
	ents, _, err := s.KGByKB(kbID)
	if err != nil {
		return nil, err
	}
	out := []MergeSuggestion{}
	for i, a := range ents {
		for _, b := range ents[i+1:] {
			if a.Type != b.Type || a.Name == b.Name {
				continue
			}
			long, short := a.Name, b.Name
			if len([]rune(short)) > len([]rune(long)) {
				long, short = short, long
			}
			if len([]rune(short)) >= 2 && strings.Contains(long, short) {
				out = append(out, MergeSuggestion{Keep: short, Merge: long,
					Reason: fmt.Sprintf("%q 是 %q 的子串且类型相同（%s）", short, long, a.Type)})
			}
			if len(out) >= 20 {
				return out, nil
			}
		}
	}
	return out, nil
}
