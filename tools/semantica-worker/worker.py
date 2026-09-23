"""semantica-worker：Semantica 独立集成薄服务（§4.9 D-O10，:8093）。

消费：TTL→OntologyIngestor→GraphBuilder→ContextGraph（内存图+文件持久化）；
检索：AgentContext GraphRAG（向量+图混合）；审计：record_decision（PROV-O，P2 复用 Explorer）。
零侵入两平面/facade；⚠️ 核心依赖重（torch/transformers），venv 数 GB 属预期；本地学习用，无鉴权。
"""
from __future__ import annotations

import json
import os
import tempfile
import threading
import warnings
from pathlib import Path

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse, Response
from pydantic import BaseModel

SEMANTICA_VERSION = "0.6.8"
PERSIST_PATH = Path(os.getenv("SEMANTICA_DATA_DIR", "./data/semantica/graph.json"))
CAUSAL_TYPES = {"CAUSED", "INFLUENCED", "PRECEDENT_FOR"}  # add_causal_relationship 允许值
_lock = threading.Lock()
_graph = None
_context = None
_prov = None  # ProvenanceManager 单例（REQ-101 溯源）
_warnings: list[str] = []
_mcp = None  # FastMCP 单例（mcp SDK 可用时挂载 /mcp 端点，REQ-99 ③）

from contextlib import asynccontextmanager as _asynccontextmanager

@_asynccontextmanager
async def _lifespan(app):
    """图初始化 + MCP session manager 生命周期。

    ⚠️ Starlette 挂载子应用不会自动运行其 lifespan——FastMCP 的 session_manager
    必须由父应用 lifespan 显式驱动，否则 /mcp 端点 503。
    """
    try:
        _ensure_graph()
    except Exception as e:  # noqa: BLE001
        warnings.warn(f"semantica 图初始化失败: {e}")
    sm = getattr(_mcp, "session_manager", None) if _mcp is not None else None
    if sm is not None:
        async with sm:
            yield
    else:
        yield

app = FastAPI(title="semantica-worker", version=SEMANTICA_VERSION, lifespan=_lifespan)
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

# ---- 防御式工具：semantica 各版本方法名/返回形状存在差异，全部容错 ----
def _pick(obj, *keys, default=None):
    """从 dict 或对象上按序取第一个非 None 字段。"""
    for k in keys:
        v = obj.get(k) if isinstance(obj, dict) else getattr(obj, k, None)
        if v is not None:
            return v
    return default
def _as_list(value):
    if value is None:
        return []
    if isinstance(value, dict):
        for k in ("claims", "entities", "nodes", "relationships", "edges", "decisions", "items", "results"):
            if isinstance(value.get(k), list):
                return value[k]
        return list(value.values())
    return list(value) if isinstance(value, (list, tuple, set)) else [value]
def _count(value):
    return len(value) if hasattr(value, "__len__") else 0
def _method(obj, *names):
    return next((getattr(obj, n) for n in names if callable(getattr(obj, n, None))), None)
def _build_graph():
    global _graph
    from semantica.context import ContextGraph
    if PERSIST_PATH.exists():
        try:
            from semantica.context import GraphSession
            sess = GraphSession.from_file(str(PERSIST_PATH))
            _graph = _pick(sess, "graph", "context_graph", "kg", default=sess)
        except Exception as e:  # noqa: BLE001
            _warnings.append(f"持久化图加载失败，改为新建空图: {e}")
        if _graph is not None:
            return
    try:
        _graph = ContextGraph(advanced_analytics=True)
    except TypeError:
        _graph = ContextGraph()
def _ensure_graph():
    global _graph
    if _graph is None:
        with _lock:
            if _graph is None:
                _build_graph()
    return _graph
def _save_graph():
    save = _method(_graph, "save_to_file", "save", "persist") if _graph is not None else None
    if save is None:
        return
    PERSIST_PATH.parent.mkdir(parents=True, exist_ok=True)
    try:
        save(str(PERSIST_PATH))
    except Exception as e:  # noqa: BLE001
        _warnings.append(f"持久化保存失败: {e}")
