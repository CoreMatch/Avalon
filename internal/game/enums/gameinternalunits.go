// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameInternalUnits 对应 GameInternalUnits 枚举 (内部单位表, 用于 gameSummonPacket 等单位名解析)
type GameInternalUnits int

const (
	// UnitExtractor 对应 extractor
	UnitExtractor GameInternalUnits = iota
	// UnitLandFactory 对应 landFactory
	UnitLandFactory
	// UnitAirFactory 对应 airFactory
	UnitAirFactory
	// UnitSeaFactory 对应 seaFactory
	UnitSeaFactory
	// UnitCommandCenter 对应 commandCenter
	UnitCommandCenter
	// UnitTurret 对应 turret
	UnitTurret
	// UnitAntiAirTurret 对应 antiAirTurret
	UnitAntiAirTurret
	// UnitBuilder 对应 builder
	UnitBuilder
	// UnitTank 对应 tank
	UnitTank
	// UnitHoverTank 对应 hoverTank
	UnitHoverTank
	// UnitArtillery 对应 artillery
	UnitArtillery
	// UnitHelicopter 对应 helicopter
	UnitHelicopter
	// UnitAirShip 对应 airShip
	UnitAirShip
	// UnitGunShip 对应 gunShip
	UnitGunShip
	// UnitMissileShip 对应 missileShip
	UnitMissileShip
	// UnitGunBoat 对应 gunBoat
	UnitGunBoat
	// UnitMegaTank 对应 megaTank
	UnitMegaTank
	// UnitLaserTank 对应 laserTank
	UnitLaserTank
	// UnitHovercraft 对应 hovercraft
	UnitHovercraft
	// UnitLadybug 对应 ladybug
	UnitLadybug
	// UnitBattleShip 对应 battleShip
	UnitBattleShip
	// UnitTankDestroyer 对应 tankDestroyer
	UnitTankDestroyer
	// UnitHeavyTank 对应 heavyTank
	UnitHeavyTank
	// UnitHeavyHoverTank 对应 heavyHoverTank
	UnitHeavyHoverTank
	// UnitLaserDefence 对应 laserDefence
	UnitLaserDefence
	// UnitDropship 对应 dropship
	UnitDropship
	// UnitTree 对应 tree
	UnitTree
	// UnitRepairbay 对应 repairbay
	UnitRepairbay
	// UnitNukeLaucher 对应 NukeLaucher
	UnitNukeLaucher
	// UnitAntiNukeLaucher 对应 AntiNukeLaucher
	UnitAntiNukeLaucher
	// UnitMammothTank 对应 mammothTank
	UnitMammothTank
	// UnitExperimentalTank 对应 experimentalTank
	UnitExperimentalTank
	// UnitExperimentalLandFactory 对应 experimentalLandFactory
	UnitExperimentalLandFactory
	// UnitCrystalResource 对应 crystalResource
	UnitCrystalResource
	// UnitWallV 对应 wall_v
	UnitWallV
	// UnitFabricator 对应 fabricator
	UnitFabricator
	// UnitAttackSubmarine 对应 attackSubmarine
	UnitAttackSubmarine
	// UnitBuilderShip 对应 builderShip
	UnitBuilderShip
	// UnitAmphibiousJet 对应 amphibiousJet
	UnitAmphibiousJet
	// UnitSupplyDepot 对应 supplyDepot
	UnitSupplyDepot
	// UnitExperimentalHoverTank 对应 experimentalHoverTank
	UnitExperimentalHoverTank
	// UnitTurretArtillery 对应 turret_artillery
	UnitTurretArtillery
	// UnitTurretFlamethrower 对应 turret_flamethrower
	UnitTurretFlamethrower
	// UnitFogRevealer 对应 fogRevealer
	UnitFogRevealer
	// UnitSpreadingFire 对应 spreadingFire
	UnitSpreadingFire
	// UnitAntiAirTurretT2 对应 antiAirTurretT2
	UnitAntiAirTurretT2
	// UnitTurretT2 对应 turretT2
	UnitTurretT2
	// UnitTurretT3 对应 turretT3
	UnitTurretT3
	// UnitDamagingBorder 对应 damagingBorder
	UnitDamagingBorder
	// UnitZoneMarker 对应 zoneMarker
	UnitZoneMarker
	// UnitEditorOrBuilder 对应 editorOrBuilder
	UnitEditorOrBuilder
	// UnitUNKNOWN 对应 UNKNOWN
	UnitUNKNOWN
)

// unitNames 对应 Kotlin enum.name, 下标即 Ordinal
var unitNames = [...]string{
	"extractor",
	"landFactory",
	"airFactory",
	"seaFactory",
	"commandCenter",
	"turret",
	"antiAirTurret",
	"builder",
	"tank",
	"hoverTank",
	"artillery",
	"helicopter",
	"airShip",
	"gunShip",
	"missileShip",
	"gunBoat",
	"megaTank",
	"laserTank",
	"hovercraft",
	"ladybug",
	"battleShip",
	"tankDestroyer",
	"heavyTank",
	"heavyHoverTank",
	"laserDefence",
	"dropship",
	"tree",
	"repairbay",
	"NukeLaucher",
	"AntiNukeLaucher",
	"mammothTank",
	"experimentalTank",
	"experimentalLandFactory",
	"crystalResource",
	"wall_v",
	"fabricator",
	"attackSubmarine",
	"builderShip",
	"amphibiousJet",
	"supplyDepot",
	"experimentalHoverTank",
	"turret_artillery",
	"turret_flamethrower",
	"fogRevealer",
	"spreadingFire",
	"antiAirTurretT2",
	"turretT2",
	"turretT3",
	"damagingBorder",
	"zoneMarker",
	"editorOrBuilder",
	"UNKNOWN",
}

// Name 对应 Kotlin enum.name
func (v GameInternalUnits) Name() string { return enumName(unitNames[:], int(v)) }

// Ordinal 对应 Kotlin enum.ordinal
func (v GameInternalUnits) Ordinal() int { return int(v) }

// String 实现 fmt.Stringer
func (v GameInternalUnits) String() string { return v.Name() }

// UnitAll 按 Ordinal 顺序返回全部枚举值
func UnitAll() []GameInternalUnits {
	out := make([]GameInternalUnits, 0, len(unitNames))
	for i := range unitNames {
		out = append(out, GameInternalUnits(i))
	}
	return out
}

// UnitFromName 对应 GameInternalUnits.from(name), 大小写不敏感, 未找到返回 UNKNOWN
func UnitFromName(name string) GameInternalUnits {
	return GameInternalUnits(lookupEnumIndex(unitNames[:], name, int(UnitUNKNOWN)))
}

// UnitFromIndex 对应 GameInternalUnits.from(index), 越界返回 UNKNOWN
func UnitFromIndex(index int) GameInternalUnits {
	return GameInternalUnits(lookupEnumIndexByIndex(len(unitNames), index, int(UnitUNKNOWN)))
}
