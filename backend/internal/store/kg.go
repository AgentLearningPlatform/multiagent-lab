package store

import (
	"database/sql"
	"errors"
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
const kgRelCols = `id,kb_id,doc_id,source,target,rel_type,created_at`
const kgClaimCols = `id,kb_id,doc_id,chunk_id,subject,text,created_at`
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
	if err := row.Scan(&r.ID, &r.KBID, &r.DocID, &r.Source, &r.Target, &r.Type, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func scanKGClaim(row interface{ Scan(...any) error }) (*KGClaim, error) {
	var c KGClaim
	if err := row.Scan(&c.ID, &c.KBID, &c.DocID, &c.ChunkID, &c.Subject, &c.Text, &c.CreatedAt); err != nil {
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
	ir, err := tx.Prepare(`INSERT INTO kg_relationship(` + kgRelCols + `) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ir.Close()
	for _, r := range rels {
		if _, err := ir.Exec(r.ID, r.KBID, r.DocID, r.Source, r.Target, r.Type, now()); err != nil {
			return err
		}
	}
	ic, err := tx.Prepare(`INSERT INTO kg_claim(` + kgClaimCols + `) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ic.Close()
	for _, c := range claims {
		if _, err := ic.Exec(c.ID, c.KBID, c.DocID, c.ChunkID, c.Subject, c.Text, now()); err != nil {
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
	q := `SELECT DISTINCT subject FROM kg_claim WHERE kb_id = ? AND chunk_id IN (` +
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
	q := `SELECT ` + kgRelCols + ` FROM kg_relationship WHERE kb_id = ? AND (source IN (` +
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
	q := `SELECT ` + kgClaimCols + ` FROM kg_claim WHERE kb_id = ? AND subject IN (` +
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
	q := `SELECT c.id,c.kb_id,c.doc_id,c.chunk_id,c.subject,c.text,c.created_at, COALESCE(ch.doc_id,''), COALESCE(ch.seq,0)
		FROM kg_claim c
		LEFT JOIN knowledge_chunk ch ON ch.id = c.chunk_id
		WHERE c.kb_id = ? AND c.subject IN (` +
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
		if err := rows.Scan(&t.ID, &t.KBID, &t.DocID, &t.ChunkID, &t.Subject, &t.Text, &t.CreatedAt, &doc, &t.ChunkSeq); err != nil {
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
