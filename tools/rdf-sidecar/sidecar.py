#!/usr/bin/env python3
"""rdf-sidecar: OWL/RDF ↔ spec_json 双向转换（方案 04 v0.6 §2.2/§6，不自研 RDF 解析）。

用法:
  sidecar.py parse  --format owl_rdfxml|turtle < input   -> stdout JSON {spec, warnings, lossy, note}
  sidecar.py export --ontology-id ID [--format turtle] < spec_json -> stdout TTL

导入映射（REQ-69）: rdfs:label→name、rdfs:subClassOf→parents、
owl:NamedIndividual+rdf:type→instance、对象属性断言→实例关系、
其他 datatype 断言→attributes；公理/推理语义丢弃并计入 warnings。
导出 URN 规则与 pkg/ontology/spec 的 sanitize 逐字符一致（查询翻译共用）。
"""
import sys, json

try:
    from rdflib import Graph, RDF, RDFS, OWL, URIRef, Literal
except ImportError:
    sys.stderr.write("rdflib 未安装：pip install rdflib\n")
    sys.exit(3)

RDFXML = "xml"
TTL = "turtle"


def sanitize(s: str) -> str:
    return "".join(ch if (ch.isalnum() or ch in "_-.") else "_" for ch in (s or "").strip())


def local(uri: str) -> str:
    for sep in ("#", "/", ":"):
        if sep in uri:
            uri = uri.rsplit(sep, 1)[-1]
    return uri


def label_or_local(g, subj) -> str:
    for lab in g.objects(subj, RDFS.label):
        s = str(lab).strip()
        if s:
            return s
    return local(str(subj))


def parse(format: str) -> int:
    content = sys.stdin.read()
    g = Graph()
    try:
        g.parse(data=content, format=RDFXML if format == "owl_rdfxml" else TTL)
    except Exception as e:  # noqa: BLE001
        sys.stderr.write(f"RDF 解析失败: {e}\n")
        return 1

    warnings, lossy = [], False
    spec = {"name": "", "description": "", "concepts": [], "relations": [], "instances": []}

    # ---- 类：owl:Class / rdfs:Class（跳过系统内建类）----
    seen_c = {}
    classes = set(g.subjects(RDF.type, OWL.Class)) | set(g.subjects(RDF.type, RDFS.Class))
    for c in classes:
        if not isinstance(c, URIRef):
            continue
        uri = str(c)
        if uri.startswith(str(RDFS)) or uri.startswith(str(OWL)) or uri.startswith(str(RDF)):
            continue
        name = label_or_local(g, c)
        if name in seen_c:
            warnings.append(f"重复概念名 {name}（{uri}）已跳过")
            continue
        seen_c[uri] = name
        concept = {"name": name}
        comment = g.value(c, RDFS.comment)
        if comment:
            concept["definition"] = str(comment)
        parents = []
        for p in g.objects(c, RDFS.subClassOf):
            if isinstance(p, URIRef) and str(p) in seen_c:
                parents.append(seen_c[str(p)])
            elif isinstance(p, URIRef) and str(p) not in seen_c:
                # 父类尚未注册（外部类）——先记占位，后续第二轮补
                pass
            else:
                lossy = True
                warnings.append(f"{name} 的匿名父类（限制/公理）丢弃: {p.n3()[:80]}")
        if parents:
            concept["parents"] = parents
        spec["concepts"].append(concept)

    # 第二轮补父类（前向引用）
    by_name = {c["name"]: c for c in spec["concepts"]}
    uri_by_name = {v: k for k, v in seen_c.items()}
    name_by_uri = {v: k for k, v in uri_by_name.items()}
    for c in classes:
        if not isinstance(c, URIRef) or str(c) not in seen_c:
            continue
        mine = by_name.get(seen_c[str(c)])
        if not mine:
            continue
        parents = []
        for p in g.objects(c, RDFS.subClassOf):
            if isinstance(p, URIRef):
                pname = name_by_uri.get(str(p))
                if pname and pname != mine["name"]:
                    parents.append(pname)
        if parents:
            mine["parents"] = parents

    # ---- 对象属性 ----
    seen_r = {}
    props = set(g.subjects(RDF.type, OWL.ObjectProperty))
    for pr in props:
        if not isinstance(pr, URIRef):
            continue
        uri = str(pr)
        if uri.startswith(str(RDF)) or uri.startswith(str(RDFS)) or uri.startswith(str(OWL)):
            continue
        name = label_or_local(g, pr)
        if name in seen_r:
            warnings.append(f"重复关系名 {name} 已跳过")
            continue
        seen_r[uri] = name
        rel = {"name": name}
        comment = g.value(pr, RDFS.comment)
        if comment:
            rel["definition"] = str(comment)
        dom = g.value(pr, RDFS.domain)
        rng = g.value(pr, RDFS.range)
        if dom is not None and str(dom) in seen_c:
            rel["from"] = seen_c[str(dom)]
        else:
            rel["from"] = ""
            lossy = True
            warnings.append(f"关系 {name} 定义域缺失或未注册为概念")
        if rng is not None and str(rng) in seen_c:
            rel["to"] = seen_c[str(rng)]
        else:
            rel["to"] = ""
            lossy = True
            warnings.append(f"关系 {name} 值域缺失或未注册为概念")
        spec["relations"].append(rel)
    # 未标注 range 的 datatype 属性按实例 attributes 处理，不计 relation
    for pr in set(g.subjects(RDF.type, OWL.DatatypeProperty)):
        lossy = True
        warnings.append(f"datatype 属性 {label_or_local(g, pr)} 转为实例 attributes 处理")

    # ---- 实例 ----
    inst_by_uri = {}
    for ind in g.subjects(RDF.type, OWL.NamedIndividual):
        if not isinstance(ind, URIRef):
            continue
        uri = str(ind)
        name = label_or_local(g, ind)
        if name in inst_by_uri:
            warnings.append(f"重复实例名 {name} 已跳过")
            continue
        inst_by_uri[uri] = name
        it = {"name": name, "attributes": {}, "relations": []}
        ctypes = [str(t) for t in g.objects(ind, RDF.type)
                  if isinstance(t, URIRef) and str(t) in seen_c and str(t) != str(OWL.NamedIndividual)]
        if ctypes:
            it["concept"] = seen_c[ctypes[0]]
        else:
            lossy = True
            warnings.append(f"实例 {name} 的类型未注册为概念，concept 置空")
        for p, o in g.predicate_objects(ind):
            pu, ou = str(p), str(o)
            if pu in (str(RDF.type), str(RDFS.label), str(RDFS.comment)):
                continue
            if ou.startswith(str(RDF)) or ou.startswith(str(OWL)):
                continue
            if pu in seen_r:  # 对象属性 → 实例关系
                tgt = inst_by_uri.get(ou) or (str(o) and None)
                if tgt:
                    it["relations"].append({"rel": seen_r[pu], "target": tgt})
                else:
                    lossy = True
                    warnings.append(f"实例 {name} 关系 {seen_r[pu]} 的目标 {local(ou)} 不是已注册实例，丢弃")
            else:  # 其余断言 → attributes
                try:
                    val = o.toPython()
                    val = val.isoformat() if hasattr(val, "isoformat") else val
                except Exception:  # noqa: BLE001
                    val = ou
                it["attributes"][local(pu) or pu] = val
        if not it["attributes"]:
            it.pop("attributes")
        if not it["relations"]:
            it.pop("relations")
        spec["instances"].append(it)

    out = {"spec": spec, "warnings": warnings, "lossy": lossy,
           "note": "OWL/TTL 有损导入：仅保留 类层次/对象属性/实例断言；公理、限制、推理语义丢弃（计入 warnings）"}
    json.dump(out, sys.stdout, ensure_ascii=False)
    return 0