def _ensure_context():
    global _context
    if _context is None:
        from semantica.context import AgentContext
        from semantica.vector_store import VectorStore
        try:
            vs = VectorStore(backend="inmemory", dimension=768)
        except TypeError:
            vs = VectorStore(backend="inmemory")
        _context = AgentContext(vector_store=vs, knowledge_graph=_ensure_graph(), decision_tracking=True)
    return _context
def _apply(graph, items, kind, warns):
    """把 GraphBuilder 产物写入 ContextGraph（add_entity/add_relationship，多版本兼容）。"""
    if not items:
        return
    add = _method(graph, f"add_{kind}", "add_node" if kind == "entity" else "add_edge")
    if add is None:
        warns.append(f"ContextGraph 无 add_{kind} 方法，{len(items)} 条 {kind} 未入图（仅计数）")
        return
    for it in items:
        try:
            add(**it) if isinstance(it, dict) else add(it)
        except Exception:  # noqa: BLE001
            pass
def _ingest_ttl(ttl_text):
    from semantica.ingest import OntologyIngestor
    from semantica.kg import GraphBuilder
    with tempfile.NamedTemporaryFile("w", suffix=".ttl", delete=False, encoding="utf-8") as f:
        f.write(ttl_text)
        tmp = f.name
    try:
        ing = OntologyIngestor().ingest_ontology(tmp)
        built = GraphBuilder(merge_entities=True).build(_pick(ing, "data", default=ing))
    finally:
        try:
            os.unlink(tmp)
        except OSError:
            pass
    return (_as_list(_pick(built, "entities", "nodes", default=[])),
            _as_list(_pick(built, "relationships", "edges", default=[])))
def _normalize_claims(result):
    raw = _pick(result, "claims", "results")
    if raw is None:
        raw = result
    return [{"text": str(_pick(c, "text", "claim", "content", "statement", default=c)),
             "source_node": _pick(c, "source_node", "source", "node", "provenance"),
             "score": _pick(c, "score", "similarity", "confidence")} for c in _as_list(raw)]

def _lightweight_extract(chunks: list[dict]):
    """内置轻量 KG 抽取（semantica 抽取器不可用时的降级管线，M14 ⑥ 语义）。

    规则：中英文名词短语为实体（去停用词）；「A 的 B」「A 是/属于 B」「A 包括 B」为关系。
    返回 (entities, relationships)，形态与 GraphBuilder 产物对齐（id/name/type + source/target）。
    """
    import re
    ents: dict[str, dict] = {}
    rels: list[dict] = []
    stop = set("的 是 在 和 与 或 及 我们 你们 他们 它 这 那 一个 一些 该 此 其 被 把 对 从 向 于 上下 左右".split())
    pat_of = re.compile(r"([\u4e00-\u9fa5A-Za-z0-9_]{2,12})的([\u4e00-\u9fa5A-Za-z0-9_]{2,12})")
    pat_is = re.compile(r"([\u4e00-\u9fa5A-Za-z0-9_]{2,12})(?:是|属于|包括|包含)([\u4e00-\u9fa5A-Za-z0-9_]{2,12})")
    for ch in chunks:
        text = str(ch.get("content") or "")
        if not text.strip():
            continue
        src = f"kb:{ch.get('doc_id', '')}:{ch.get('seq', '')}"
        # 按句抽取：「A 是 B」所在句回填实体 desc（O13 策略 B/C 的 definition 来源）
        for sent in re.split(r"[。！？!?;\n\r]+", text):
            s = sent.strip()
            if not s:
                continue
            for m in pat_of.finditer(s):
                a, b = m.group(1), m.group(2)
                if a in stop or b in stop:
                    continue
                ents.setdefault(a, {"id": a, "name": a, "type": "entity"})
                ents.setdefault(b, {"id": b, "name": b, "type": "entity"})
                rels.append({"source": a, "target": b, "type": "HAS", "provenance": src})
            for m in pat_is.finditer(s):
                a, b = m.group(1), m.group(2)
                if a in stop or b in stop or a == b:
                    continue
                ents.setdefault(a, {"id": a, "name": a, "type": "entity"})
                ents.setdefault(b, {"id": b, "name": b, "type": "entity"})
                ents[a].setdefault("desc", s[:120])  # 首个定义句作实体描述
                rels.append({"source": a, "target": b, "type": "IS_A", "provenance": src})
    return list(ents.values()), rels

