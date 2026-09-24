// Package fuseki Apache Jena Fuseki 适配器（O6，方案 04 §4.2：SPARQL + RDFS/OWL 推理，可配置开启）。
// 启动流程：按 profile 建数据目录 → fuseki-server --conf 装载（推理级别经 config.ttl 的
// tdb:unionDefaultGraph + ja:model/infModel 表达）→ 健康检查 /$/ping。
// 推理实现说明（P1 通用版）：为控制复杂度，推理开关经 JVM 级 --generalAssemblerProfile
// 不可行（需 per-dataset assembler 配置），故采用方案 A：每个 profile 生成一份 config.ttl，
// dataset 用 infModel 包装（reasoning=true 时 rdfsLevel=OWL_RDFS_MEM_RDFS_INF，false 时纯 TDB）。
// Java 二进制要求：JDK 11+（apache-jena-fuseki 6.x 需 17+，见 FUSEKI_JAVA 提示）。
package fuseki

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
)

type Runtime struct {
	Binary  string // fuseki-server 启动脚本路径
	JavaBin string // java 可执行（FUSEKI_JAVA，默认 PATH 上的 java）
	DataDir string // 数据根目录（每 profile 一个子目录）
	LogDir  string // 引擎日志目录
}

func New(binary, javaBin, dataDir, logDir string) *Runtime {
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(logDir, 0o755)
	return &Runtime{Binary: binary, JavaBin: javaBin, DataDir: dataDir, LogDir: logDir}
}

// Probe 引擎自检（REQ-146）：fuseki 依赖 JDK + 分发包解压，不支持一键安装（诚实边界），
// 只报 detected/缺失 + 手动指引。
func (r *Runtime) Probe() engine.EngineStatus {
	st := engine.EngineStatus{Engine: "fuseki", Registered: true}
	cands := []string{r.Binary}
	if p, err := exec.LookPath("fuseki-server"); err == nil {
		cands = append(cands, p)
	}
	st.Searched = cands
	if bin, ok := engine.FindExecutable(cands); ok {
		st.Installed = true
		st.Binary = bin
		if _, jok := engine.FindExecutable([]string{r.JavaBin, "java"}); !jok {
			st.Hint = "fuseki-server 已找到，但未找到 java（JDK 17+）：安装 JDK 并加入 PATH，或设置 FUSEKI_JAVA 后重启 runtimed"
		}
		return st
	}
	st.Hint = "未安装：请下载 apache-jena-fuseki（https://jena.apache.org/download/）解压，设置 FUSEKI_BIN 指向 fuseki-server 启动脚本（需 JDK 17+）后重启 runtimed；暂不支持一键安装"
	return st
}

// reasoningConfig 生成带/不带推理的 dataset 配置（Turtle，assembler 语法）。
// reasoning=true：ja:InfModel + RDFS/OWL 规则推理（GenericRuleReasoner，OWL_FB 规则集）；
// reasoning=false：纯 TDB2 数据集（与 Oxigraph 同为无推理基线）。
// rulesFileURL 为 OWL_FB 规则文件绝对 file: URL（Start 时从 fuseki-server.jar 解出）。
//
// 数据一律装入默认图（tdbloader 不带 --graph）：TDB2 的 unionDefaultGraph 只在
// 数据集级 assembler 生效，图级 tdb:GraphTDB 会静默忽略，推理侧 InfModel 的
// baseModel 指向默认图才能看到数据。多本体共存默认图，URI 按本体 ID 命名空间隔离。
func reasoningConfig(datasetName, dbDir, rulesFileURL string, reasoning bool) string {
	var b strings.Builder
	b.WriteString("@prefix tdb:   <http://jena.apache.org/2016/tdb#> .\n")
	b.WriteString("@prefix rdf:   <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .\n")
	b.WriteString("@prefix rdfs:  <http://www.w3.org/2000/01/rdf-schema#> .\n")
	b.WriteString("@prefix ja:    <http://jena.hpl.hp.com/2005/11/Assembler#> .\n")
	b.WriteString("@prefix fuseki: <http://jena.apache.org/fuseki#> .\n\n")
	b.WriteString("<#service> rdf:type fuseki:Service ;\n")
	fmt.Fprintf(&b, "    fuseki:name \"%s\" ;\n", datasetName)
	b.WriteString("    fuseki:endpoint [ fuseki:operation fuseki:query ; ] ;\n")
	b.WriteString("    fuseki:endpoint [ fuseki:operation fuseki:update ; ] ;\n")
	b.WriteString("    fuseki:dataset <#dataset> ;\n    .\n\n")
	if reasoning {
		// 推理模式：InfModel 包 TDB2 默认图（数据装入默认图，见函数头注释）
		b.WriteString("<#dataset> rdf:type ja:RDFDataset ;\n")
		b.WriteString("    ja:defaultGraph <#infModel> ;\n    .\n\n")
		b.WriteString("<#baseModel> rdf:type tdb:GraphTDB ;\n")
		fmt.Fprintf(&b, "    tdb:location \"%s\" ;\n", dbDir)
		b.WriteString("    .\n\n")
		b.WriteString("<#infModel> rdf:type ja:InfModel ;\n")
		b.WriteString("    ja:baseModel <#baseModel> ;\n")
		b.WriteString("    ja:reasoner [ rdf:type ja:GenericRuleReasoner ;\n")
		fmt.Fprintf(&b, "        ja:rulesFrom <%s> ;\n", rulesFileURL)
		b.WriteString("        ja:mode \"FULL\" ; ] ;\n    .\n")
	} else {
		// 无推理：纯 TDB2 数据集。数据在默认图（见函数头注释），不开 union——
		// unionDefaultGraph 使默认图=命名图并集，反而盖掉裸默认图里的数据。
		b.WriteString("<#dataset> rdf:type tdb:DatasetTDB2 ;\n")
		fmt.Fprintf(&b, "    tdb:location \"%s\" ;\n", dbDir)
		b.WriteString("    .\n")
	}
	return b.String()
}

