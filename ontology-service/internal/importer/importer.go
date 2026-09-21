// Package importer 导入流（REQ-80/69）：格式嗅探 → 原样存档 → 解析为 spec_json → 导入报告。
// spec 无损；csv/graphml 有损（规则透明写入报告）；OWL/TTL 解析经 Python rdflib sidecar（不自研）。
package importer

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// Format 支持的形态格式。
const (
	FormatSpecJSON = "spec_json"
	FormatCSV      = "csv"
	FormatGraphML  = "graphml"
	FormatOWLRDF   = "owl_rdfxml"
	FormatTurtle   = "turtle"
)

// Report 导入报告（统计 + warnings + 有损映射说明）。
type Report struct {
	Format    string   `json:"format"`
	Lossy     bool     `json:"lossy"`
	Warnings  []string `json:"warnings"`
	LossyNote string   `json:"lossy_note,omitempty"`
}

type Sidecar struct {
	Python string // python3 可执行路径
	Script string // sidecar.py 绝对路径
}

// Sniff 格式嗅探：扩展名优先，内容特征兜底。
func Sniff(filename, content string) (string, error) {
	lf := strings.ToLower(filename)
	trimmed := strings.TrimSpace(content)
	switch {
	case strings.HasSuffix(lf, ".ttl"):
		return FormatTurtle, nil
	case strings.HasSuffix(lf, ".owl"), strings.HasSuffix(lf, ".rdf"), strings.HasSuffix(lf, ".rdfxml"), strings.HasSuffix(lf, ".xml"):
		isXML := strings.HasPrefix(trimmed, "<?xml") || strings.HasPrefix(trimmed, "<rdf:RDF")
		if !isXML && (strings.HasPrefix(trimmed, "@prefix") || strings.Contains(trimmed, "\n@prefix")) {
			return FormatTurtle, nil // .owl 后缀常装 Turtle 内容（实测 HOCC 即如此）
		}
		if strings.Contains(trimmed, "<graphml") {
			return FormatGraphML, nil
		}
		return FormatOWLRDF, nil
	case strings.HasSuffix(lf, ".graphml"):
		return FormatGraphML, nil
	case strings.HasSuffix(lf, ".csv"):
		return FormatCSV, nil
	case strings.HasSuffix(lf, ".json"):
		if looksLikeSpec(trimmed) {
			return FormatSpecJSON, nil
		}
		return "", fmt.Errorf("json 内容不是 spec_json（缺少 concepts/relations/instances 三要素）")
	default:
		// 内容特征兜底
		if strings.HasPrefix(trimmed, "@prefix") || strings.Contains(trimmed, "@prefix") {
			return FormatTurtle, nil
		}
		if strings.Contains(trimmed, "<rdf:RDF") {
			return FormatOWLRDF, nil
		}
		if strings.Contains(trimmed, "<graphml") {
			return FormatGraphML, nil
		}
		if looksLikeSpec(trimmed) {
			return FormatSpecJSON, nil
		}
		return "", fmt.Errorf("无法识别格式（支持 spec_json / csv / graphml / owl-rdfxml / turtle）")
	}
}

func looksLikeSpec(s string) bool {
	var probe struct {
		Concepts  *[]json.RawMessage `json:"concepts"`
		Relations *[]json.RawMessage `json:"relations"`
		Instances *[]json.RawMessage `json:"instances"`
	}
	return json.Unmarshal([]byte(s), &probe) == nil &&
		probe.Concepts != nil && probe.Relations != nil && probe.Instances != nil
}

// Import 解析为 spec_json + 报告。original 由调用方原样存档。
func Import(sidecar *Sidecar, filename, content string) (*pkgspec.Spec, *Report, error) {
	format, err := Sniff(filename, content)
	if err != nil {
		return nil, nil, err
	}
	switch format {
	case FormatSpecJSON:
		var sp pkgspec.Spec
		if err := json.Unmarshal([]byte(content), &sp); err != nil {
			return nil, nil, fmt.Errorf("spec_json 解析失败: %w", err)
		}
		rep := &Report{Format: format}
		if errs := sp.Validate(); len(errs) > 0 {
			for _, e := range errs {
				rep.Warnings = append(rep.Warnings, "校验问题: "+e.Error())
			}
		}
		return &sp, rep, nil
	case FormatCSV:
		sp, rep := parseCSV(content)
		return sp, rep, nil
	case FormatGraphML:
		sp, rep := parseGraphML(content)
		return sp, rep, nil
	case FormatOWLRDF, FormatTurtle:
		return parseViaSidecar(sidecar, format, content)
	}
	return nil, nil, fmt.Errorf("unsupported format %q", format)
}

// ---- csv：单文件列驱动（kind 列区分 concept/relation/instance/instance_rel）----