def _graphrag_ingest_chunks(req: "GraphragIngestReq"):
    try:  # semantica 缺失时图不可用：跳过入图，仅完成抽取 + per-KB 记录（O13 回读不受阻）
        g = _ensure_graph()
    except Exception as e:  # noqa: BLE001
        g = None
        _warnings.append(f"semantica 图不可用，KG 仅按 KB 记录（/graphrag/kg 可回读）: {e}")
    chunks = [c for c in (req.chunks or []) if str(c.get("content") or "").strip()]
    if not chunks:
        return JSONResponse(status_code=400, content={"error": "chunks 为空（需含 content 字段）"})
    ents: list = []
    rels: list = []
    warns: list[str] = []
    method = "lightweight"
    try:  # 优先 semantica 原生抽取器
        from semantica.ingest import TextIngestor  # noqa: F401
        from semantica.kg import GraphBuilder
        texts = [str(c.get("content")) for c in chunks]
        ing = TextIngestor().ingest_text(texts) if hasattr(TextIngestor(), "ingest_text") else None
        if ing is not None:
            built = GraphBuilder(merge_entities=True).build(_pick(ing, "data", default=ing))
            ents = _as_list(_pick(built, "entities", "nodes", default=[]))
            rels = _as_list(_pick(built, "relationships", "edges", default=[]))
            method = "semantica"
    except Exception as e:  # noqa: BLE001
        warns.append(f"semantica 抽取器不可用，降级轻量规则抽取: {e}")
    if method == "lightweight":
        ents, rels = _lightweight_extract(chunks)
    with _lock:
        if g is not None:
            _apply(g, ents, "entity", warns)
            _apply(g, rels, "relationship", warns)
        try:  # chunk 原文进向量索引（GraphRAG 检索的文本侧）
            text = " ".join(str(c.get("content")) for c in chunks)[:8000]
            if text.strip():
                ctx = _ensure_context()
                try:
                    ctx.store(text, conversation_id=f"kb:{req.kb_id}")
                except TypeError:
                    ctx.store(text)
        except Exception as e:  # noqa: BLE001
            warns.append(f"向量索引写入失败（GraphRAG 检索可能降级）: {e}")
        _record_kbkg(req.kb_id, method, ents, rels)  # O13：按 KB 记录 KG 子图（/graphrag/kg 回读）
        _save_graph()
    return {"kb_id": req.kb_id, "method": method, "chunks": len(chunks),
            "entities": len(ents), "relationships": len(rels), "warnings": warns + _warnings}


# ---- O13（D-O14/REQ-108）：per-KB KG 记录与回读（本体平面零侵入，仅 worker 接口面扩展） ----
KBKG_PATH = PERSIST_PATH.parent / "kb_kg.json"
_kbkg: dict | None = None

def _normalize_ent(e):
    """KG 实体归一化（dict/对象/标量 → {id, name, type, desc}）。"""
    if isinstance(e, (str, int, float)):
        s = str(e)
        return {"id": s, "name": s, "type": "entity", "desc": ""}
    return {"id": str(_pick(e, "id", "name", "label", default="")),
            "name": str(_pick(e, "name", "label", "id", default="")),
            "type": str(_pick(e, "type", "entity_type", default="entity") or "entity"),
            "desc": str(_pick(e, "desc", "description", default="") or "")}

def _normalize_rel(r):
    """KG 关系归一化（→ {source, target, type}）。"""
    if isinstance(r, (list, tuple)) and len(r) >= 2:
        return {"source": str(r[0]), "target": str(r[1]), "type": "RELATED"}
    return {"source": str(_pick(r, "source", "source_id", "from", default="")),
            "target": str(_pick(r, "target", "target_id", "to", default="")),
            "type": str(_pick(r, "type", "relationship_type", "relation", default="RELATED") or "RELATED")}

