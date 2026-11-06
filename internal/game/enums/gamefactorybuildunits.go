// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameFactoryBuildUnits 对应 GameFactoryBuildUnits 枚举 (工厂建造单位表, 名称带 u_ 前缀)
type GameFactoryBuildUnits int

const (
	// FactoryUCombatEngineer 对应 u_combatEngineer
	FactoryUCombatEngineer GameFactoryBuildUnits = iota
	// FactoryUCExperimentalTank 对应 u_c_experimentalTank
	FactoryUCExperimentalTank
	// FactoryUNautilusSubmarineLand 对应 u_nautilusSubmarineLand
	FactoryUNautilusSubmarineLand
	// FactoryUExperimentalHoverTank 对应 u_experimentalHoverTank
	FactoryUExperimentalHoverTank
	// FactoryUExperimentalDropship 对应 u_experimentalDropship
	FactoryUExperimentalDropship
	// FactoryUExperimentalSpider 对应 u_experimentalSpider
	FactoryUExperimentalSpider
	// FactoryUFireBee 对应 u_fireBee
	FactoryUFireBee
	// FactoryUBuilder 对应 u_builder
	FactoryUBuilder
	// FactoryUScout 对应 u_scout
	FactoryUScout
	// FactoryUCTank 对应 u_c_tank
	FactoryUCTank
	// FactoryUHoverTank 对应 u_hoverTank
	FactoryUHoverTank
	// FactoryUCArtillery 对应 u_c_artillery
	FactoryUCArtillery
	// FactoryUMissileTank 对应 u_missileTank
	FactoryUMissileTank
	// FactoryUPlasmaTank 对应 u_plasmaTank
	FactoryUPlasmaTank
	// FactoryUHeavyTank 对应 u_heavyTank
	FactoryUHeavyTank
	// FactoryUHeavyArtillery 对应 u_heavyArtillery
	FactoryUHeavyArtillery
	// FactoryUHeavyHoverTank 对应 u_heavyHoverTank
	FactoryUHeavyHoverTank
	// FactoryUCLaserTank 对应 u_c_laserTank
	FactoryUCLaserTank
	// FactoryUCMammothTank 对应 u_c_mammothTank
	FactoryUCMammothTank
	// FactoryULightGunship 对应 u_lightGunship
	FactoryULightGunship
	// FactoryUCInterceptor 对应 u_c_interceptor
	FactoryUCInterceptor
	// FactoryUCHelicopter 对应 u_c_helicopter
	FactoryUCHelicopter
	// FactoryUSpyDrone 对应 u_spyDrone
	FactoryUSpyDrone
	// FactoryUDropship 对应 u_dropship
	FactoryUDropship
	// FactoryUGunShip 对应 u_gunShip
	FactoryUGunShip
	// FactoryUHeavyInterceptor 对应 u_heavyInterceptor
	FactoryUHeavyInterceptor
	// FactoryUAmphibiousJet 对应 u_amphibiousJet
	FactoryUAmphibiousJet
	// FactoryUAaBeamGunship 对应 u_aaBeamGunship
	FactoryUAaBeamGunship
	// FactoryUBomber 对应 u_bomber
	FactoryUBomber
	// FactoryUMissileAirship 对应 u_missileAirship
	FactoryUMissileAirship
	// FactoryUMechGun 对应 u_mechGun
	FactoryUMechGun
	// FactoryUMechMissile 对应 u_mechMissile
	FactoryUMechMissile
	// FactoryUMechEngineer 对应 u_mechEngineer
	FactoryUMechEngineer
	// FactoryUMechArtillery 对应 u_mechArtillery
	FactoryUMechArtillery
	// FactoryUMechBunker 对应 u_mechBunker
	FactoryUMechBunker
	// FactoryUMechMinigun 对应 u_mechMinigun
	FactoryUMechMinigun
	// FactoryUMechLaser 对应 u_mechLaser
	FactoryUMechLaser
	// FactoryUMechLightning 对应 u_mechLightning
	FactoryUMechLightning
	// FactoryUMechHeavyMissile 对应 u_mechHeavyMissile
	FactoryUMechHeavyMissile
	// FactoryUMechFlame 对应 u_mechFlame
	FactoryUMechFlame
	// FactoryUBuilderShip 对应 u_builderShip
	FactoryUBuilderShip
	// FactoryUGunBoat 对应 u_gunBoat
	FactoryUGunBoat
	// FactoryULightSub 对应 u_lightSub
	FactoryULightSub
	// FactoryUMissileShip 对应 u_missileShip
	FactoryUMissileShip
	// FactoryUHovercraft 对应 u_hovercraft
	FactoryUHovercraft
	// FactoryUBattleShip 对应 u_battleShip
	FactoryUBattleShip
	// FactoryUAttackSubmarine 对应 u_attackSubmarine
	FactoryUAttackSubmarine
	// FactoryUHeavyAAShip 对应 u_heavyAAShip
	FactoryUHeavyAAShip
	// FactoryUHeavyBattleship 对应 u_heavyBattleship
	FactoryUHeavyBattleship
	// FactoryUHeavyMissileShip 对应 u_heavyMissileShip
	FactoryUHeavyMissileShip
	// FactoryUHeavySub 对应 u_heavySub
	FactoryUHeavySub
	// FactoryUNautilusSubmarine 对应 u_nautilusSubmarine
	FactoryUNautilusSubmarine
	// FactoryUExperiementalCarrier 对应 u_experiementalCarrier
	FactoryUExperiementalCarrier
	// Factory0 对应 _0
	Factory0
	// Factory1 对应 _1
	Factory1
	// FactoryCTurretT2Gun0 对应 c_turret_t2_gun_0
	FactoryCTurretT2Gun0
	// FactoryCTurretT3Gun0 对应 c_turret_t3_gun_0
	FactoryCTurretT3Gun0
	// FactoryCTurretT1Artillery1 对应 c_turret_t1_artillery_1
	FactoryCTurretT1Artillery1
	// FactoryCTurretT2Flame2 对应 c_turret_t2_flame_2
	FactoryCTurretT2Flame2
	// FactoryCTurretT1Lightning3 对应 c_turret_t1_lightning_3
	FactoryCTurretT1Lightning3
	// FactoryMechFactoryT22 对应 mechFactoryT2_2
	FactoryMechFactoryT22
	// FactoryCAntiAirTurretT20 对应 c_antiAirTurretT2_0
	FactoryCAntiAirTurretT20
	// FactoryCAntiAirTurretT30 对应 c_antiAirTurretT3_0
	FactoryCAntiAirTurretT30
	// FactoryAntiAirTurretFlak1 对应 antiAirTurretFlak_1
	FactoryAntiAirTurretFlak1
	// FactoryUNKNOWN 对应 UNKNOWN
	FactoryUNKNOWN
)

