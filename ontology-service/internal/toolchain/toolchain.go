// Package toolchain REQ-155 AI-native 本体工具链核心（M-O15 阶段一，docs/23 §6.1）。
// 设计取向：LLM 生成 → 工具链验证 → 迭代修复闭环——工具输出结构化、机器可读，
// 供 LLM/Agent 直接消费（结构化工具访问显著优于 LLM 直读原始 OWL，F1 0.717 vs 0.323）。
// 复用边界：结构校验复用 pkg/ontology/spec Spec.Validate；质量检查复用 internal/qualitygate
// 引擎（REQ-171 交付，不重复建设）；SPARQL 只读白名单与 runtime-manager facade sparql.go
// 同口径（facade 侧为权威语义源，本包独立实现以避免跨 go module 依赖）。
package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/qualitygate"
)

// Validation validate 工具结果：结构错误 + 质量报告（复用 qualitygate 三维评分）。
type Validation struct {
	Pass      bool                 `json:"pass"`
	Strict    bool                 `json:"strict"`
	Errors    []string             `json:"errors"` // 结构错误（path: message），存在即 pass=false
	Quality   *qualitygate.Report  `json:"quality"`
}

// LintReport lint 工具结果：qualitygate 命中以 lint 语义呈现（风格/最佳实践，不阻断结构）。
type LintReport struct {
	Pass     bool                  `json:"pass"` // 无错误级命中即 pass（lint 不因风格项失败）
	Findings []qualitygate.Finding `json:"findings"`
	Count    int                   `json:"count"`
}

// Validate 结构校验 + 质量检查。strict=true 时质量错误级命中也判不通过
// （与 REQ-171 修复循环的门禁口径一致）。
func Validate(sp *pkgspec.Spec, strict bool) *Validation {
	v := &Validation{Pass: true, Strict: strict, Errors: []string{}}
	for _, e := range sp.Validate() {
		v.Errors = append(v.Errors, e.Error())
	}
	rep := qualitygate.Check(sp, qualitygate.DefaultConfig())
	v.Quality = rep
	if len(v.Errors) > 0 {
		v.Pass = false
	}
	if strict && rep != nil && rep.ErrorCount > 0 {
		v.Pass = false
	}
	return v
}

// Lint 质量门禁全量命中以 lint 语义输出（含 warning/info；错误级决定 pass）。
func Lint(sp *pkgspec.Spec) *LintReport {
	rep := qualitygate.Check(sp, qualitygate.DefaultConfig())
	l := &LintReport{Pass: true, Findings: []qualitygate.Finding{}}
	if rep != nil {
		l.Findings = rep.Findings
		l.Pass = rep.ErrorCount == 0
	}
	l.Count = len(l.Findings)
	return l
}

// ---- query 工具：SELECT-only 词法门禁 + SPARQL results JSON → 表格 ----

// 变更关键字（与 facade sparql.go 同清单）；字面量/IRI 内同形词不误伤；
// 「?delete 变量按裸词 delete 命中」与 facade 一致，为受控查询面的已知取舍（教学点）。
var changeKeywords = map[string]bool{
	"UPDATE": true, "INSERT": true, "DELETE": true, "LOAD": true, "CLEAR": true,
	"CREATE": true, "DROP": true, "MOVE": true, "COPY": true, "ADD": true,
}

func isWordChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == ':' || c == '.'
}

