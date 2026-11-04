package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"rwhps/internal/util/log"
)

// 契约 "Errors and retries" (Cascade-Backend-v1.md :137-164) 的稳定错误码.
// 应答形状固定为 {"code","message"} (openapi BackendError).
const (
	CodeInvalidRequest          = "INVALID_REQUEST"
	CodeSessionNotFound         = "SESSION_NOT_FOUND"
	CodeSessionExpired          = "SESSION_EXPIRED"
	CodeSessionIdentityInvalid  = "SESSION_IDENTITY_INVALID"
	CodeSessionIdentityReplayed = "SESSION_IDENTITY_REPLAYED"
	CodeSessionIdentityConflict = "SESSION_IDENTITY_CONFLICT"
	CodeRoomNotFound            = "ROOM_NOT_FOUND"
	CodeRoomConflict            = "ROOM_CONFLICT"
	CodeBackendNotReady         = "BACKEND_NOT_READY"
	CodeInternalError           = "INTERNAL_ERROR"
	// CodeForbidden 信任档位拒绝 (来源 IP / 客户端证书指纹不匹配).
	// 有意修正 (作者已批准, 2026-11-04; DECISIONS D-24): 契约错误码表
	// 未含信任拒绝, 新增此码, 待契约升版收录.
	CodeForbidden = "FORBIDDEN"
)

// ServeControl 启动 /v1 控制面. 传输随信任档位: 配置了服务端证书走 TLS,
// 否则明文 (DECISIONS D-24). 阻塞直到 Stop/出错.
func (g *Gateway) ServeControl() error {
	ln, err := net.Listen("tcp", g.cfg.ControlAddr)
	if err != nil {
		return err
	}
	if g.tlsCfg != nil {
		log.Clog("[Cascade] control API listening on {0} (TLS)", g.cfg.ControlAddr)
	} else {
		log.Clog("[Cascade] control API listening on {0} (明文)", g.cfg.ControlAddr)
	}
	return g.ServeControlOn(ln)
}