func parseCSV(content string) (*pkgspec.Spec, *Report) {
	rep := &Report{Format: FormatCSV, Lossy: true, LossyNote: "csv 为扁平表格，仅映射名称/标签/定义与引用，其他列丢弃计入 warnings"}
	rows := splitCSV(content)
	if len(rows) < 1 {
		return nil, rep
	}
	header := rows[0]
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, key string) string {
		i, ok := col[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	sp := &pkgspec.Spec{}
	cn := map[string]bool{}
	rn := map[string]bool{}
	in := map[string]bool{}
	for _, row := range rows[1:] {
		kind := strings.ToLower(get(row, "kind"))
		name := get(row, "name")
		switch kind {
		case "concept":
			if name == "" || cn[name] {
				rep.warn("跳过无效/重复概念行: " + name)
				continue
			}
			c := pkgspec.Concept{Name: name, Label: get(row, "label"), Definition: get(row, "definition")}
			if p := get(row, "parents"); p != "" {
				for _, x := range strings.Split(p, ";") {
					if x = strings.TrimSpace(x); x != "" {
						c.Parents = append(c.Parents, x)
					}
				}
			}
			sp.Concepts = append(sp.Concepts, c)
			cn[name] = true
		case "relation":
			if name == "" || rn[name] {
				rep.warn("跳过无效/重复关系行: " + name)
				continue
			}
			sp.Relations = append(sp.Relations, pkgspec.Relation{Name: name, Label: get(row, "label"), Definition: get(row, "definition"), From: get(row, "from"), To: get(row, "to")})
			rn[name] = true
		case "instance":
			if name == "" || in[name] {
				rep.warn("跳过无效/重复实例行: " + name)
				continue
			}
			it := pkgspec.Instance{Name: name, Concept: get(row, "concept"), Attributes: map[string]any{}}
			for k, i := range col {
				if k == "kind" || k == "name" || k == "concept" || strings.HasPrefix(k, "rel_") {
					continue
				}
				if i < len(row) && strings.TrimSpace(row[i]) != "" {
					it.Attributes[k] = strings.TrimSpace(row[i])
				}
			}
			if len(it.Attributes) == 0 {
				it.Attributes = nil
			}
			sp.Instances = append(sp.Instances, it)
			in[name] = true
		case "instance_rel":
			src, rel, tgt := get(row, "source"), get(row, "rel"), get(row, "target")
			idx := indexOfInstance(sp, src)
			if idx < 0 || rel == "" || tgt == "" {
				rep.warn(fmt.Sprintf("跳过无效实例关系行: %s -[%s]-> %s", src, rel, tgt))
				continue
			}
			sp.Instances[idx].Relations = append(sp.Instances[idx].Relations, pkgspec.InstanceRel{Rel: rel, Target: tgt})
		default:
			rep.warn("未知 kind 行已跳过: " + kind)
		}
	}
	return sp, rep
}

func indexOfInstance(sp *pkgspec.Spec, name string) int {
	for i := range sp.Instances {
		if sp.Instances[i].Name == name {
			return i
		}
	}
	return -1
}

// splitCSV 最小 CSV 解析（RFC4180 引号支持，无多行字段）。
func splitCSV(content string) [][]string {
	var rows [][]string
	var cur []string
	var field strings.Builder
	inQuote := false
	flush := func() {
		cur = append(cur, field.String())
		field.Reset()
	}
	endRow := func() {
		flush()
		rows = append(rows, cur)
		cur = nil
	}
	for _, r := range content {
		switch {
		case inQuote:
			if r == '"' {
				inQuote = false
			} else {
				field.WriteRune(r)
			}
		case r == '"':
			inQuote = true
		case r == ',':
			flush()
		case r == '\n' || r == '\r':
			if r == '\r' {
				continue
			}
			endRow()
		default:
			field.WriteRune(r)
		}
	}
	if field.Len() > 0 || len(cur) > 0 {
		endRow()
	}
	// 去掉全空行
	out := rows[:0]
	for _, row := range rows {
		empty := true
		for _, f := range row {
			if strings.TrimSpace(f) != "" {
				empty = false
				break
			}
		}
		if !empty {
			out = append(out, row)
		}
	}
	return out
}

// ---- graphml：node→实例（data key: concept），edge→实例关系（data key: rel/label）----

type graphmlDoc struct {
	Keys []struct {
		ID   string `xml:"id,attr"`
		For  string `xml:"for,attr"`
		Name string `xml:"attr.name,attr"`
	} `xml:"key"`
	Graph struct {
		Nodes []struct {
			ID    string `xml:"id,attr"`
			Datas []struct {
				Key string `xml:"key,attr"`
				Val string `xml:",chardata"`
			} `xml:"data"`
		} `xml:"node"`
		Edges []struct {
			Source string `xml:"source,attr"`
			Target string `xml:"target,attr"`
			Datas  []struct {
				Key string `xml:"key,attr"`
				Val string `xml:",chardata"`
			} `xml:"data"`
		} `xml:"edge"`
	} `xml:"graph"`
}

func parseGraphML(content string) (*pkgspec.Spec, *Report) {
	rep := &Report{Format: FormatGraphML, Lossy: true, LossyNote: "graphml 无本体语义：node→实例、edge→实例关系；未标注概念/关系的元素跳过并计入 warnings；未出现的概念/关系定义不生成"}
	var doc graphmlDoc
	if err := xml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, rep
	}
	// key 名映射
	nodeKeys, edgeKeys := map[string]string{}, map[string]string{}
	for _, k := range doc.Keys {
		if k.For == "node" || (k.For == "" && k.Name != "") {
			nodeKeys[k.ID] = k.Name
		}
		if k.For == "edge" {
			edgeKeys[k.ID] = k.Name
		}
	}
	sp := &pkgspec.Spec{}
	rn := map[string]bool{}
	in := map[string]bool{}
	for _, n := range doc.Graph.Nodes {
		name := strings.TrimSpace(n.ID)
		if name == "" || in[name] {
			rep.warn("跳过无效/重复节点: " + name)
			continue
		}
		it := pkgspec.Instance{Name: name, Attributes: map[string]any{}}
		for _, d := range n.Datas {
			key := nodeKeys[d.Key]
			switch strings.ToLower(key) {
			case "concept", "type":
				it.Concept = strings.TrimSpace(d.Val)
			case "label", "name":
				it.Name = strings.TrimSpace(d.Val)
				name = it.Name
			default:
				if v := strings.TrimSpace(d.Val); v != "" {
					it.Attributes[key] = v
				}
			}
		}
		if it.Name == "" {
			it.Name = name
		}
		sp.Instances = append(sp.Instances, it)
		in[it.Name] = true
	}
	for _, e := range doc.Graph.Edges {
		var rel string
		for _, d := range e.Datas {
			if k := strings.ToLower(edgeKeys[d.Key]); k == "rel" || k == "relation" || k == "label" {
				if rel == "" {
					rel = strings.TrimSpace(d.Val)
				}
			}
		}
		if rel == "" {
			rep.warn(fmt.Sprintf("边 %s->%s 无关系标注，跳过", e.Source, e.Target))
			continue
		}
		if !rn[rel] {
			rep.warn("关系未定义: " + rel + "（保留实例关系，校验时会报引用缺失）")
			rn[rel] = true
		}
		idx := indexOfInstance(sp, e.Source)
		if idx < 0 {
			rep.warn("边源节点不存在: " + e.Source)
			continue
		}
		sp.Instances[idx].Relations = append(sp.Instances[idx].Relations, pkgspec.InstanceRel{Rel: rel, Target: e.Target})
	}
	return sp, rep
}