def _load_kbkg() -> dict:
    global _kbkg
    if _kbkg is None:
        try:
            _kbkg = json.loads(KBKG_PATH.read_text(encoding="utf-8")) if KBKG_PATH.exists() else {}
        except Exception:  # noqa: BLE001
            _kbkg = {}
    return _kbkg

def _save_kbkg():
    try:
        KBKG_PATH.parent.mkdir(parents=True, exist_ok=True)
        KBKG_PATH.write_text(json.dumps(_load_kbkg(), ensure_ascii=False), encoding="utf-8")
    except Exception as e:  # noqa: BLE001
        _warnings.append(f"kb_kg 持久化失败: {e}")

def _record_kbkg(kb_id: str, method: str, ents: list, rels: list):
    """按 kb_id 合并记录抽取产物（entity 按 name 去重保留首个 desc；关系三元组去重）。"""
    data = _load_kbkg()
    rec = data.setdefault(str(kb_id), {"method": method, "entities": [], "relationships": []})
    seen_e = {e["name"] or e["id"] for e in rec["entities"]}
    seen_r = {(r["source"], r["target"], r["type"]) for r in rec["relationships"]}
    for raw in ents:
        e = _normalize_ent(raw)
        key = e["name"] or e["id"]
        if not key or key in seen_e:
            continue
        seen_e.add(key)
        rec["entities"].append(e)
    for raw in rels:
        r = _normalize_rel(raw)
        key = (r["source"], r["target"], r["type"])
        if not r["source"] or not r["target"] or key in seen_r:
            continue
        seen_r.add(key)
        rec["relationships"].append(r)
    rec["method"] = method
    _save_kbkg()

class GraphragKGReq(BaseModel):
    """O13 策略 B/C：按 kb_id 回读该库抽取出的 KG 子图（04 §3.7 kg-to-spec-json 数据源）。"""
    kb_id: str

@app.post("/graphrag/kg")
def graphrag_kg(req: GraphragKGReq):
    rec = _load_kbkg().get(str(req.kb_id)) or {}
    return {"kb_id": str(req.kb_id), "method": rec.get("method", ""),
            "entities": rec.get("entities", []), "relationships": rec.get("relationships", [])}

# ---- 审计/溯源（REQ-101，§4.9.4）：决策链 + PROV-O lineage/export + Explorer 惰性挂载 ----
def _normalize_node(x):
    """归一化决策链/图节点（dict 或对象；标量退化为 {id}）。"""
    if isinstance(x, (str, int, float)):
        return {"id": str(x)}
    return {"id": str(_pick(x, "id", "decision_id", "node_id", default="")),
            "category": _pick(x, "category"), "scenario": _pick(x, "scenario"),
            "outcome": _pick(x, "outcome"), "confidence": _pick(x, "confidence"),
            "relation": _pick(x, "relationship_type", "relation", "type"),
            "ts": _pick(x, "ts", "timestamp", "created_at", "time")}
def _normalize_prov(x):
    """归一化 PROV-O 溯源条目。"""
    if isinstance(x, (str, int, float)):
        return {"id": str(x)}
    return {"id": str(_pick(x, "id", "entity_id", "name", "uri", default="")),
            "source": _pick(x, "source", "source_node", "wasDerivedFrom", "was_derived_from"),
            "metadata": _pick(x, "metadata", "meta"),
            "type": _pick(x, "type", "entity_type", "prov_type")}
def _ensure_prov():
    """惰性创建 ProvenanceManager（storage 落在 SEMANTICA_DATA_DIR 同目录 audit.db）。"""
    global _prov
    if _prov is None:
        from semantica.provenance import ProvenanceManager
        PERSIST_PATH.parent.mkdir(parents=True, exist_ok=True)
        db = str(PERSIST_PATH.parent / "audit.db")
        try:
            _prov = ProvenanceManager(storage_path=db)
        except TypeError:
            _prov = ProvenanceManager()
    return _prov
async def _asgi_json(send, status, msg):
    body = json.dumps({"error": msg}, ensure_ascii=False).encode("utf-8")
    await send({"type": "http.response.start", "status": status,
                "headers": [(b"content-type", b"application/json; charset=utf-8"),
                            (b"content-length", str(len(body)).encode())]})
    await send({"type": "http.response.body", "body": body})
