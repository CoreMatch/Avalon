package core

import "testing"

func TestRelayStatusString(t *testing.T) {
	cases := map[RelayStatus]string{
		RelayInitialConnection:    "InitialConnection",
		RelayGetPlayerInfo:        "GetPlayerInfo",
		RelayWaitCertified:        "WaitCertified",
		RelayCertifiedEnd:         "CertifiedEnd",
		RelayPlayerPermission:     "PlayerPermission",
		RelayPlayerJoinPermission: "PlayerJoinPermission",
		RelayHostPermission:       "HostPermission",
		RelayDebug:                "Debug",
	}
	for status, want := range cases {
		if got := status.String(); got != want {
			t.Errorf("RelayStatus(%d).String() = %q, 期望 %q", status, got, want)
		}
	}

	// 顺序必须与原版枚举声明顺序一致, 认证流程按序号推进
	if RelayHostPermission != 6 {
		t.Errorf("RelayHostPermission = %d, 期望 6", RelayHostPermission)
	}
}

func TestServerStatusString(t *testing.T) {
	if got := ServerInitialConnection.String(); got != "InitialConnection" {
		t.Errorf("ServerInitialConnection = %q", got)
	}
	if got := ServerCertifiedEnd.String(); got != "CertifiedEnd" {
		t.Errorf("ServerCertifiedEnd = %q", got)
	}
}

func TestDevConnectStatusString(t *testing.T) {
	cases := map[DevConnectStatus]string{
		DevInitialConnection: "InitialConnection",
		DevWaitCertified:     "WaitCertified",
		DevCertifiedEnd:      "CertifiedEnd",
		DevMaster:            "Master",
	}
	for status, want := range cases {
		if got := status.String(); got != want {
			t.Errorf("DevConnectStatus(%d).String() = %q, 期望 %q", status, got, want)
		}
	}
}