// ---- OWL/TTL：经 sidecar（Python rdflib，不自研）----

type sidecarParseOut struct {
	Spec     pkgspec.Spec `json:"spec"`
	Warnings []string     `json:"warnings"`
	Lossy    bool         `json:"lossy"`
	Note     string       `json:"note"`
}

func parseViaSidecar(sc *Sidecar, format, content string) (*pkgspec.Spec, *Report, error) {
	if sc == nil || sc.Python == "" || sc.Script == "" {
		return nil, nil, fmt.Errorf("sidecar 未配置（SIDECAR_PYTHON / SIDECAR_SCRIPT）")
	}
	cmd := exec.Command(sc.Python, sc.Script, "parse", "--format", format)
	cmd.Stdin = strings.NewReader(content)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("sidecar parse: %w; stderr: %s", err, strings.TrimSpace(errb.String()))
	}
	var res sidecarParseOut
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		return nil, nil, fmt.Errorf("sidecar 输出解析失败: %w; stdout: %.200s", err, out.String())
	}
	rep := &Report{Format: format, Lossy: res.Lossy, Warnings: res.Warnings, LossyNote: res.Note}
	return &res.Spec, rep, nil
}

// ExportTTL spec_json → Turtle（sidecar export，导出即校验）。
func ExportTTL(sc *Sidecar, ontologyID string, sp *pkgspec.Spec) (string, error) {
	if sc == nil || sc.Python == "" || sc.Script == "" {
		return "", fmt.Errorf("sidecar 未配置（SIDECAR_PYTHON / SIDECAR_SCRIPT）")
	}
	bts, err := json.Marshal(sp)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(sc.Python, sc.Script, "export", "--ontology-id", ontologyID)
	cmd.Stdin = bytes.NewReader(bts)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sidecar export: %w; stderr: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (r *Report) warn(s string) { r.Warnings = append(r.Warnings, s) }
