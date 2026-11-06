// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameExternalUnits 对应 GameExternalUnits 枚举 (外部单位表 (mod / 扩展单位))
type GameExternalUnits int

const (
	// ExtAaBeamGunship 对应 aaBeamGunship
	ExtAaBeamGunship GameExternalUnits = iota
	// ExtBomber 对应 bomber
	ExtBomber
	// ExtBugGeneratorN 对应 bugGeneratorN
	ExtBugGeneratorN
	// ExtBugGeneratorNT2 对应 bugGeneratorNT2
	ExtBugGeneratorNT2
	// ExtBugMeleeT31 对应 bugMeleeT31
	ExtBugMeleeT31
	// ExtBugBee 对应 bugBee
	ExtBugBee
	// ExtBugExtractor 对应 bugExtractor
	ExtBugExtractor
	// ExtBugExtractorT2 对应 bugExtractorT2
	ExtBugExtractorT2
	// ExtBugFly 对应 bugFly
	ExtBugFly
	// ExtBugGenerator 对应 bugGenerator
	ExtBugGenerator
	// ExtBugGeneratorT2 对应 bugGeneratorT2
	ExtBugGeneratorT2
	// ExtBugMelee 对应 bugMelee
	ExtBugMelee
	// ExtBugMeleeLarge 对应 bugMeleeLarge
	ExtBugMeleeLarge
	// ExtBugMeleeSmall 对应 bugMeleeSmall
	ExtBugMeleeSmall
	// ExtBugNest 对应 bugNest
	ExtBugNest
	// ExtBugRanged 对应 bugRanged
	ExtBugRanged
	// ExtBugSpore 对应 bugSpore
	ExtBugSpore
	// ExtBugTurret 对应 bugTurret
	ExtBugTurret
	// ExtBugPickup 对应 bugPickup
	ExtBugPickup
	// ExtBugWasp 对应 bugWasp
	ExtBugWasp
	// ExtBugRangedT2 对应 bugRangedT2
	ExtBugRangedT2
	// ExtCombatEngineer 对应 combatEngineer
	ExtCombatEngineer
	// ExtExperiementalCarrier 对应 experiementalCarrier
	ExtExperiementalCarrier
	// ExtExperimentalDropship 对应 experimentalDropship
	ExtExperimentalDropship
	// ExtExperimentalGunship 对应 experimentalGunship
	ExtExperimentalGunship
	// ExtExperimentalGunshipLanded 对应 experimentalGunshipLanded
	ExtExperimentalGunshipLanded
	// ExtExperimentalSpider 对应 experimentalSpider
	ExtExperimentalSpider
	// ExtCExperimentalTank 对应 c_experimentalTank
	ExtCExperimentalTank
	// ExtExtractorT1 对应 extractorT1
	ExtExtractorT1
	// ExtExtractorT2 对应 extractorT2
	ExtExtractorT2
	// ExtExtractorT3 对应 extractorT3
	ExtExtractorT3
	// ExtExtractorT3Overclocked 对应 extractorT3_overclocked
	ExtExtractorT3Overclocked
	// ExtExtractorT3Reinforced 对应 extractorT3_reinforced
	ExtExtractorT3Reinforced
	// ExtFabricatorT1 对应 fabricatorT1
	ExtFabricatorT1
	// ExtFabricatorT2 对应 fabricatorT2
	ExtFabricatorT2
	// ExtFabricatorT3 对应 fabricatorT3
	ExtFabricatorT3
	// ExtFireBee 对应 fireBee
	ExtFireBee
	// ExtHeavyBattleship 对应 heavyBattleship
	ExtHeavyBattleship
	// ExtHeavyInterceptor 对应 heavyInterceptor
	ExtHeavyInterceptor
	// ExtHeavyMissileShip 对应 heavyMissileShip
	ExtHeavyMissileShip
	// ExtHeavySub 对应 heavySub
	ExtHeavySub
	// ExtLaboratory 对应 laboratory
	ExtLaboratory
	// ExtLightGunship 对应 lightGunship
	ExtLightGunship
	// ExtLightSub 对应 lightSub
	ExtLightSub
	// ExtMechEngineer 对应 mechEngineer
	ExtMechEngineer
	// ExtMechFactory 对应 mechFactory
	ExtMechFactory
	// ExtMechFactoryT2 对应 mechFactoryT2
	ExtMechFactoryT2
	// ExtMechArtillery 对应 mechArtillery
	ExtMechArtillery
	// ExtMechBunker 对应 mechBunker
	ExtMechBunker
	// ExtMechBunkerDeployed 对应 mechBunkerDeployed
	ExtMechBunkerDeployed
	// ExtMechFlame 对应 mechFlame
	ExtMechFlame
	// ExtMechFlyingLanded 对应 mechFlyingLanded
	ExtMechFlyingLanded
	// ExtMechFlyingTakeoff 对应 mechFlyingTakeoff
	ExtMechFlyingTakeoff
	// ExtMechHeavyMissile 对应 mechHeavyMissile
	ExtMechHeavyMissile
	// ExtMechLaser 对应 mechLaser
	ExtMechLaser
	// ExtMechLightning 对应 mechLightning
	ExtMechLightning
	// ExtMechMinigun 对应 mechMinigun
	ExtMechMinigun
	// ExtMechGun 对应 mechGun
	ExtMechGun
	// ExtMechMissile 对应 mechMissile
	ExtMechMissile
	// ExtMissileAirship 对应 missileAirship
	ExtMissileAirship
	// ExtMissileTank 对应 missileTank
	ExtMissileTank
	// ExtMissing 对应 missing
	ExtMissing
	// ExtModularSpiderAntiair 对应 modularSpider_antiair
	ExtModularSpiderAntiair
	// ExtModularSpiderAntiairFlak 对应 modularSpider_antiairFlak
	ExtModularSpiderAntiairFlak
	// ExtModularSpiderAntiairT2 对应 modularSpider_antiairT2
	ExtModularSpiderAntiairT2
	// ExtModularSpiderAntinuke 对应 modularSpider_antinuke
	ExtModularSpiderAntinuke
	// ExtModularSpiderArtillery 对应 modularSpider_artillery
	ExtModularSpiderArtillery
	// ExtModularSpiderEmptySlot 对应 modularSpider_emptySlot
	ExtModularSpiderEmptySlot
	// ExtModularSpiderFabricator 对应 modularSpider_fabricator
	ExtModularSpiderFabricator
	// ExtModularSpiderFabricatorT2 对应 modularSpider_fabricatorT2
	ExtModularSpiderFabricatorT2
	// ExtModularSpiderGunturret 对应 modularSpider_gunturret
	ExtModularSpiderGunturret
	// ExtModularSpiderGunturretT2 对应 modularSpider_gunturretT2
	ExtModularSpiderGunturretT2
	// ExtModularSpiderLaserdefense 对应 modularSpider_laserdefense
	ExtModularSpiderLaserdefense
	// ExtModularSpiderLightning 对应 modularSpider_lightning
	ExtModularSpiderLightning
	// ExtModularSpider 对应 modularSpider
	ExtModularSpider
	// ExtModularSpiderNonEmpty 对应 modularSpider_nonEmpty
	ExtModularSpiderNonEmpty
	// ExtModularSpiderShieldGen 对应 modularSpider_shieldGen
	ExtModularSpiderShieldGen
	// ExtModularSpiderSmallgunturret 对应 modularSpider_smallgunturret
	ExtModularSpiderSmallgunturret
	// ExtModularSpiderSmallgunturretT2 对应 modularSpider_smallgunturretT2
	ExtModularSpiderSmallgunturretT2
	// ExtNautilusSubmarine 对应 nautilusSubmarine
	ExtNautilusSubmarine
	// ExtNautilusSubmarineLand 对应 nautilusSubmarineLand
	ExtNautilusSubmarineLand
	// ExtNautilusSubmarineSurface 对应 nautilusSubmarineSurface
	ExtNautilusSubmarineSurface
	// ExtRobotCrab 对应 robotCrab
	ExtRobotCrab
	// ExtRobotCrabWater 对应 robotCrabWater
	ExtRobotCrabWater
	// ExtOutpostT1 对应 outpostT1
	ExtOutpostT1
	// ExtOutpostT2 对应 outpostT2
	ExtOutpostT2
	// ExtPlasmaTank 对应 plasmaTank
	ExtPlasmaTank
	// ExtCreditsCrates 对应 creditsCrates
	ExtCreditsCrates
	// ExtCrystalMid 对应 crystal_mid
	ExtCrystalMid
	// ExtScout 对应 scout
	ExtScout
	// ExtHeavyArtillery 对应 heavyArtillery
	ExtHeavyArtillery
	// ExtAntiAirTurretFlak 对应 antiAirTurretFlak
	ExtAntiAirTurretFlak
	// ExtUNKNOWN 对应 UNKNOWN
	ExtUNKNOWN
)