// factoryUnitNames 对应 Kotlin enum.name, 下标即 Ordinal
var factoryUnitNames = [...]string{
	"u_combatEngineer",
	"u_c_experimentalTank",
	"u_nautilusSubmarineLand",
	"u_experimentalHoverTank",
	"u_experimentalDropship",
	"u_experimentalSpider",
	"u_fireBee",
	"u_builder",
	"u_scout",
	"u_c_tank",
	"u_hoverTank",
	"u_c_artillery",
	"u_missileTank",
	"u_plasmaTank",
	"u_heavyTank",
	"u_heavyArtillery",
	"u_heavyHoverTank",
	"u_c_laserTank",
	"u_c_mammothTank",
	"u_lightGunship",
	"u_c_interceptor",
	"u_c_helicopter",
	"u_spyDrone",
	"u_dropship",
	"u_gunShip",
	"u_heavyInterceptor",
	"u_amphibiousJet",
	"u_aaBeamGunship",
	"u_bomber",
	"u_missileAirship",
	"u_mechGun",
	"u_mechMissile",
	"u_mechEngineer",
	"u_mechArtillery",
	"u_mechBunker",
	"u_mechMinigun",
	"u_mechLaser",
	"u_mechLightning",
	"u_mechHeavyMissile",
	"u_mechFlame",
	"u_builderShip",
	"u_gunBoat",
	"u_lightSub",
	"u_missileShip",
	"u_hovercraft",
	"u_battleShip",
	"u_attackSubmarine",
	"u_heavyAAShip",
	"u_heavyBattleship",
	"u_heavyMissileShip",
	"u_heavySub",
	"u_nautilusSubmarine",
	"u_experiementalCarrier",
	"_0",
	"_1",
	"c_turret_t2_gun_0",
	"c_turret_t3_gun_0",
	"c_turret_t1_artillery_1",
	"c_turret_t2_flame_2",
	"c_turret_t1_lightning_3",
	"mechFactoryT2_2",
	"c_antiAirTurretT2_0",
	"c_antiAirTurretT3_0",
	"antiAirTurretFlak_1",
	"UNKNOWN",
}

// Name 对应 Kotlin enum.name
func (v GameFactoryBuildUnits) Name() string { return enumName(factoryUnitNames[:], int(v)) }

// Ordinal 对应 Kotlin enum.ordinal
func (v GameFactoryBuildUnits) Ordinal() int { return int(v) }

// String 实现 fmt.Stringer
func (v GameFactoryBuildUnits) String() string { return v.Name() }

// FactoryAll 按 Ordinal 顺序返回全部枚举值
func FactoryAll() []GameFactoryBuildUnits {
	out := make([]GameFactoryBuildUnits, 0, len(factoryUnitNames))
	for i := range factoryUnitNames {
		out = append(out, GameFactoryBuildUnits(i))
	}
	return out
}

// FactoryFromName 对应 GameFactoryBuildUnits.from(name), 大小写不敏感, 未找到返回 UNKNOWN
func FactoryFromName(name string) GameFactoryBuildUnits {
	return GameFactoryBuildUnits(lookupEnumIndex(factoryUnitNames[:], name, int(FactoryUNKNOWN)))
}

// FactoryFromIndex 对应 GameFactoryBuildUnits.from(index), 越界返回 UNKNOWN
func FactoryFromIndex(index int) GameFactoryBuildUnits {
	return GameFactoryBuildUnits(lookupEnumIndexByIndex(len(factoryUnitNames), index, int(FactoryUNKNOWN)))
}