# ---- 导出：spec_json → TTL ----

def esc(s) -> str:
    return str(s).replace("\\", "\\\\").replace('"', '\\"')


def export(argv) -> int:
    oid = ""
    fmt = TTL
    args = argv
    i = 0
    while i < len(args):
        if args[i] == "--ontology-id":
            oid = args[i + 1]; i += 2
        elif args[i] == "--format":
            fmt = args[i + 1]; i += 2
        else:
            i += 1
    spec = json.load(sys.stdin)
    base = f"urn:o:{sanitize(oid)}:"
    cu = lambda n: f"{base}concept:{sanitize(n)}"       # noqa: E731
    ru = lambda n: f"{base}relation:{sanitize(n)}"      # noqa: E731
    iu = lambda n: f"{base}instance:{sanitize(n)}"      # noqa: E731
    au = lambda k: f"{base}attr:{sanitize(k)}"          # noqa: E731

    L = []
    L.append(f'@prefix o: <{base}> .')
    L.append('@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .')
    L.append('@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .')
    L.append('@prefix owl: <http://www.w3.org/2002/07/owl#> .')
    L.append('@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .')
    L.append('')
    for c in spec.get("concepts", []):
        u = cu(c["name"])
        L.append(f'<{u}> rdf:type owl:Class ; rdfs:label "{esc(c["name"])}"')
        if c.get("definition"):
            L[-1] += f' ;\n    rdfs:comment "{esc(c["definition"])}"'
        for p in c.get("parents", []):
            L[-1] += f' ;\n    rdfs:subClassOf <{cu(p)}>'
        L[-1] += ' .'
    for r in spec.get("relations", []):
        u = ru(r["name"])
        L.append(f'<{u}> rdf:type owl:ObjectProperty ; rdfs:label "{esc(r["name"])}"')
        if r.get("definition"):
            L[-1] += f' ;\n    rdfs:comment "{esc(r["definition"])}"'
        if r.get("from"):
            L[-1] += f' ;\n    rdfs:domain <{cu(r["from"])}>'
        if r.get("to"):
            L[-1] += f' ;\n    rdfs:range <{cu(r["to"])}>'
        L[-1] += ' .'
    for it in spec.get("instances", []):
        u = iu(it["name"])
        L.append(f'<{u}> rdf:type owl:NamedIndividual')
        if it.get("concept"):
            L[-1] += f' ;\n    rdf:type <{cu(it["concept"])}>'
        L[-1] += f' ;\n    rdfs:label "{esc(it["name"])}"'
        for k, v in (it.get("attributes") or {}).items():
            L[-1] += f' ;\n    <{au(k)}> "{esc(v)}"'
        for ir in it.get("relations", []):
            L[-1] += f' ;\n    <{ru(ir["rel"])}> <{iu(ir["target"])}>'
        L[-1] += ' .'

    ttl = "\n".join(L) + "\n"
    # rdflib 回读校验（导出即验证）
    try:
        g = Graph()
        g.parse(data=ttl, format=TTL)
    except Exception as e:  # noqa: BLE001
        sys.stderr.write(f"导出 TTL 校验失败: {e}\n")
        return 1
    sys.stdout.write(ttl)
    return 0


def main() -> int:
    if len(sys.argv) < 2:
        sys.stderr.write(__doc__)
        return 2
    cmd = sys.argv[1]
    if cmd == "parse":
        fmt = "turtle"
        if "--format" in sys.argv:
            fmt = sys.argv[sys.argv.index("--format") + 1]
        return parse(fmt)
    if cmd == "export":
        return export(sys.argv[2:])
    sys.stderr.write(f"未知命令 {cmd}\n")
    return 2


if __name__ == "__main__":
    sys.exit(main())