// Start 装载并启动（无推理基线；带推理用 StartWithReasoning）。
func (r *Runtime) Start(ctx context.Context, profileID string, port int, ttls map[string]string) (*engine.Process, error) {
	return r.StartWithReasoning(ctx, profileID, port, ttls, false)
}

// StartWithReasoning 装载并启动。ttls: ontology_id → TTL 内容；reasoning=true 开 RDFS/OWL 推理。
func (r *Runtime) StartWithReasoning(ctx context.Context, profileID string, port int, ttls map[string]string, reasoning bool) (*engine.Process, error) {
	// 预检：fuseki-server 脚本 + java
	if _, err := os.Stat(r.Binary); err != nil {
		if _, lookErr := exec.LookPath(r.Binary); lookErr != nil {
			return nil, fmt.Errorf("未找到 fuseki-server（%q 不存在且不在 PATH）: 请从 https://jena.apache.org/download/ 下载 apache-jena-fuseki 并解压，设置 FUSEKI_BIN 指向其 fuseki-server 启动脚本后重启 runtimed（本仓库 data/bin/ 下亦随版本放置）", r.Binary)
		}
	}
	java := r.JavaBin
	if java == "" {
		java = "java"
	}
	if _, err := exec.LookPath(java); err != nil {
		return nil, fmt.Errorf("未找到 java（Fuseki 为 Java 进程，需 JDK 17+）: 请安装 JDK 并加入 PATH，或设置 FUSEKI_JAVA 指向 java 可执行文件")
	}

	dir := filepath.Join(r.DataDir, profileID)
	_ = os.RemoveAll(dir) // 幂等：重建数据目录（显式重载语义，REQ-87）
	dbDir := filepath.Join(dir, "tdb")
	loadDir := filepath.Join(dir, "load")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	if err := os.MkdirAll(loadDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建装载目录失败: %w", err)
	}

	// 装载：s-put 逐本体入默认图（TDB2 命令行装载，避免 text/turtle POST 到未启动服务）
	for oid, ttl := range ttls {
		f := filepath.Join(loadDir, sanitizeID(oid)+".ttl")
		if err := os.WriteFile(f, []byte(ttl), 0o644); err != nil {
			return nil, err
		}
	}
	// 生成 dataset 配置（推理开关）；推理时从 fuseki-server.jar 解出 OWL_FB 规则文件（绝对 file: URL，不依赖 CWD）
	rulesURL := ""
	if reasoning {
		rf, err := extractRulesFile(java, filepath.Dir(r.Binary), dir)
		if err != nil {
			return nil, err
		}
		rulesURL = "file://" + rf
	}
	cfgPath := filepath.Join(dir, "config.ttl")
	if err := os.WriteFile(cfgPath, []byte(reasoningConfig("ds", dbDir, rulesURL, reasoning)), 0o644); err != nil {
		return nil, err
	}

	// 装载阶段：tdbloader 离线灌入默认图（快于在线 SPARQL PUT，且不依赖服务启动）。
	// 不带 --graph：推理侧 InfModel baseModel 指向默认图，见 reasoningConfig 注释。
	// Fuseki 6.x 精简发行版无独立 tdbloader 脚本，统一走 java -cp fuseki-server.jar tdb2.tdbloader。
	fusekiJar := filepath.Join(filepath.Dir(r.Binary), "fuseki-server.jar")
	for oid := range ttls {
		f := filepath.Join(loadDir, sanitizeID(oid)+".ttl")
		cmd := exec.CommandContext(ctx, java, "-cp", fusekiJar, "tdb2.tdbloader", "--loc", dbDir, f)
		cmd.Env = append(os.Environ(), fmt.Sprintf("JAVA_HOME=%s", javaHome(java)))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("tdbloader 装载 %s 失败: %v: %s", oid, err, tail(out, 300))
		}
		_ = os.Remove(f)
	}

	// serve：fuseki-server --config config.ttl --port（Fuseki 6.x 无 --host 参数，默认监听 localhost）
	cmd := exec.Command(r.Binary, "--config", cfgPath, "--port", fmt.Sprint(port))
	cmd.Env = append(os.Environ(), fmt.Sprintf("JAVA=%s", java), fmt.Sprintf("FUSEKI_HOME=%s", filepath.Dir(r.Binary)))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("fuseki-server 启动失败: %w", err)
	}
	logPath := filepath.Join(r.LogDir, profileID+".log")
	go drainLog(logPath, stderr)
	go drainLog(logPath, stdout)

	// Fuseki 6.x dataset 端点为 /ds/（POST SPARQL 即查询；GET /$/ping 做健康探测）
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/ds/", port)
	p := engine.NewProcess(endpoint, cmd.Process.Pid, func() error {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(8 * time.Second): // JVM 退出较慢，宽限 8s
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	})
	return p, nil
}