class _ExplorerMount:
    """惰性挂载 semantica Explorer ASGI（首次访问创建并缓存；剥离 X-Frame-Options 便于 iframe 嵌入）。

    首次访问才 import/create_app——启动时图单例可能尚未就绪；未安装 semantica[explorer] 时返回 503 JSON，
    不影响 worker 其余端点启动。
    """
    def __init__(self):
        self._app = None
    def _get_app(self):
        if self._app is None:
            from semantica.explorer.app import create_app
            try:
                from semantica.context import GraphSession
                self._app = create_app(session=GraphSession(_ensure_graph()))
            except TypeError:
                self._app = create_app(_ensure_graph())
        return self._app
    async def __call__(self, scope, receive, send):
        if scope.get("type") != "http":
            return
        try:
            sub = self._get_app()
        except ImportError as e:  # noqa: BLE001
            await _asgi_json(send, 503, f"semantica[explorer] 未安装: {e}")
            return
        except Exception as e:  # noqa: BLE001
            await _asgi_json(send, 503, f"Explorer 初始化失败: {e}")
            return
        async def _send(message):
            # 剥离 X-Frame-Options（存在才剥离；不存在则原样透传）
            if message["type"] == "http.response.start":
                message = {**message, "headers": [(k, v) for k, v in message.get("headers", [])
                                                  if k.lower() != b"x-frame-options"]}
            await send(message)
        await sub(scope, receive, _send)