// ServeControlOn 在既有监听器上服务控制面 (测试注入 :0 监听器用).
// 阻塞直到监听器关闭/出错.
func (g *Gateway) ServeControlOn(ln net.Listener) error {
	g.controlMu.Lock()
	g.controlLn = ln
	g.controlMu.Unlock()

	if g.tlsCfg != nil {
		ln = newTLSListener(ln, g.tlsCfg)
	}

	srv := &http.Server{
		Handler:           g.trustMiddleware(g.controlRouter()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// trustMiddleware 把连接层信任档位套到控制面 (DECISIONS D-24):
// ip 档查来源白名单; pinned 档再查 TLS 客户端证书指纹.
// open 档不拦截 (裸奔). 被拒一律 403 + 契约错误形状.
func (g *Gateway) trustMiddleware(next http.Handler) http.Handler {
	if g.cfg.TrustMode == TrustOpen {
		return next
	}
	ipCheck := g.cfg.TrustMode == TrustIP || !g.cfg.TrustedIPs.empty()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ipCheck && !g.cfg.TrustedIPs.contains(remoteIP(r.RemoteAddr)) {
			log.Warnf("[Cascade]", "control request from {0} rejected: source IP not trusted (trust={1})", r.RemoteAddr, g.cfg.TrustMode)
			writeErr(w, http.StatusForbidden, CodeForbidden, "source IP is not trusted")
			return
		}
		if g.cfg.TrustMode == TrustPinned {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 ||
				!fingerprintMatches(r.TLS.PeerCertificates[0].Raw, g.cfg.PinnedFingerprints) {
				log.Warnf("[Cascade]", "control request from {0} rejected: client certificate fingerprint not pinned", r.RemoteAddr)
				writeErr(w, http.StatusForbidden, CodeForbidden, "client certificate fingerprint is not pinned")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// controlRouter 契约 HTTP API (Cascade-Backend-v1.md :86-101,
// openapi cascade-backend-v1.yaml). 基路径 /v1.
func (g *Gateway) controlRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", g.handleHealth)
	mux.HandleFunc("GET /v1/rooms", g.handleRooms)
	mux.HandleFunc("PUT /v1/sessions/{sessionID}/identity", g.handleIdentity)
	mux.HandleFunc("POST /v1/rooms/create", g.handleCreateRoom)
	mux.HandleFunc("GET /v1/rooms/{code}", g.handleGetRoom)
	mux.HandleFunc("DELETE /v1/rooms/{code}", g.handleDeleteRoom)
	mux.HandleFunc("PUT /v1/rooms/{code}/settings", g.handleSettings)
	mux.HandleFunc("POST /v1/rooms/{code}/kick", g.handleKick)
	mux.HandleFunc("POST /v1/rooms/{code}/ban", g.handleBan)
	mux.HandleFunc("GET /v1/rooms/{code}/players", g.handlePlayers)
	return mux
}

// --- 核心三端点: health / rooms / sessions identity ---

func (g *Gateway) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// ready 判据 (DECISIONS D-21, 2026-11-04 修订): 游戏监听已建立;
	// 配置了 helper 时还须 helper 活着 (HelperReady 探针), 未配置维持现状.
	if !g.Ready() {
		writeErr(w, http.StatusServiceUnavailable, CodeBackendNotReady, "game listener is not ready")
		return
	}
	if g.cfg.HelperReady != nil && !g.cfg.HelperReady() {
		writeErr(w, http.StatusServiceUnavailable, CodeBackendNotReady, "external game helper is not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"backendId": g.cfg.BackendID,
		"status":    "ready",
	})
}

func (g *Gateway) handleRooms(w http.ResponseWriter, _ *http.Request) {
	summary, err := g.roomSummary(false)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, CodeBackendNotReady, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"backendId":   g.cfg.BackendID,
		"generatedAt": time.Now().UTC(),
		"rooms":       []*RoomSummary{summary},
	})
}

// identityRequest PUT /v1/sessions/{sessionID}/identity 请求体
type identityRequest struct {
	Token string `json:"token"`
}

// handleIdentity 契约 "Session state machine" 的绑定点 (:45-59, :99-101):
// 校验 JWT -> 会话存在性 -> jti 一次性消费 -> PENDING 迁移 BOUND.
//
// 有意修正（作者已批准，2026-11-04）: 任务文案"扣住首包"按契约落地为
// "160 放行、110 起门控" (DECISIONS D-18); 本端点成功返回前, 该会话的
// 注册与游戏流量一直被 Session.AllowPacket 拦在引擎之外.
func (g *Gateway) handleIdentity(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	log.Debugf("[Cascade]", "identity bind request for session {0}", sessionID)

	var req identityRequest
	if err := decodeBody(w, r, &req); err != nil {
		log.Warnf("[Cascade]", "identity for {0}: bad body: {1}", sessionID, err)
		return
	}
	if req.Token == "" {
		writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "token is required")
		return
	}

	claims, err := g.verifier.Verify(req.Token, time.Now())
	if err != nil {
		log.Warnf("[Cascade]", "identity for {0}: token rejected: {1}", sessionID, err)
		writeErr(w, http.StatusBadRequest, CodeSessionIdentityInvalid, err.Error())
		return
	}
	if claims.Sid != sessionID {
		log.Warnf("[Cascade]", "identity for {0}: token sid mismatch {1}", sessionID, claims.Sid)
		writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "token sid does not match the session path")
		return
	}

	s := g.registry.Get(sessionID)
	if s == nil {
		log.Warnf("[Cascade]", "identity for {0}: session not found", sessionID)
		writeErr(w, http.StatusNotFound, CodeSessionNotFound, "session is not pending")
		return
	}

	switch s.State() {
	case StateBound:
		// 幂等重试: 完全相同的身份 + jti 接受 (契约 :99-101)
		if s.Matches(claims.Sub, claims.Jti) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Warnf("[Cascade]", "identity for {0}: conflict (already bound)", sessionID)
		writeErr(w, http.StatusConflict, CodeSessionIdentityConflict, "session already bound to a different identity")
		return
	case StateClosed:
		log.Warnf("[Cascade]", "identity for {0}: session closed", sessionID)
		writeErr(w, http.StatusGone, CodeSessionExpired, "session is closed")
		return
	}

	// PENDING: 先消费 jti (一次性, 持久化), 再迁移状态
	if err := g.jti.Consume(claims.Jti, claims.Exp); err != nil {
		if errors.Is(err, ErrJtiReplayed) {
			log.Warnf("[Cascade]", "identity for {0}: jti replayed", sessionID)
			writeErr(w, http.StatusBadRequest, CodeSessionIdentityReplayed, "token ID already consumed")
			return
		}
		// 落盘失败不阻断 (内存账本仍保证进程内一次性, 见 JtiStore.Consume)
		log.Warnf("[Cascade]", "jti store save failed: {0}", err)
	}
	if err := s.bind(claims.Sub, claims.Jti); err != nil {
		log.Warnf("[Cascade]", "identity for {0}: bind failed: {1}", sessionID, err)
		if errors.Is(err, ErrSessionConflict) {
			writeErr(w, http.StatusConflict, CodeSessionIdentityConflict, err.Error())
			return
		}
		writeErr(w, http.StatusGone, CodeSessionExpired, err.Error())
		return
	}

	log.Clog("[Cascade] session {0} bound to player {1}", sessionID, claims.Sub)
	w.WriteHeader(http.StatusNoContent)
}