// HealthCheck SPARQL ping（Fuseki 就绪标志：/$/ping 200 或 query 端点可应答）。
func (r *Runtime) HealthCheck(ctx context.Context, endpoint string) error {
	ping := strings.Replace(endpoint, "/ds/", "/$/ping", 1)
	tr := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", ping, nil)
	if err != nil {
		return err
	}
	if resp, err := tr.Do(req); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}
	// ping 未就绪则探 query 端点
	req2, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader("SELECT * WHERE {} LIMIT 1"))
	if err != nil {
		return err
	}
	req2.Header.Set("Content-Type", "application/sparql-query")
	resp, err := tr.Do(req2)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("SPARQL 端点异常: %s", resp.Status)
	}
	return nil
}

// extractRulesFile 从 fuseki-server.jar 解出 etc/owl-fb.rules 到 destDir，返回绝对路径。
// Jena 6.x 内置 urn:x-jena:* 规则 URL 处理器已移除，故显式解压后以 file: URL 引用。
// JDK 自带 jar 工具（<javaHome>/bin/jar）；找不到时回退 PATH 上的 jar。
func extractRulesFile(java, fusekiDir, destDir string) (string, error) {
	jar := filepath.Join(fusekiDir, "fuseki-server.jar")
	dst := filepath.Join(destDir, "owl-fb.rules")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // 已解出（同 profile 重启复用）
	}
	jarTool := filepath.Join(javaHome(java), "bin", "jar")
	if _, err := os.Stat(jarTool); err != nil {
		jarTool = "jar"
	}
	jarCmd := exec.Command(jarTool, "xf", jar, "etc/owl-fb.rules")
	jarCmd.Dir = destDir
	if out, err := jarCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("解压 OWL 推理规则文件失败: %v: %s", err, tail(out, 200))
	}
	src := filepath.Join(destDir, "etc", "owl-fb.rules")
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("规则文件未解出: %s 不存在", src)
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// javaHome 从 java 可执行路径推导 JAVA_HOME（<home>/bin/java → <home>）；PATH 上的 java 则留空。
func javaHome(javaBin string) string {
	if abs, err := filepath.Abs(javaBin); err == nil && filepath.Base(abs) == "java" {
		return filepath.Dir(filepath.Dir(abs))
	}
	return ""
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func tail(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

func drainLog(path string, r io.Reader) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = io.Copy(f, r)
}
