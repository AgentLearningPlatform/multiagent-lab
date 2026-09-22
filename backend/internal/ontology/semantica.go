package ontology

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// semanticaPrefix 主平台侧统一入口前缀（server.go 挂载 /api/semantica 与 /api/semantica/）。
const semanticaPrefix = "/api/semantica"

// SemanticaProxy Semantica worker 反代：/api/semantica/* → SEMANTICA_WORKER_URL/*（§4.9 D-O10）。
//
// 与 BuildProxy/RuntimeProxy 的「原样透传」不同：semantica worker 的 REST 路径不带 /api/semantica
// 前缀，故在 Director 中剥离该前缀，使 /api/semantica/health → :8093/health、
// /api/semantica/ingest-ttl → :8093/ingest-ttl。超时与 502 语义对齐 ontology.go 其余反代。
func (s *Service) SemanticaProxy() http.Handler {
	tu, err := url.Parse(s.SemanticaURL)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeErrJSON(w, http.StatusBadGateway, "semantica worker 地址配置无效: "+s.SemanticaURL)
		})
	}
	rp := httputil.NewSingleHostReverseProxy(tu)
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
		origDirector(req)
		// 剥离前缀；空路径（/api/semantica 或 /api/semantica/）归一到上游根 "/"。
		p := strings.TrimPrefix(req.URL.Path, semanticaPrefix)
		if p == "" {
			p = "/"
		}
		req.URL.Path = p
		req.URL.RawPath = "" // 前缀变更后原 RawPath 不再匹配，交由 Path 重新编码
	}
	origErr := rp.ErrorHandler
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, perr error) {
		if origErr != nil {
			origErr(w, r, perr)
			return
		}
		writeErrJSON(w, http.StatusBadGateway, "semantica worker 不可达: "+s.SemanticaURL+"（"+perr.Error()+"）")
	}
	// 拨号超时对齐 ONTOLOGY_DIAL_TIMEOUT（§9），与其余本体反代一致（3s）。
	rp.Transport = &http.Transport{ResponseHeaderTimeout: s.DialTimeout}
	return rp
}

// semanticaExplorerPrefix 主平台侧 Explorer 入口（前端 iframe/src 路径，§4.9.2）。
const semanticaExplorerPrefix = "/semantica/explorer"

// SemanticaExplorerProxy Explorer 反代：/semantica/explorer/* → SEMANTICA_WORKER_URL/explorer/*（§4.9.2/§4.9.4）。
//
// 前端以 /semantica/explorer/ 作为 iframe src；worker 侧 Explorer ASGI 挂载于 /explorer。
// Director 把 /semantica/explorer 前缀改写为 /explorer：
//   /semantica/explorer        → /explorer
//   /semantica/explorer/       → /explorer/
//   /semantica/explorer/x.js   → /explorer/x.js
// Explorer 会设置 X-Frame-Options 阻止 iframe 嵌入，这里在 ModifyResponse 中剥离（缺失时无副作用）。
func (s *Service) SemanticaExplorerProxy() http.Handler {
	tu, err := url.Parse(s.SemanticaURL)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeErrJSON(w, http.StatusBadGateway, "semantica worker 地址配置无效: "+s.SemanticaURL)
		})
	}
	rp := httputil.NewSingleHostReverseProxy(tu)
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
		origDirector(req)
		// 前缀重写：/semantica/explorer → /explorer（保留其余子路径与查询串）。
		req.URL.Path = "/explorer" + strings.TrimPrefix(req.URL.Path, semanticaExplorerPrefix)
		req.URL.RawPath = ""
	}
	origModify := rp.ModifyResponse
	rp.ModifyResponse = func(resp *http.Response) error {
		if origModify != nil {
			if err := origModify(resp); err != nil {
				return err
			}
		}
		// 允许主平台 iframe 嵌入 Explorer（Header.Del 大小写不敏感，缺失为 no-op）。
		resp.Header.Del("X-Frame-Options")
		return nil
	}
	origErr := rp.ErrorHandler
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, perr error) {
		if origErr != nil {
			origErr(w, r, perr)
			return
		}
		writeErrJSON(w, http.StatusBadGateway, "semantica explorer 不可达: "+s.SemanticaURL+"（"+perr.Error()+"）")
	}
	rp.Transport = &http.Transport{ResponseHeaderTimeout: s.DialTimeout}
	return rp
}
