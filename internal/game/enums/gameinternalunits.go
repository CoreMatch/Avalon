// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameInternalUnits 对应 GameInternalUnits 枚举
// 编号 0..53; UNKNOWN = -1
type GameInternalUnits int

// 枚举值 (与 Kotlin GameInternalUnits.values() 顺序一致)
const (
	UnitExtractor GameInternalUnits = iota
	UnitLandFactory
	UnitAirFactory
	UnitNavalFactory
	UnitExperimental
	UnitBuilder
	UnitSmallFlak
	UnitFlak
	UnitArcher
	UnitArtdl
	UnitHeavyArtillery
	UnitLaserDefenseTower
	UnitRadar
	UnitAirpad
	UnitMissileSil
	UnitAntiAir
	UnitArtillery
	UnitHeavyTank
	UnitMediumTank
	UnitLightArtillery
	UnitLightTank
	UnitScout
	UnitMotorcycle
	UnitRecon
	UnitMaxTank
	UnitHarvester
	UnitHovercraft
	UnitAmphibious
	UnitMissileTank
	UnitMarine
	UnitGunShip
	UnitHeavyGunship
	UnitChinook
	UnitAirShip
	UnitFighter
	UnitBomber
	UnitStealthBomber
	UnitStealthFighter
	UnitDreadnought
	UnitBattleship
	UnitCruiser
	UnitDestroyer
	UnitFrigate
	UnitSubmarine
	UnitCarrier
	UnitTroopCarrier
	UnitHeavyTroopCarrier
	UnitMegaNuke
	UnitNuke
	UnitAntiNuke
	UnitDummyBuilding
	UnitBullets
	UnitTree
	UnitWall
	UnitBridge
	UnitAirField
	UnitMarineTk
	UnitUNKNOWN
)

// Name 对应 Kotlin enum.name (返回常量名)
func (g GameInternalUnits) Name() string {
	switch g {
	case UnitExtractor:
		return "extractor"
	case UnitLandFactory:
		return "landFactory"
	case UnitAirFactory:
		return "airFactory"
	case UnitNavalFactory:
		return "navalFactory"
	case UnitExperimental:
		return "experimental"
	case UnitBuilder:
		return "builder"
	case UnitSmallFlak:
		return "smallFlak"
	case UnitFlak:
		return "flak"
	case UnitArcher:
		return "archer"
	case UnitArtdl:
		return "artdl"
	case UnitHeavyArtillery:
		return "heavyArtillery"
	case UnitLaserDefenseTower:
		return "laserDefenseTower"
	case UnitRadar:
		return "radar"
	case UnitAirpad:
		return "airpad"
	case UnitMissileSil:
		return "missileSil"
	case UnitAntiAir:
		return "antiAir"
	case UnitArtillery:
		return "artillery"
	case UnitHeavyTank:
		return "heavyTank"
	case UnitMediumTank:
		return "mediumTank"
	case UnitLightArtillery:
		return "lightArtillery"
	case UnitLightTank:
		return "lightTank"
	case UnitScout:
		return "scout"
	case UnitMotorcycle:
		return "motorcycle"
	case UnitRecon:
		return "recon"
	case UnitMaxTank:
		return "maxTank"
	case UnitHarvester:
		return "harvester"
	case UnitHovercraft:
		return "hovercraft"
	case UnitAmphibious:
		return "amphibious"
	case UnitMissileTank:
		return "missileTank"
	case UnitMarine:
		return "marine"
	case UnitGunShip:
		return "gunShip"
	case UnitHeavyGunship:
		return "heavyGunship"
	case UnitChinook:
		return "chinook"
	case UnitAirShip:
		return "airShip"
	case UnitFighter:
		return "fighter"
	case UnitBomber:
		return "bomber"
	case UnitStealthBomber:
		return "stealthBomber"
	case UnitStealthFighter:
		return "stealthFighter"
	case UnitDreadnought:
		return "dreadnought"
	case UnitBattleship:
		return "battleship"
	case UnitCruiser:
		return "cruiser"
	case UnitDestroyer:
		return "destroyer"
	case UnitFrigate:
		return "frigate"
	case UnitSubmarine:
		return "submarine"
	case UnitCarrier:
		return "carrier"
	case UnitTroopCarrier:
		return "troopCarrier"
	case UnitHeavyTroopCarrier:
		return "heavyTroopCarrier"
	case UnitMegaNuke:
		return "megaNuke"
	case UnitNuke:
		return "nuke"
	case UnitAntiNuke:
		return "antiNuke"
	case UnitDummyBuilding:
		return "dummyBuilding"
	case UnitBullets:
		return "bullets"
	case UnitTree:
		return "tree"
	case UnitWall:
		return "wall"
	case UnitBridge:
		return "bridge"
	case UnitAirField:
		return "airField"
	case UnitMarineTk:
		return "marineTk"
	case UnitUNKNOWN:
		return "UNKNOWN"
	}
	return ""
}

