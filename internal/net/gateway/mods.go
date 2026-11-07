package gateway

import (
	"net/http"
	"strconv"

	"github.com/CoreMatch/Avalon/internal/game/manage"
)

// 契约 v1.3 "Room mod manifest" (Cascade-Backend-v1.md, openapi
// cascade-backend-v1.yaml): 只读模组清单与模组文件端点, 供客户端模组
// 装配 (RW-Mod-Injector) 在连接房间前备齐所需模组. 两端点面向玩家、
// 只读、不回显敏感字段 (与 /rooms 房间列表同公开级别); 信任档位中间件
// 包住整个 /v1 面, 本文件不做额外信任逻辑.

// handleRoomMods GET /v1/rooms/{code}/mods —— 房间模组清单.
// 零模组房间返回空清单 (契约合法形态).
func (g *Gateway) handleRoomMods(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("code") != g.cfg.RoomCode {
		writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
		return
	}
	manifest, err := manage.BuildModManifest()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, CodeInternalError, "mod manifest is not available")
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

// handleRoomModFile GET /v1/rooms/{code}/mods/{file} —— 模组文件字节.
// 应答体必须与清单声明的 sha256/size 严格一致 (同一份缓存产物, 天然自洽).
//
// 错误映射 (契约 v1.3): 白名单外文件名 (含裸 "."/"..") -> 400
// INVALID_REQUEST; 房间码或清单未收录的文件 -> 404 ROOM_NOT_FOUND.
func (g *Gateway) handleRoomModFile(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("code") != g.cfg.RoomCode {
		writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
		return
	}
	fileName := r.PathValue("file")
	if !manage.ValidModFileName(fileName) {
		writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "file name is outside the mod file whitelist")
		return
	}
	data, ok, err := manage.ModArtifact(fileName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, CodeInternalError, "mod manifest is not available")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, CodeRoomNotFound, "mod file is not in this room's manifest")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