// externalUnitNames 对应 Kotlin enum.name, 下标即 Ordinal
var externalUnitNames = [...]string{
	"aaBeamGunship",
	"bomber",
	"bugGeneratorN",
	"bugGeneratorNT2",
	"bugMeleeT31",
	"bugBee",
	"bugExtractor",
	"bugExtractorT2",
	"bugFly",
	"bugGenerator",
	"bugGeneratorT2",
	"bugMelee",
	"bugMeleeLarge",
	"bugMeleeSmall",
	"bugNest",
	"bugRanged",
	"bugSpore",
	"bugTurret",
	"bugPickup",
	"bugWasp",
	"bugRangedT2",
	"combatEngineer",
	"experiementalCarrier",
	"experimentalDropship",
	"experimentalGunship",
	"experimentalGunshipLanded",
	"experimentalSpider",
	"c_experimentalTank",
	"extractorT1",
	"extractorT2",
	"extractorT3",
	"extractorT3_overclocked",
	"extractorT3_reinforced",
	"fabricatorT1",
	"fabricatorT2",
	"fabricatorT3",
	"fireBee",
	"heavyBattleship",
	"heavyInterceptor",
	"heavyMissileShip",
	"heavySub",
	"laboratory",
	"lightGunship",
	"lightSub",
	"mechEngineer",
	"mechFactory",
	"mechFactoryT2",
	"mechArtillery",
	"mechBunker",
	"mechBunkerDeployed",
	"mechFlame",
	"mechFlyingLanded",
	"mechFlyingTakeoff",
	"mechHeavyMissile",
	"mechLaser",
	"mechLightning",
	"mechMinigun",
	"mechGun",
	"mechMissile",
	"missileAirship",
	"missileTank",
	"missing",
	"modularSpider_antiair",
	"modularSpider_antiairFlak",
	"modularSpider_antiairT2",
	"modularSpider_antinuke",
	"modularSpider_artillery",
	"modularSpider_emptySlot",
	"modularSpider_fabricator",
	"modularSpider_fabricatorT2",
	"modularSpider_gunturret",
	"modularSpider_gunturretT2",
	"modularSpider_laserdefense",
	"modularSpider_lightning",
	"modularSpider",
	"modularSpider_nonEmpty",
	"modularSpider_shieldGen",
	"modularSpider_smallgunturret",
	"modularSpider_smallgunturretT2",
	"nautilusSubmarine",
	"nautilusSubmarineLand",
	"nautilusSubmarineSurface",
	"robotCrab",
	"robotCrabWater",
	"outpostT1",
	"outpostT2",
	"plasmaTank",
	"creditsCrates",
	"crystal_mid",
	"scout",
	"heavyArtillery",
	"antiAirTurretFlak",
	"UNKNOWN",
}

// Name 对应 Kotlin enum.name
func (v GameExternalUnits) Name() string { return enumName(externalUnitNames[:], int(v)) }

// Ordinal 对应 Kotlin enum.ordinal
func (v GameExternalUnits) Ordinal() int { return int(v) }

// String 实现 fmt.Stringer
func (v GameExternalUnits) String() string { return v.Name() }

// ExtAll 按 Ordinal 顺序返回全部枚举值
func ExtAll() []GameExternalUnits {
	out := make([]GameExternalUnits, 0, len(externalUnitNames))
	for i := range externalUnitNames {
		out = append(out, GameExternalUnits(i))
	}
	return out
}

// ExtFromName 对应 GameExternalUnits.from(name), 大小写不敏感, 未找到返回 UNKNOWN
func ExtFromName(name string) GameExternalUnits {
	return GameExternalUnits(lookupEnumIndex(externalUnitNames[:], name, int(ExtUNKNOWN)))
}

// ExtFromIndex 对应 GameExternalUnits.from(index), 越界返回 UNKNOWN
func ExtFromIndex(index int) GameExternalUnits {
	return GameExternalUnits(lookupEnumIndexByIndex(len(externalUnitNames), index, int(ExtUNKNOWN)))
}
