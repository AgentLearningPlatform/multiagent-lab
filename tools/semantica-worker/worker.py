"""semantica-worker：Semantica 独立集成薄服务（§4.9 D-O10，:8093）。

消费：TTL→OntologyIngestor→GraphBuilder→ContextGraph（内存图+文件持久化）；
检索：AgentContext GraphRAG（向量+图混合）；审计：record_decision（PROV-O，P2 复用 Explorer）。
零侵入两平面/facade；⚠️ 核心依赖重（torch/transformers），venv 数 GB 属预期；本地学习用，无鉴权。
"""
from __future__ import annotations

import os
import tempfile
import threading
import warnings
from pathlib import Path

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from pydantic import BaseModel

SEMANTICA_VERSION = "0.6.8"
PERSIST_PATH = Path(os.getenv("SEMANTICA_DATA_DIR", "./data/semantica/graph.json"))
_lock = threading.Lock()
_graph = None
_context = None
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

# ---- 契约端点 ----
class IngestReq(BaseModel):
    ontology_id: str
    ttl: str
class QueryReq(BaseModel):
    q: str
    max_results: int = 5
class DecisionReq(BaseModel):
    category: str
    scenario: str
    reasoning: str
    outcome: str
    confidence: float | None = None

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

if __name__ == "__main__":
    import uvicorn
    host, _, port = os.getenv("SEMANTICA_ADDR", ":8093").rpartition(":")
    uvicorn.run(app, host=host or "0.0.0.0", port=int(port or "8093"))
