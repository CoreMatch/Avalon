package command

import (
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/util/isutils"
)

// playerPosition 对应 TabCompleterProcess.playerPosition(nameOrPosition, strCons)
//
// 上游查找顺序: 先按名字精确匹配 playerAll, 未命中再按数字位置查
// (位置 = 数字 - 1, 即 getPlayer); 非数字或位置不存在时经 print 报告.
func playerPosition(rm *room.ServerRoom, nameOrPosition string, print func(string)) *player.Player {
	if rm == nil {
		return nil
	}

	if result, ok := rm.PlayerManage.PlayerAll.Find(func(p *player.Player) bool {
		return p.Name() == nameOrPosition
	}); ok {
		return result
	}

	if isutils.NotIsNumeric(nameOrPosition) {
		print(tr("err.noNumber"))
		return nil
	}

	position := atoiOr(nameOrPosition, 0)
	result := rm.PlayerManage.GetPlayer(position - 1)
	if result == nil {
		print(tr("err.player.no.site", position))
		return nil
	}
	return result
}