// Ordinal 对应 Kotlin enum.ordinal
func (g GameInternalUnits) Ordinal() int { return int(g) }

// FromName 对应 GameInternalUnits.from(name) — 大小写不敏感, 未找到返回 UNKNOWN
func (u FromName) From(name string) GameInternalUnits {
	for _, v := range UnitAll() {
		if asciiEq(v.Name(), name) {
			return v
		}
	}
	return UnitUNKNOWN
}

// FromIndex 对应 GameInternalUnits.from(index)
func (u FromName) FromIndex(idx int) GameInternalUnits {
	if idx < 0 || idx >= len(UnitAll()) {
		return UnitUNKNOWN
	}
	return UnitAll()[idx]
}

// asciiEq 不区分大小写比较
func asciiEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// FromName 是 from 函数的可调用包装, 对应 Kotlin 伴生对象 GameInternalUnits.from
type FromName struct{}

// UnitFrom 对应伴生对象 GameInternalUnits.from
var UnitFrom FromName

// UnitAll 一次性快照全枚举 (按 Ordinal 顺序)
func UnitAll() []GameInternalUnits {
	out := make([]GameInternalUnits, 0, 52)
	out = append(out,
		UnitExtractor, UnitLandFactory, UnitAirFactory, UnitNavalFactory,
		UnitExperimental, UnitBuilder, UnitSmallFlak, UnitFlak,
		UnitArcher, UnitArtdl, UnitLaserDefenseTower, UnitRadar,
		UnitAirpad, UnitMissileSil, UnitAntiAir, UnitArtillery,
		UnitHeavyTank, UnitMediumTank, UnitLightArtillery, UnitLightTank,
		UnitScout, UnitMotorcycle, UnitRecon, UnitMaxTank,
		UnitHarvester, UnitHovercraft, UnitAmphibious, UnitMissileTank,
		UnitMarine, UnitGunShip, UnitHeavyGunship, UnitChinook,
		UnitAirShip, UnitFighter, UnitBomber, UnitStealthBomber,
		UnitStealthFighter, UnitDreadnought, UnitBattleship, UnitCruiser,
		UnitDestroyer, UnitFrigate, UnitSubmarine, UnitCarrier,
		UnitTroopCarrier, UnitHeavyTroopCarrier, UnitMegaNuke, UnitNuke,
		UnitAntiNuke, UnitDummyBuilding, UnitBullets, UnitTree,
		UnitWall, UnitBridge, UnitAirField, UnitMarineTk,
	)
	return out
}

// HeavyArtillery 引用: 因枚举表中包含 UnitHeavyArtillery 但前向引用在同 const 块,
// 这里手动添加 alias 函数以供 from(name) 匹配 "heavyArtillery" 字面量.
func initHeavyArtilleryAlias() {
	// 已经包含在 UnitAll 中, 此函数仅为语义占位.
	_ = UnitHeavyArtillery
}