# ---- MCP 端点（REQ-99 ③）：Streamable HTTP，与 REST 共享同一图/上下文单例 ----
# 平台挂载：agent 级 mcp_servers 配 {name:"semantica", url:"http://127.0.0.1:8093/mcp"}
try:
    from mcp.server.fastmcp import FastMCP  # mcp>=1.2.0,<2（v2 改名 MCPServer，破坏性）
    _mcp = FastMCP("semantica")

    @_mcp.tool()
    def extract_entities(text: str, method: str = "pattern") -> list:
        """从文本抽取实体（pattern 法默认，无需模型下载）。"""
        from semantica.semantic_extract import NERExtractor
        try:
            ner = NERExtractor(method=method)
        except TypeError:
            ner = NERExtractor()
        call = _method(ner, "extract_entities", "extract", "process")
        raw = call(text) if call else ner
        return [str(_pick(e, "name", "text", "entity", default=e)) for e in _as_list(raw)]

    @_mcp.tool()
    def extract_relations(text: str) -> list:
        """从文本抽取关系三元组。"""
        from semantica.semantic_extract import RelationExtractor
        try:
            rex = RelationExtractor(bidirectional=True)
        except TypeError:
            rex = RelationExtractor()
        call = _method(rex, "extract_relations", "extract")
        raw = call(text) if call else rex
        return [{"head": str(_pick(r, "head", "source", "from", default="")),
                 "relation": str(_pick(r, "relation", "type", "label", default="")),
                 "tail": str(_pick(r, "tail", "target", "to", default="")),
                 "confidence": _pick(r, "confidence", "score")} for r in _as_list(raw)]

    @_mcp.tool()
    def record_decision(category: str, scenario: str, reasoning: str, outcome: str,
                        confidence: float = 1.0) -> str:
        """记录一条决策（PROV-O 溯源入口），返回 decision_id。"""
        g = _ensure_graph()
        try:
            did = g.record_decision(category=category, scenario=scenario,
                                    reasoning=reasoning, outcome=outcome, confidence=confidence)
        except TypeError:
            did = g.record_decision(category, scenario, reasoning, outcome)
        _save_graph()
        return str(_pick(did, "decision_id", "id", default=did))

    @_mcp.tool()
    def query_decisions(category: str = "", limit: int = 20) -> list:
        """按类别（可空=全部）查询决策记录。"""
        rows = decisions(limit * 4)["decisions"]
        if category:
            rows = [d for d in rows if d.get("category") == category]
        return rows[: max(0, limit)]

    @_mcp.tool()
    def find_precedents(query: str, max_results: int = 5) -> list:
        """按语义相似查找历史决策先例。"""
        g = _ensure_graph()
        finder = _method(g, "find_similar_decisions")
        if finder is None:
            return []
        try:
            return _as_list(finder(query, max_results))
        except TypeError:
            return _as_list(finder(query))

    @_mcp.tool()
    def get_causal_chain(decision_id: str) -> object:
        """追溯决策因果链（PROV-O 溯源）。"""
        g = _ensure_graph()
        tracer = _method(g, "trace_decision_chain")
        return tracer(decision_id) if tracer else {"error": "graph 不支持因果链追溯"}

    @_mcp.tool()
    def add_entity(name: str, entity_type: str = "", attributes: dict | None = None) -> str:
        """向知识图谱添加实体。"""
        g = _ensure_graph()
        add = _method(g, "add_entity", "add_node")
        if add is None:
            return "graph 不支持 add_entity"
        try:
            add(name, entity_type, attributes or {})
        except TypeError:
            add(name)
        _save_graph()
        return f"实体 {name} 已添加"

    @_mcp.tool()
    def add_relationship(from_entity: str, to_entity: str, relation: str) -> str:
        """向知识图谱添加关系（from → to）。"""
        g = _ensure_graph()
        add = _method(g, "add_relationship", "add_edge")
        if add is None:
            return "graph 不支持 add_relationship"
        try:
            add(from_entity, to_entity, relation)
        except TypeError:
            add(from_entity, relation, to_entity)
        _save_graph()
        return f"关系 {from_entity} -{relation}-> {to_entity} 已添加"

    @_mcp.tool()
    def run_reasoning(query: str) -> dict:
        """图检索（reasoning 模块 API 不稳定，暂以关键词图检索代替）。"""
        g = _ensure_graph()
        q = _method(g, "query")
        raw = q(query) if q else {}
        return {"query": query, "results": _as_list(_pick(raw, "results", "nodes", default=raw))[:20]}

    @_mcp.tool()
    def get_graph_analytics() -> dict:
        """知识图谱分析摘要（计数 + 可用分析器结果）。"""
        g = _ensure_graph()
        out: dict[str, object] = {"entities": _count(_pick(g, "entities", "nodes", default=[])),
                                  "relationships": _count(_pick(g, "relationships", "edges", default=[])),
                                  "decisions": _count(_pick(g, "decisions", default=[]))}
        analyzer = _method(g, "analyze_graph")
        if analyzer is not None:
            try:
                out["analytics"] = _pick(analyzer(g), "analytics", default=analyzer(g))
            except Exception:  # noqa: BLE001
                pass
        return out

    @_mcp.tool()
    def export_graph() -> dict:
        """导出知识图谱为 dict（to_kg_dict）。"""
        g = _ensure_graph()
        exporter = _method(g, "to_kg_dict")
        return exporter() if exporter else {}

    @_mcp.tool()
    def get_graph_summary() -> dict:
        """知识图谱摘要（计数 + 实体样例）。"""
        g = _ensure_graph()
        ents = _as_list(_pick(g, "entities", "nodes", default=[]))
        return {"entities": len(ents),
                "relationships": _count(_pick(g, "relationships", "edges", default=[])),
                "decisions": _count(_pick(g, "decisions", default=[])),
                "sample_entities": [str(_pick(e, "name", "id", "label", default=e)) for e in ents[:10]]}

    app.mount("/mcp", _mcp.streamable_http_app())
except ImportError as e:  # noqa: BLE001
    warnings.warn(f"mcp SDK 未安装（pip install 'mcp>=1.2.0,<2'），/mcp 端点不可用: {e}")

# Explorer 审计视图（REQ-101，§4.9.2）：/explorer 惰性挂载，未安装 semantica[explorer] 时返回 503 JSON
app.mount("/explorer", _ExplorerMount())

# ---- 契约端点 ----
class IngestReq(BaseModel):
    ontology_id: str
    ttl: str
class QueryReq(BaseModel):
    q: str
    max_results: int = 5

