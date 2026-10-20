// Package temp 对应 net.rwhps.server.data.temp
package temp

import "rwhps/internal/data/totalizer"

// ConnectReceiveData 对应 ConnectReceiveData
type ConnectReceiveData struct {
	ReceiveBigPacket      bool
	ReceiveBigPacketCount *totalizer.TimeAndNumber
	InputPassword         bool
}

// NewConnectReceiveData 对应 ConnectReceiveData()
func NewConnectReceiveData() *ConnectReceiveData {
	return &ConnectReceiveData{
		ReceiveBigPacketCount: totalizer.NewTimeAndNumber(4, 1),
	}
}
