// Package temp 对应 上游 data.temp
package temp

import "github.com/CoreMatch/Avalon/internal/data/totalizer"

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
