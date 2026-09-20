package api

import (
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// GET /api/skills：技能列表（P1，方案 §6.12）。
func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := s.Store.ListSkills()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skills)
}

// POST /api/skills：新建技能。
func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var sk store.Skill
	if err := decodeJSON(r, &sk); err != nil {
		writeErr(w, err)
		return
	}
	sk.Enabled = true // 新建即启用；停用走 PUT（请求体 enabled 字段忽略）
	created, err := s.Store.CreateSkill(&sk)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// GET /api/skills/{id}：技能详情。
func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.Store.GetSkill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

// PUT /api/skills/{id}：更新技能（builtin 只可停用与改内容，不可改名隐藏？——
// 保持简单：builtin 允许编辑与停用，不允许删除；name 唯一性由 store 保证）。
func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	var sk store.Skill
	if err := decodeJSON(r, &sk); err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	old, err := s.Store.GetSkill(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	sk.ID = id
	sk.Builtin = old.Builtin // builtin 标记不可篡改
	updated, err := s.Store.UpdateSkill(&sk)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DELETE /api/skills/{id}：删除技能（内置技能禁止删除，返回 409 提示停用）。
func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, err := s.Store.GetSkill(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if old.Builtin {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "内置技能不可删除，可停用"})
		return
	}
	if err := s.Store.DeleteSkill(id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/skills/{id}/preview：注入预览——该技能注入后的指令块与工具白名单（§6.12 LG-16）。
func (s *Server) previewSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.Store.GetSkill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	block := ""
	if sk.Enabled {
		block = "<skill name=\"" + sk.Name + "\">" + sk.Instruction + "</skill>"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                sk.ID,
		"name":              sk.Name,
		"enabled":           sk.Enabled,
		"instruction_block": block,
		"tools":             sk.Tools,
	})
}
