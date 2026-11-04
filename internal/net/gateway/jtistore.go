package gateway

import (
	"errors"
	"sync"
	"time"

	"rwhps/internal/util/file/plugindata"
)

// ErrJtiReplayed 令牌 ID 已被消费过 (契约 :84 "A token ID can only be
// consumed once"; 错误码 SESSION_IDENTITY_REPLAYED)
var ErrJtiReplayed = errors.New("gateway: token ID already consumed")

// pruneGrace 已过期的 jti 记录保留宽限: JWT 寿命上限 30 秒, 留足宽限
// 防止时钟漂移下的误判, 之后连记录一起清理.
const pruneGrace = 5 * 60

// JtiStore 是 jti 一次性消费的持久化账本 (DECISIONS D-19).
//
// 复用既有 plugindata Bin 存储 (Settings.bin 同款格式), 独立文件:
// key = jti, value = 令牌 exp (KindLong). 后端重启不丢, 满足契约
// "一次性消费"的跨重启语义; 条目随过期清理, 文件不会无限增长.
type JtiStore struct {
	mu    sync.Mutex
	items map[string]int64
	store *plugindata.Settings
}

// NewJtiStore 打开 (或创建) 账本文件并载入既有条目
func NewJtiStore(store *plugindata.Settings) *JtiStore {
	s := &JtiStore{
		items: make(map[string]int64),
		store: store,
	}
	now := time.Now().Unix()
	for _, key := range store.Keys() {
		if v, ok := store.Get(key); ok && v.Kind == plugindata.KindLong {
			if v.Long+pruneGrace >= now {
				s.items[key] = v.Long
			} else {
				store.Remove(key)
			}
		}
	}
	_ = store.Save()
	return s
}

// Consume 消费一个 jti: 已存在返回 ErrJtiReplayed, 否则记账并落盘.
//
// 落盘失败不阻断绑定: 内存账本在进程存活期内已保证一次性语义,
// 只有"落盘失败 + 立刻重启 + 令牌 30 秒寿命内重放"的极端窗口会漏,
// 由调用方记日志警示.
func (s *JtiStore) Consume(jti string, expUnix int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[jti]; exists {
		return ErrJtiReplayed
	}
	s.items[jti] = expUnix
	s.store.SetLong(jti, expUnix)
	return s.store.Save()
}

// Size 当前账本条目数 (测试与诊断用)
func (s *JtiStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