class GraphragIngestReq(BaseModel):
    """M14/KB-O4：KB chunk 集合 → KG 抽取（D-O14 策略 B 数据源；02 v0.25）。

    chunks: [{id, doc_id, seq, content}]；kb_id 仅作 provenance 标注。
    抽取管线：semantica TextIngestor/KGBuilder 可用则用之；否则内置轻量规则抽取
    （名词短语实体 + 「A 的 B」「A 是 B」关系），保证链路在无重依赖环境可演示。
    """
    kb_id: str = ""
    chunks: list[dict] = []
class DecisionReq(BaseModel):
    category: str
    scenario: str
    reasoning: str
    outcome: str
    confidence: float | None = None
class CausalReq(BaseModel):
    from_id: str
    to_id: str
    type: str = "CAUSED"

@app.get("/health")
def health():
    g = _graph
    if g is None:
        return {"ok": True, "version": SEMANTICA_VERSION, "graph_loaded": False,
                "entities": 0, "relationships": 0, "decisions": 0}
    return {"ok": True, "version": SEMANTICA_VERSION, "graph_loaded": True,
            "entities": _count(_pick(g, "entities", "nodes", default=[])),
            "relationships": _count(_pick(g, "relationships", "edges", default=[])),
            "decisions": _count(_pick(g, "decisions", default=[]))}

@app.post("/ingest-ttl")
def ingest_ttl(req: IngestReq):
    g = _ensure_graph()
    try:
        ents, rels = _ingest_ttl(req.ttl)
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"TTL 摄入失败: {e}"})
    warns: list[str] = []
    with _lock:
        _apply(g, ents, "entity", warns)
        _apply(g, rels, "relationship", warns)
        try:
            text = " ".join(str(_pick(e, "text", "label", "name", "description", "id", default="")) for e in ents)
            if text.strip():
                ctx = _ensure_context()
                try:
                    ctx.store(text[:8000], conversation_id="ingest")
                except TypeError:
                    ctx.store(text[:8000])
        except Exception as e:  # noqa: BLE001
            warns.append(f"向量索引写入失败（GraphRAG 检索可能降级）: {e}")
        _save_graph()
    return {"ontology_id": req.ontology_id, "entities": len(ents), "relationships": len(rels),
            "warnings": warns + _warnings}

@app.post("/query")
def query(req: QueryReq):
    _ensure_graph()
    try:
        ctx = _ensure_context()
        try:
            result = ctx.retrieve(req.q, use_graph=True, max_results=req.max_results)
        except TypeError:
            result = ctx.retrieve(req.q, max_results=req.max_results)
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"GraphRAG 检索失败: {e}"})
    return {"claims": _normalize_claims(result), "query": req.q}

@app.post("/graphrag/ingest")
def graphrag_ingest(req: GraphragIngestReq):
    return _graphrag_ingest_chunks(req)

@app.post("/decision")
def decision(req: DecisionReq):
    g = _ensure_graph()
    kwargs: dict[str, object] = dict(category=req.category, scenario=req.scenario,
                                     reasoning=req.reasoning, outcome=req.outcome)
    if req.confidence is not None:
        kwargs["confidence"] = req.confidence
    try:
        did = g.record_decision(**kwargs)
    except TypeError:
        did = g.record_decision(req.category, req.scenario, req.reasoning, req.outcome)
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"决策记录失败: {e}"})
    with _lock:
        _save_graph()
    return {"decision_id": did if isinstance(did, str) else str(_pick(did, "decision_id", "id", default=did))}

@app.get("/decisions")
def decisions(limit: int = 20):
    g = _ensure_graph()
    getter = _method(g, "get_decisions", "list_decisions", "recent_decisions")
    raw = None
    if getter is not None:
        try:
            raw = getter()
        except Exception:  # noqa: BLE001
            raw = None
    if raw is None:
        raw = _pick(g, "decisions", "decision_nodes", default=[])
    return {"decisions": [{
        "id": str(_pick(d, "id", "decision_id", "node_id", default="")),
        "category": _pick(d, "category"), "scenario": _pick(d, "scenario"),
        "outcome": _pick(d, "outcome"), "confidence": _pick(d, "confidence"),
        "ts": _pick(d, "ts", "timestamp", "created_at", "time"),
    } for d in _as_list(raw)[: max(0, limit)]]}

