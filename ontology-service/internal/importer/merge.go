package importer

import (
	"encoding/json"
	"fmt"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// ---------------------------------------------------------------------------
// REQ-157/M-O15 导入合并底座（docs/23 启示 6「增量消歧而非一次性重建」+ OrionBelt 模式）：
// 外部本体入库前的字段级冲突审查与合并策略——
//   replace          冲突实体以导入版整体替换（导入版优先）
//   merge-overwrite  冲突实体字段级覆盖（导入版非空字段覆盖现行，保留现行独有字段）
//   merge            冲突实体保留现行，导入实体重命名并入（前缀自动协调，增量消歧）
// 复杂语义合并明确不做（26 号方案 §8-3：三选项 + 字段级预览，按误报率迭代）。
// 纯函数便于零依赖单测；预览与应用共用同一构建逻辑（审查向导所见即所得）。
// ---------------------------------------------------------------------------

const (
	StrategyReplace        = "replace"
	StrategyMergeOverwrite = "merge-overwrite"
	StrategyMerge          = "merge"
	DefaultConflictPrefix  = "ext"
)

// MergeConflict 单条实体冲突（字段级）。
type MergeConflict struct {
	Kind       string   `json:"kind"`                  // concept | relation | instance
	Name       string   `json:"name"`                  // 冲突键（实体名；IRI 由名派生，同名即同 IRI）
	Fields     []string `json:"fields"`                // 取值有差异的字段
	Incoming   string   `json:"incoming"`              // 导入版摘要（JSON）
	Current    string   `json:"current"`               // 现行版摘要（JSON）
	Resolution string   `json:"resolution"`            // 本策略下的处置：replaced | field-merged | renamed
	ResolvedAs string   `json:"resolved_as,omitempty"` // renamed 处置后的实体名
}

// MergePreview 合并预览（审查向导数据；Apply 复用同一构建）。
type MergePreview struct {
	Strategy   string          `json:"strategy"`
	Prefix     string          `json:"prefix"`
	Added      []string        `json:"added"` // 将新增的实体（kind:name）
	Conflicts  []MergeConflict `json:"conflicts"`
	Renamed    []string        `json:"renamed"` // merge 策略重命名清单（old -> new）
	Stats      MergeStats      `json:"stats"`
	TargetName string          `json:"target_name"`
	MergedSpec *pkgspec.Spec   `json:"merged_spec"` // 应用后的完整结果（diff 预览用）
}

// MergeStats 合并统计。
type MergeStats struct {
	ConceptsAdded    int `json:"concepts_added"`
	ConceptsUpdated  int `json:"concepts_updated"`
	RelationsAdded   int `json:"relations_added"`
	RelationsUpdated int `json:"relations_updated"`
	InstancesAdded   int `json:"instances_added"`
	InstancesUpdated int `json:"instances_updated"`
	InstancesRenamed int `json:"instances_renamed"`
	TotalConflicts   int `json:"total_conflicts"`
}

// BuildMerged 按 strategy 构建合并结果（target=现行，incoming=导入）。纯函数。
func BuildMerged(target, incoming *pkgspec.Spec, strategy, prefix string) (*MergePreview, error) {
	if target == nil || incoming == nil {
		return nil, fmt.Errorf("target 与 incoming spec 必填")
	}
	switch strategy {
	case StrategyReplace, StrategyMergeOverwrite, StrategyMerge:
	default:
		return nil, fmt.Errorf("strategy 须为 replace | merge-overwrite | merge")
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = DefaultConflictPrefix
	}
	pv := &MergePreview{
		Strategy:   strategy,
		Prefix:     prefix,
		Added:      []string{},
		Conflicts:  []MergeConflict{},
		Renamed:    []string{},
		TargetName: target.Name,
		MergedSpec: &pkgspec.Spec{
			ID:          target.ID,
			Name:        target.Name,
			Description: target.Description,
			Concepts:    append([]pkgspec.Concept{}, target.Concepts...),
			Relations:   append([]pkgspec.Relation{}, target.Relations...),
			Instances:   append([]pkgspec.Instance{}, target.Instances...),
		},
	}

	// ---- 概念 ----
	cIdx := map[string]int{}
	for i, c := range pv.MergedSpec.Concepts {
		cIdx[c.Name] = i
	}
	for _, in := range incoming.Concepts {
		if i, ok := cIdx[in.Name]; ok {
			cur := pv.MergedSpec.Concepts[i]
			conf := conceptConflict(cur, in)
			pv.Stats.TotalConflicts++
			switch strategy {
			case StrategyReplace:
				pv.MergedSpec.Concepts[i] = in
				conf.Resolution = "replaced"
				pv.Stats.ConceptsUpdated++
			case StrategyMergeOverwrite:
				pv.MergedSpec.Concepts[i] = conceptFieldMerge(cur, in)
				conf.Resolution = "field-merged"
				pv.Stats.ConceptsUpdated++
			default: // merge：保留现行，导入版重命名并入
				newName := uniquify(prefix+"_"+in.Name, cIdx)
				renamed := in
				renamed.Name = newName
				// 指向该概念的父子引用一并改写（保持层级语义）
				rewireConceptRefs(pv.MergedSpec, in.Name, newName)
				pv.MergedSpec.Concepts = append(pv.MergedSpec.Concepts, renamed)
				cIdx[newName] = len(pv.MergedSpec.Concepts) - 1
				conf.Resolution = "renamed"
				conf.ResolvedAs = newName
				pv.Renamed = append(pv.Renamed, fmt.Sprintf("concept %s -> %s", in.Name, newName))
				pv.Stats.ConceptsAdded++
			}
			pv.Conflicts = append(pv.Conflicts, conf)
		} else {
			pv.MergedSpec.Concepts = append(pv.MergedSpec.Concepts, in)
			cIdx[in.Name] = len(pv.MergedSpec.Concepts) - 1
			pv.Added = append(pv.Added, "concept:"+in.Name)
			pv.Stats.ConceptsAdded++
		}
	}

	// ---- 关系 ----
	rIdx := map[string]int{}
	for i, r := range pv.MergedSpec.Relations {
		rIdx[r.Name] = i
	}
	for _, in := range incoming.Relations {
		if i, ok := rIdx[in.Name]; ok {
			cur := pv.MergedSpec.Relations[i]
			conf := relationConflict(cur, in)
			pv.Stats.TotalConflicts++
			switch strategy {
			case StrategyReplace:
				pv.MergedSpec.Relations[i] = in
				conf.Resolution = "replaced"
				pv.Stats.RelationsUpdated++
			case StrategyMergeOverwrite:
				pv.MergedSpec.Relations[i] = relationFieldMerge(cur, in)
				conf.Resolution = "field-merged"
				pv.Stats.RelationsUpdated++
			default:
				newName := uniquify(prefix+"_"+in.Name, rIdx)
				renamed := in
				renamed.Name = newName
				// 实例断言引用该关系名的改写
				rewireRelationRefs(pv.MergedSpec, in.Name, newName)
				pv.MergedSpec.Relations = append(pv.MergedSpec.Relations, renamed)
				rIdx[newName] = len(pv.MergedSpec.Relations) - 1
				conf.Resolution = "renamed"
				conf.ResolvedAs = newName
				pv.Renamed = append(pv.Renamed, fmt.Sprintf("relation %s -> %s", in.Name, newName))
				pv.Stats.RelationsAdded++
			}
			pv.Conflicts = append(pv.Conflicts, conf)
		} else {
			pv.MergedSpec.Relations = append(pv.MergedSpec.Relations, in)
			rIdx[in.Name] = len(pv.MergedSpec.Relations) - 1
			pv.Added = append(pv.Added, "relation:"+in.Name)
			pv.Stats.RelationsAdded++
		}
	}

	// ---- 实例 ----
	iIdx := map[string]int{}
	for i, in := range pv.MergedSpec.Instances {
		iIdx[in.Name] = i
	}
	for _, in := range incoming.Instances {
		if i, ok := iIdx[in.Name]; ok {
			cur := pv.MergedSpec.Instances[i]
			conf := instanceConflict(cur, in)
			pv.Stats.TotalConflicts++
			switch strategy {
			case StrategyReplace:
				pv.MergedSpec.Instances[i] = in
				conf.Resolution = "replaced"
				pv.Stats.InstancesUpdated++
			case StrategyMergeOverwrite:
				pv.MergedSpec.Instances[i] = instanceFieldMerge(cur, in)
				conf.Resolution = "field-merged"
				pv.Stats.InstancesUpdated++
			default:
				newName := uniquify(prefix+"_"+in.Name, iIdx)
				renamed := in
				renamed.Name = newName
				// 他者实例断言指向该实例的 target 改写
				rewireInstanceRefs(pv.MergedSpec, in.Name, newName)
				pv.MergedSpec.Instances = append(pv.MergedSpec.Instances, renamed)
				iIdx[newName] = len(pv.MergedSpec.Instances) - 1
				conf.Resolution = "renamed"
				conf.ResolvedAs = newName
				pv.Renamed = append(pv.Renamed, fmt.Sprintf("instance %s -> %s", in.Name, newName))
				pv.Stats.InstancesRenamed++
			}
			pv.Conflicts = append(pv.Conflicts, conf)
		} else {
			pv.MergedSpec.Instances = append(pv.MergedSpec.Instances, in)
			iIdx[in.Name] = len(pv.MergedSpec.Instances) - 1
			pv.Added = append(pv.Added, "instance:"+in.Name)
			pv.Stats.InstancesAdded++
		}
	}
	return pv, nil
}

// uniquify 前缀协调：重命名后仍冲突则追加 _2/_3…
func uniquify(base string, taken map[string]int) string {
	name := base
	for n := 2; ; n++ {
		if _, ok := taken[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s_%d", base, n)
	}
}

// mergeConflictOf 构建冲突条目（导入/现行实体 JSON 摘要，供审查向导字段级对照）。
func mergeConflictOf(kind, name string, fields []string, incoming, current any) MergeConflict {
	inJSON, _ := json.Marshal(incoming)
	curJSON, _ := json.Marshal(current)
	if fields == nil {
		fields = []string{}
	}
	return MergeConflict{
		Kind:     kind,
		Name:     name,
		Fields:   fields,
		Incoming: string(inJSON),
		Current:  string(curJSON),
	}
}

// ---- 冲突字段提取 ----

func conceptConflict(cur, in pkgspec.Concept) MergeConflict {
	fields := []string{}
	if cur.Definition != in.Definition {
		fields = append(fields, "definition")
	}
	if cur.Label != in.Label {
		fields = append(fields, "label")
	}
	if strings.Join(cur.Parents, "|") != strings.Join(in.Parents, "|") {
		fields = append(fields, "parents")
	}
	return mergeConflictOf("concept", in.Name, fields, in, cur)
}

func conceptFieldMerge(cur, in pkgspec.Concept) pkgspec.Concept {
	out := cur
	if strings.TrimSpace(in.Definition) != "" {
		out.Definition = in.Definition
	}
	if strings.TrimSpace(in.Label) != "" {
		out.Label = in.Label
	}
	if len(in.Parents) > 0 {
		out.Parents = in.Parents
	}
	return out
}

func relationConflict(cur, in pkgspec.Relation) MergeConflict {
	fields := []string{}
	if cur.Definition != in.Definition {
		fields = append(fields, "definition")
	}
	if cur.Label != in.Label {
		fields = append(fields, "label")
	}
	if cur.From != in.From {
		fields = append(fields, "from")
	}
	if cur.To != in.To {
		fields = append(fields, "to")
	}
	return mergeConflictOf("relation", in.Name, fields, in, cur)
}

func relationFieldMerge(cur, in pkgspec.Relation) pkgspec.Relation {
	out := cur
	if strings.TrimSpace(in.Definition) != "" {
		out.Definition = in.Definition
	}
	if strings.TrimSpace(in.Label) != "" {
		out.Label = in.Label
	}
	if strings.TrimSpace(in.From) != "" {
		out.From = in.From
	}
	if strings.TrimSpace(in.To) != "" {
		out.To = in.To
	}
	return out
}

func instanceConflict(cur, in pkgspec.Instance) MergeConflict {
	fields := []string{}
	if cur.Concept != in.Concept {
		fields = append(fields, "concept")
	}
	if len(cur.Attributes) != len(in.Attributes) || !equalAttributes(cur.Attributes, in.Attributes) {
		fields = append(fields, "attributes")
	}
	if len(cur.Relations) != len(in.Relations) || !equalInstanceRels(cur.Relations, in.Relations) {
		fields = append(fields, "relations")
	}
	return mergeConflictOf("instance", in.Name, fields, in, cur)
}

func instanceFieldMerge(cur, in pkgspec.Instance) pkgspec.Instance {
	out := cur
	if strings.TrimSpace(in.Concept) != "" {
		out.Concept = in.Concept
	}
	if len(in.Attributes) > 0 {
		merged := map[string]any{}
		for k, v := range cur.Attributes {
			merged[k] = v
		}
		for k, v := range in.Attributes {
			merged[k] = v
		}
		out.Attributes = merged
	}
	if len(in.Relations) > 0 {
		out.Relations = in.Relations
	}
	return out
}

func equalAttributes(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || fmt.Sprint(bv) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

func equalInstanceRels(a, b []pkgspec.InstanceRel) bool {
	if len(a) != len(b) {
		return false
	}
	join := func(rs []pkgspec.InstanceRel) string {
		parts := make([]string, 0, len(rs))
		for _, r := range rs {
			parts = append(parts, r.Rel+"->"+r.Target)
		}
		return strings.Join(parts, "|")
	}
	return join(a) == join(b)
}

// ---- 引用改写（merge 重命名并入时保持语义） ----

func rewireConceptRefs(sp *pkgspec.Spec, oldName, newName string) {
	for i := range sp.Concepts {
		for j, pr := range sp.Concepts[i].Parents {
			if pr == oldName {
				sp.Concepts[i].Parents[j] = newName
			}
		}
	}
	for i := range sp.Relations {
		if sp.Relations[i].From == oldName {
			sp.Relations[i].From = newName
		}
		if sp.Relations[i].To == oldName {
			sp.Relations[i].To = newName
		}
	}
	for i := range sp.Instances {
		if sp.Instances[i].Concept == oldName {
			sp.Instances[i].Concept = newName
		}
	}
}

func rewireRelationRefs(sp *pkgspec.Spec, oldName, newName string) {
	for i := range sp.Instances {
		for j := range sp.Instances[i].Relations {
			if sp.Instances[i].Relations[j].Rel == oldName {
				sp.Instances[i].Relations[j].Rel = newName
			}
		}
	}
}

func rewireInstanceRefs(sp *pkgspec.Spec, oldName, newName string) {
	for i := range sp.Instances {
		for j := range sp.Instances[i].Relations {
			if sp.Instances[i].Relations[j].Target == oldName {
				sp.Instances[i].Relations[j].Target = newName
			}
		}
	}
}
