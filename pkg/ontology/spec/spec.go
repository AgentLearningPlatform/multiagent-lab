// Package spec 定义本体归一化形态 spec_json 的类型与校验规则。
// 三类要素：concepts / relations / instances（REQ-61）。
// 校验 = JSON 结构校验 + 引用完整性（REQ-63），构建平面服务侧执行；
// 规则与人工编辑、LLM 辅助创建共用（REQ-63/82）。
package spec

import (
	"fmt"
	"strings"
	"unicode"
)

// Concept 概念（类）。
type Concept struct {
	Name       string   `json:"name"`
	Label      string   `json:"label,omitempty"`
	Definition string   `json:"definition,omitempty"`
	Parents    []string `json:"parents,omitempty"` // 父概念名（多继承）
}

// Relation 对象属性/关系（概念层，From/To 为概念名）。
type Relation struct {
	Name       string `json:"name"`
	Label      string `json:"label,omitempty"`
	Definition string `json:"definition,omitempty"`
	From       string `json:"from"` // 定义域概念名
	To         string `json:"to"`   // 值域概念名
}

// InstanceRel 实例关系断言。
type InstanceRel struct {
	Rel    string `json:"rel"`    // 关系名
	Target string `json:"target"` // 目标实例名
}

// Instance 实例。
type Instance struct {
	Name       string         `json:"name"`
	Concept    string         `json:"concept"` // 所属概念名
	Attributes map[string]any `json:"attributes,omitempty"`
	Relations  []InstanceRel  `json:"relations,omitempty"`
}

// Spec 本体归一化形态（spec_json）。
type Spec struct {
	ID          string     `json:"id,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Concepts    []Concept  `json:"concepts"`
	Relations   []Relation `json:"relations"`
	Instances   []Instance `json:"instances"`
}

// ValidationError 单条校验错误（结构化，供 LLM 修正循环回喂）。
type ValidationError struct {
	Path    string `json:"path"` // 如 concepts[2].parents[0]
	Message string `json:"message"`
}

func (e ValidationError) Error() string { return e.Path + ": " + e.Message }

// Validate 校验结构完整性与引用完整性，返回全部错误（不短 路）。
func (s *Spec) Validate() []ValidationError {
	var errs []ValidationError
	add := func(path, msg string) { errs = append(errs, ValidationError{Path: path, Message: msg}) }

	// ---- concepts：name 必填唯一；parents 引用已定义概念 ----
	cn := map[string]int{}
	for i, c := range s.Concepts {
		p := fmt.Sprintf("concepts[%d]", i)
		if strings.TrimSpace(c.Name) == "" {
			add(p+".name", "概念名不能为空")
			continue
		}
		if _, dup := cn[c.Name]; dup {
			add(p+".name", "概念名重复: "+c.Name)
			continue
		}
		cn[c.Name] = i
	}
	for i, c := range s.Concepts {
		if _, ok := cn[c.Name]; !ok {
			continue // 名字本身有问题，跳过引用检查
		}
		for j, p := range c.Parents {
			if _, ok := cn[p]; !ok {
				add(fmt.Sprintf("concepts[%d].parents[%d]", i, j), "引用了未定义概念: "+p)
			}
		}
	}

	// ---- relations：name 必填唯一；from/to 必须是已定义概念 ----
	rn := map[string]int{}
	for i, r := range s.Relations {
		p := fmt.Sprintf("relations[%d]", i)
		if strings.TrimSpace(r.Name) == "" {
			add(p+".name", "关系名不能为空")
			continue
		}
		if _, dup := rn[r.Name]; dup {
			add(p+".name", "关系名重复: "+r.Name)
			continue
		}
		rn[r.Name] = i
		if _, ok := cn[r.From]; !ok {
			add(p+".from", "定义域引用了未定义概念: "+r.From)
		}
		if _, ok := cn[r.To]; !ok {
			add(p+".to", "值域引用了未定义概念: "+r.To)
		}
	}

	// ---- instances：name 必填唯一；concept/relations 引用完整性 ----
	in := map[string]int{}
	for i, it := range s.Instances {
		p := fmt.Sprintf("instances[%d]", i)
		if strings.TrimSpace(it.Name) == "" {
			add(p+".name", "实例名不能为空")
			continue
		}
		if _, dup := in[it.Name]; dup {
			add(p+".name", "实例名重复: "+it.Name)
			continue
		}
		in[it.Name] = i
		if _, ok := cn[it.Concept]; !ok {
			add(p+".concept", "引用了未定义概念: "+it.Concept)
		}
	}
	for i, it := range s.Instances {
		if _, ok := in[it.Name]; !ok {
			continue
		}
		for j, ir := range it.Relations {
			if _, ok := rn[ir.Rel]; !ok {
				add(fmt.Sprintf("instances[%d].relations[%d].rel", i, j), "引用了未定义关系: "+ir.Rel)
			}
			if _, ok := in[ir.Target]; !ok {
				add(fmt.Sprintf("instances[%d].relations[%d].target", i, j), "引用了未定义实例: "+ir.Target)
			}
		}
	}
	return errs
}

// Stats 返回概念/关系/实例数量（列表页统计，REQ-60）。
func (s *Spec) Stats() (concepts, relations, instances int) {
	return len(s.Concepts), len(s.Relations), len(s.Instances)
}

// ConceptURI / RelationURI / InstanceURI / AttrURI 生成确定性 URN。
// 导出（spec_json→TTL）与查询翻译（onto_*→SPARQL）共用同一规则，
// 保证自建本体在 RDF 形态下可双向定位。
func URIPrefix(ontologyID string) string {
	return "urn:o:" + Sanitize(ontologyID) + ":"
}

func ConceptURI(ontologyID, name string) string {
	return URIPrefix(ontologyID) + "concept:" + Sanitize(name)
}

func RelationURI(ontologyID, name string) string {
	return URIPrefix(ontologyID) + "relation:" + Sanitize(name)
}

func InstanceURI(ontologyID, name string) string {
	return URIPrefix(ontologyID) + "instance:" + Sanitize(name)
}

func AttrURI(ontologyID, key string) string {
	return URIPrefix(ontologyID) + "attr:" + Sanitize(key)
}

func Sanitize(s string) string {
	// 确定性规则：保留 Unicode 字母/数字与 _-. ，其余每字符替换为 '_'。
	// 与 tools/rdf-sidecar/sidecar.py 的 sanitize 保持逐字符一致（导出与查询翻译共用）。
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