@app.get("/stats")
def stats():
    g = _ensure_graph()
    return {"entities": _count(_pick(g, "entities", "nodes", default=[])),
            "relationships": _count(_pick(g, "relationships", "edges", default=[])),
            "decisions": _count(_pick(g, "decisions", default=[]))}

@app.get("/decision-chain/{decision_id}")
def decision_chain(decision_id: str):
    """决策因果链（PROV-O 溯源，§4.9.4）。"""
    g = _ensure_graph()
    tracer = _method(g, "trace_decision_chain")
    if tracer is None:
        return {"decision_id": decision_id, "chain": [], "warnings": ["graph 不支持 trace_decision_chain"]}
    try:
        raw = tracer(decision_id)
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"决策链追溯失败: {e}"})
    items = _pick(raw, "chain", "decisions", "path", "nodes", default=raw)
    return {"decision_id": decision_id, "chain": [_normalize_node(x) for x in _as_list(items)]}

@app.get("/lineage/{entity_id}")
def lineage(entity_id: str):
    """实体溯源 lineage（ProvenanceManager，§4.9.4）。"""
    try:
        prov = _ensure_prov()
    except ImportError as e:  # noqa: BLE001
        return JSONResponse(status_code=503, content={"error": f"semantica provenance 不可用: {e}"})
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"ProvenanceManager 初始化失败: {e}"})
    getter = _method(prov, "get_lineage", "trace_lineage")
    if getter is None:
        return {"entity_id": entity_id, "lineage": [], "warnings": ["ProvenanceManager 不支持 lineage"]}
    try:
        raw = getter(entity_id)
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"溯源查询失败: {e}"})
    items = _pick(raw, "lineage", "entities", "path", "nodes", default=raw)
    return {"entity_id": entity_id, "lineage": [_normalize_prov(x) for x in _as_list(items)]}

@app.get("/prov-export")
def prov_export(format: str = "turtle"):
    """导出 PROV-O（默认 turtle，含 prov:Entity/wasDerivedFrom 等词汇）。"""
    try:
        prov = _ensure_prov()
    except ImportError as e:  # noqa: BLE001
        return JSONResponse(status_code=503, content={"error": f"semantica provenance 不可用: {e}"})
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"ProvenanceManager 初始化失败: {e}"})
    exporter = _method(prov, "export_prov", "export")
    if exporter is None:
        return JSONResponse(status_code=501, content={"error": "ProvenanceManager 不支持导出"})
    try:
        try:
            text = exporter(format=format)
        except TypeError:
            text = exporter()
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"PROV-O 导出失败: {e}"})
    media = "text/turtle" if str(format).lower() in ("turtle", "ttl") else "text/plain; charset=utf-8"
    return Response(content=str(text), media_type=media)

@app.post("/causal")
def causal(req: CausalReq):
    """写入因果/先例关系（CAUSED|INFLUENCED|PRECEDENT_FOR，§4.9.4）。"""
    rtype = (req.type or "CAUSED").upper()
    if rtype not in CAUSAL_TYPES:
        return JSONResponse(status_code=400, content={"error": f"不支持的因果类型: {req.type}（允许 {sorted(CAUSAL_TYPES)}）"})
    g = _ensure_graph()
    adder = _method(g, "add_causal_relationship")
    if adder is None:
        return JSONResponse(status_code=501, content={"error": "graph 不支持 add_causal_relationship"})
    try:
        with _lock:
            try:
                adder(req.from_id, req.to_id, relationship_type=rtype)
            except TypeError:
                adder(req.from_id, req.to_id, rtype)
            _save_graph()
    except Exception as e:  # noqa: BLE001
        return JSONResponse(status_code=500, content={"error": f"因果关系写入失败: {e}"})
    return {"ok": True, "from_id": req.from_id, "to_id": req.to_id, "type": rtype}

if __name__ == "__main__":
    import uvicorn
    host, _, port = os.getenv("SEMANTICA_ADDR", ":8093").rpartition(":")
    uvicorn.run(app, host=host or "0.0.0.0", port=int(port or "8093"))
