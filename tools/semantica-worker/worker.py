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
app = FastAPI(title="semantica-worker", version=SEMANTICA_VERSION)
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])
_lock = threading.Lock()
_graph = None
_context = None
_warnings: list[str] = []

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

@app.on_event("startup")
def _startup():
    try:
        _ensure_graph()
    except Exception as e:  # noqa: BLE001
        warnings.warn(f"semantica 图初始化失败: {e}")

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
