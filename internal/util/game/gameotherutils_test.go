package game

import "testing"

// TestGetBetaVersionMatchesOriginalRange 固定原版 `version in 152 .. 175` 的区间.
//
// 边界值得单独钉住: 151 不是 beta, 152 是; 175 是, 176 不是.
func TestGetBetaVersionMatchesOriginalRange(t *testing.T) {
	cases := map[int]bool{
		0:    false,
		151:  false,
		152:  true,
		153:  true,
		174:  true,
		175:  true,
		176:  false,
		177:  false,
		9999: false,
	}
	for version, want := range cases {
		if got := GetBetaVersion(version); got != want {
			t.Errorf("GetBetaVersion(%d) = %v, want %v", version, got, want)
		}
	}
}
