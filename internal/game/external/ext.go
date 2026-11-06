package external

import (
	"rwhps/internal/game"
	"rwhps/internal/game/event"
)

// 本文件实现 helper 协议 v2-ext 的四个扩展操作 (DECISIONS D-9).
//
// 有意修正（作者已批准，2026-11-04）: 上游这些能力直接调用进程内的引擎对象,
// Go 版引擎在 helper 内, 经扩展操作完成. 全部走既有的"请求-应答"通路与世代号
// 规则, 不改动 v2 已有操作.
//
// 降级约定: 尚未创建房间时返回 ErrNoRoom; helper 只支持 v2 时, 应答里的
// "unsupported" 被 errorf 归一成 game.ErrUnsupported, 由上层降级为空操作.

// 编译期确认: 房间层按 game.RoomControl 断言 Backend.
var _ game.RoomControl = (*Backend)(nil)

// AddAI 实现 game.RoomControl, 对应 gameScriptMultiPlayer.addAi()
//
// 有意修正（作者已批准，2026-11-04）: 引擎不会为 AI 触发 PlayerJoinEvent
// (上游该事件只由真实连接的 PlayerConnectX 发出), 档案改为随 room.addai
// 应答的 ai 字段回传 (DECISIONS D-9). 返回 nil 档案 + nil 错误表示引擎
// 没有创建 AI (无空位), 对应上游 addAI 扫描不到空位时的静默返回.
func (b *Backend) AddAI() (*game.AIProfile, error) {
	room := b.currentRoom()
	if room == "" {
		return nil, ErrNoRoom
	}
	resp, err := b.Request(&Request{Op: OpRoomAddAI, Room: room})
	if err != nil {
		return nil, err
	}
	if resp.AI == nil {
		return nil, nil
	}
	ai := &game.AIProfile{
		ID:   resp.AI.ID,
		Name: resp.AI.Name,
		Team: resp.AI.Team,
	}
	if resp.AI.Position != nil {
		ai.Position = *resp.AI.Position
	}
	return ai, nil
}

// SyncAllPlayer 实现 game.RoomControl, 对应 gameLinkFunction.allPlayerSync()
func (b *Backend) SyncAllPlayer() error {
	room := b.currentRoom()
	if room == "" {
		return ErrNoRoom
	}
	_, err := b.Request(&Request{Op: OpRoomSync, Room: room})
	return err
}

// RemovePlayer 实现 game.RoomControl, 对应引擎侧玩家移除
// (AiPlayer.kickPlayer 里的 playerPrivateData.removePlayer())
func (b *Backend) RemovePlayer(position int) error {
	room := b.currentRoom()
	if room == "" {
		return ErrNoRoom
	}
	pos := position
	_, err := b.Request(&Request{Op: OpRoomRemovePlayer, Room: room, Position: &pos})
	return err
}

// GameResult 实现 game.RoomControl, 对应 gameHessData.getGameOverData()
//
// 返回值语义与上游对齐:
//
//	ok=false, err=nil  -> 尚未分出胜负 (上游返回 null)
//	ok=true            -> 结算数据就绪
//	err!=nil           -> 查询失败 (含 game.ErrUnsupported: helper 只支持 v2)
func (b *Backend) GameResult() (*event.GameOverData, bool, error) {
	room := b.currentRoom()
	if room == "" {
		return nil, false, ErrNoRoom
	}
	resp, err := b.Request(&Request{Op: OpGameResult, Room: room})
	if err != nil {
		return nil, false, err
	}
	if resp.Result == nil {
		return nil, false, nil
	}
	return toGameOverData(resp.Result), true, nil
}

// ApplySettings 实现 game.RoomControl, 对应 v2-ext room.settings:
// 把引擎侧房间设置写进 helper 内的 gameLinkServerData (DECISIONS D-20).
//
// 全部字段都未提供时不发请求 (没有可写的字段).
func (b *Backend) ApplySettings(s game.RoomSettings) error {
	room := b.currentRoom()
	if room == "" {
		return ErrNoRoom
	}
	payload := toSettingsPayload(s)
	if payload == nil {
		return nil
	}
	_, err := b.Request(&Request{Op: OpRoomSettings, Room: room, Settings: payload})
	return err
}

// toSettingsPayload 把接缝层设置转成线上负载; 无任何字段时返回 nil
func toSettingsPayload(s game.RoomSettings) *SettingsPayload {
	if s.Credits == nil && s.Fog == nil && s.Income == nil && s.DisableNuke == nil &&
		s.SharedControl == nil && s.StartingUnits == nil && s.MaxUnits == nil {
		return nil
	}
	return &SettingsPayload{
		Credits:       s.Credits,
		Fog:           s.Fog,
		Income:        s.Income,
		DisableNuke:   s.DisableNuke,
		SharedControl: s.SharedControl,
		StartingUnits: s.StartingUnits,
		MaxUnits:      s.MaxUnits,
	}
}

// toGameOverData 把线上结构转成事件层的 GameOverData
//
// 线上结构 (GameResult) 带 JSON 标签, 事件层结构不带 —— 协议格式变化时
// 只改本文件, 不波及事件消费者.
func toGameOverData(r *GameResult) *event.GameOverData {
	return &event.GameOverData{
		GameTime:      r.GameTime,
		AllPlayerList: r.AllPlayerList,
		WinPlayerList: r.WinPlayerList,
		MapName:       r.MapName,
		PlayerData:    r.PlayerData,
		ReplayName:    r.ReplayName,
	}
}