// scanBareWords 词法扫描产出裸词：跳过 # 注释、<…> IRI、单双引号字符串（含 '''/""" 长串与 \ 转义）。
func scanBareWords(q string) []string {
	var words []string
	i, n := 0, len(q)
	for i < n {
		c := q[i]
		switch {
		case c == '#':
			for i < n && q[i] != '\n' {
				i++
			}
		case c == '<':
			for i < n && q[i] != '>' {
				i++
			}
			i++
		case c == '\'' || c == '"':
			if i+2 < n && q[i+1] == c && q[i+2] == c {
				i += 3
				for i < n {
					if q[i] == c && i+2 < n && q[i+1] == c && q[i+2] == c {
						break
					}
					i++
				}
				i += 3
			} else {
				i++
				for i < n && q[i] != c {
					if q[i] == '\\' {
						i++
					}
					i++
				}
				i++
			}
		case isWordChar(c):
			j := i
			for j < n && isWordChar(q[j]) {
				j++
			}
			words = append(words, strings.TrimSuffix(q[i:j], "."))
			i = j
		default:
			i++
		}
	}
	return words
}

// VetReadonlySelect 只读 SELECT 门禁：PREFIX/BASE 前导声明（任意次序）之后首关键词须为
// SELECT；全文裸词命中变更关键字即拒绝。带冒号的词（前缀名 ex:xxx）不作关键字判定。
func VetReadonlySelect(q string) error {
	words := scanBareWords(q)
	first := ""
	for k := 0; k < len(words); k++ {
		w := words[k]
		up := strings.ToUpper(w)
		if up == "PREFIX" {
			k++ // 跳过前缀名参数（ex: / ex:a 形态）
			continue
		}
		if up == "BASE" {
			continue // 其 IRI 参数在扫描期已被 <…> 吞掉
		}
		if strings.Contains(w, ":") {
			continue // 前缀名不作结构关键字
		}
		first = up
		break
	}
	if first == "" {
		return errors.New("查询为空或缺少关键词")
	}
	if first != "SELECT" {
		return fmt.Errorf("只读面仅允许 SELECT，收到 %q", first)
	}
	for _, w := range words {
		if strings.Contains(w, ":") {
			continue
		}
		if changeKeywords[strings.ToUpper(w)] {
			return fmt.Errorf("变更关键字 %q 被拒绝（只读查询面）", strings.ToUpper(w))
		}
	}
	return nil
}

// Table 查询结果表格（facade toTable 同口径：列名字母序稳定、缺键补空、total+truncated 诚实标注）。
type Table struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	Total     int        `json:"total"`
	Truncated bool       `json:"truncated"`
}

// ClampLimit 行数上限钳制：缺省 50、上限 200（facade sparql_query 同参语义）。
func ClampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

type sparqlResults struct {
	Head struct {
		Vars []string `json:"vars"`
	} `json:"head"`
	Results struct {
		Bindings []map[string]map[string]any `json:"bindings"`
	} `json:"results"`
}

// Query 对 endpoint 执行只读 SELECT：门禁 → POST（SPARQL 1.1 urlencoded 协议）→ 表格投影。
// timeoutMs<=0 时取 10s（与 facade 单次超时同量级）。
func Query(ctx context.Context, endpoint, sparql string, limit, timeoutMs int) (*Table, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("endpoint 不能为空")
	}
	if err := VetReadonlySelect(sparql); err != nil {
		return nil, err
	}
	limit = ClampLimit(limit)
	if timeoutMs <= 0 {
		timeoutMs = 10_000
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	form := url.Values{"query": {sparql}}
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("查询端点返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseResults(data, limit)
}

func parseResults(data []byte, limit int) (*Table, error) {
	var parsed sparqlResults
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("SPARQL results JSON 解析失败: %s", err.Error())
	}
	cols := append([]string(nil), parsed.Head.Vars...)
	sort.Strings(cols)
	t := &Table{Columns: cols, Rows: [][]string{}, Total: len(parsed.Results.Bindings)}
	bindings := parsed.Results.Bindings
	if len(bindings) > limit {
		bindings = bindings[:limit]
		t.Truncated = true
	}
	for _, b := range bindings {
		row := make([]string, len(cols))
		for i, c := range cols {
			if term, ok := b[c]; ok {
				row[i] = termValue(term)
			}
		}
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

func termValue(term map[string]any) string {
	if v, ok := term["value"].(string); ok {
		return v
	}
	return ""
}