// --- 房间门面 (最小可用, DECISIONS D-20) ---

// roomCreateRequest 对应 Cascade api.roomCreateRequest
type roomCreateRequest struct {
	Code       string `json:"code"`
	Title      string `json:"title"`
	MaxPlayers int    `json:"maxPlayers"`
}

func (g *Gateway) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req roomCreateRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	summary, err := g.createRoom(req.Code, req.Title, req.MaxPlayers)
	if err != nil {
		if errors.Is(err, ErrRoomConflict) {
			writeErr(w, http.StatusConflict, CodeRoomConflict, "single-room backend already owns a different room code")
			return
		}
		writeErr(w, http.StatusServiceUnavailable, CodeBackendNotReady, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

func (g *Gateway) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("code") != g.cfg.RoomCode {
		writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
		return
	}
	summary, err := g.roomSummary(true)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, CodeBackendNotReady, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (g *Gateway) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("code") != g.cfg.RoomCode {
		writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
		return
	}
	// 单房间后端的唯一房间不可删 (DECISIONS D-20): 房间即服务器,
	// 上游亦无"删房间"语义.
	writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "single-room backend cannot delete its only room")
}

func (g *Gateway) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("code") != g.cfg.RoomCode {
		writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
		return
	}
	var req RoomSettings
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	summary, err := g.applySettings(&req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// playerActionRequest 对应 Cascade api.playerActionRequest
type playerActionRequest struct {
	PlayerID string `json:"playerId"`
}

func (g *Gateway) handleKick(w http.ResponseWriter, r *http.Request) {
	g.playerAction(w, r, g.kickPlayer)
}

func (g *Gateway) handleBan(w http.ResponseWriter, r *http.Request) {
	g.playerAction(w, r, g.banPlayer)
}

func (g *Gateway) playerAction(w http.ResponseWriter, r *http.Request, action func(code, playerID string) error) {
	var req playerActionRequest
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.PlayerID == "" {
		writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "playerId is required")
		return
	}
	if err := action(r.PathValue("code"), req.PlayerID); err != nil {
		if errors.Is(err, ErrRoomNotFound) {
			writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
			return
		}
		if errors.Is(err, ErrPlayerNotFound) {
			writeErr(w, http.StatusNotFound, CodeRoomNotFound, "player is not in the room")
			return
		}
		writeErr(w, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) handlePlayers(w http.ResponseWriter, r *http.Request) {
	players, err := g.listPlayers(r.PathValue("code"))
	if err != nil {
		if errors.Is(err, ErrRoomNotFound) {
			writeErr(w, http.StatusNotFound, CodeRoomNotFound, "room is not owned by this backend")
			return
		}
		writeErr(w, http.StatusServiceUnavailable, CodeBackendNotReady, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"players": players})
}

// --- 工具 ---

// decodeBody 读取不超过 1MB 的 JSON 请求体
func decodeBody(w http.ResponseWriter, r *http.Request, out any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "unreadable body")
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		writeErr(w, http.StatusBadRequest, CodeInvalidRequest, "invalid JSON body")
		return err
	}
	return nil
}

// writeJSON 契约要求的 application/json 应答
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

// writeErr 契约错误形状 {"code","message"}
func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
