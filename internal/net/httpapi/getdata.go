package httpapi

import (
	"runtime"
	"sync"

	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/util/systemutils"
)

// gameOverCache 对应 GetData.GameOverPositive.data (SeqSave(10)):
// 定长缓存, 满时移除最旧一条 (照搬 SeqSave.addSeq 语义).
type gameOverCache struct {
	mu   sync.Mutex
	data []string
	max  int
}

func newGameOverCache(max int) *gameOverCache {
	return &gameOverCache{max: max}
}

// add 对应 SeqSave.addSeq
func (c *gameOverCache) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) >= c.max {
		c.data = c.data[1:]
	}
	c.data = append(c.data, s)
}

// snapshotAndClear 取走全部缓存并清空 (对应上游 event/GameOver 路由的
// toList() + clear() 两步)
func (c *gameOverCache) snapshotAndClear() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.data))
	copy(out, c.data)
	c.data = c.data[:0]
	return out
}

// size 当前缓存条数 (测试用)
func (c *gameOverCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.data)
}

// DefaultGameOverCache 对应 GetData.GameOverPositive.data 单例 (容量 10)
var DefaultGameOverCache = newGameOverCache(10)

// SystemInfo 对应 GetData.SystemInfo
//
// 上游字段读 JVM 属性 (os.name/os.arch/java.name/java.version); Go 版无 JVM,
// system/arch 用 systemutils 的 Java 风格映射, jvmName/jvmVersion 以 Go 运行时
// (runtime.Compiler / runtime.Version()) 为等价物, 字段名保留以维持 API 形状.
type SystemInfo struct {
	System     string `json:"system"`
	Arch       string `json:"arch"`
	JvmName    string `json:"jvmName"`
	JvmVersion string `json:"jvmVersion"`
}

// SystemInfoSnapshot 对应 GetData.SystemInfo() 的构造时取值
func SystemInfoSnapshot() SystemInfo {
	return SystemInfo{
		System:     systemutils.OSName(),
		Arch:       systemutils.OSArch(),
		JvmName:    runtime.Compiler,
		JvmVersion: runtime.Version(),
	}
}

// GameInfo 对应 GetData.GameInfo (部分): 房间状态与玩家列表.
//
// 上游还有 income/noNukes/credits/sharedControl 四个字段, 来自
// HeadlessModuleManage.hps.gameLinkServerData (JVM 内核侧数据); Go 版内核在
// 外部 helper 内, 协议尚无这些字段 (DECISIONS D-9), 不伪造数据, 暂不包含.
type GameInfo struct {
	StartGame bool     `json:"startGame"`
	Players   []string `json:"players"`
}

// GameInfoSnapshot 对应 GetData.GameInfo() 的构造时取值
func GameInfoSnapshot() GameInfo {
	info := GameInfo{Players: []string{}}
	r := room.Default()
	if r == nil {
		return info
	}
	info.StartGame = r.IsStartGame
	r.PlayerManage.PlayerAll.EachAll(func(p *player.Player) {
		info.Players = append(info.Players, p.PlayerInfo())
	})
	return info
}
