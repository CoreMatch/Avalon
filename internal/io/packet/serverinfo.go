package packet

// ServerInfoPacket 对应 ServerInfoPacket
// 解析逻辑位于 gameio.NewServerInfoPacket (因 Go 不允许 io -> packet -> io 的循环依赖)
type ServerInfoPacket struct {
	MapName     string
	Credits     int
	Fog         int
	RevealedMap bool
	AIDifficuly int

	InitUnit int
	Income   float32
	Nukes    bool
}
