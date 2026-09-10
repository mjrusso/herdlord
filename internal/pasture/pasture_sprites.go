package pasture

import "embed"

// Blank entries reserve removed sprite IDs so later protocol image IDs stay stable.
const (
	kittySheepImageID = 71 + iota
	kittyWalkImageID
	kittyBlinkImageID
	kittySheepRightID
	kittyWalkRightID
	kittyBlinkRightID
	kittyDoneImageID
	kittyShepherdID
	kittyGrassImageID
	kittyFenceHorizID
	kittyFenceVertID
	kittyTargetSignID
	kittyCastleID
	kittyLordID
	kittyRoyalBgID
	kittyRoadVertID
	kittyRoadHorizID
	kittyFenceGateID
	kittyWorkingID
	kittyWorkingAltID
	kittyBlockedID
	kittyStompID
	kittyWobbleID
	kittyFallenID
	kittySpiritID
	_
	kittyShepherdWorkID
	kittyShepherdAlertID
	_
	kittyLordAlertID
	kittyDirtID
	kittyLoomID
	kittyLoomAltID
	kittyLoomJammedID
	_
	kittyLordMapID
	kittyDoneAltID
	kittyRetiredImageID
)

type kittySprite struct {
	id    int
	path  string
	class kittySpriteClass
	poses []pose
}

func (s kittySprite) active() bool {
	return s.path != ""
}

type kittySpriteClass uint8

const (
	kittySheep kittySpriteClass = 1 << iota
	kittyShepherd
	kittyLord
	kittyLoom
	kittyLayout
	kittyForeground
)

//go:embed assets/*.png
var assets embed.FS

var kittySprites = []kittySprite{
	{id: kittySheepImageID, path: "assets/sheep.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepRestLeft}},
	{id: kittyWalkImageID, path: "assets/sheep-walk.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepWalkLeft}},
	{id: kittyBlinkImageID, path: "assets/sheep-blink.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepBlinkLeft}},
	{id: kittySheepRightID, path: "assets/sheep-right.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepRestRight}},
	{id: kittyWalkRightID, path: "assets/sheep-walk-right.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepWalkRight}},
	{id: kittyBlinkRightID, path: "assets/sheep-blink-right.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepBlinkRight}},
	{id: kittyDoneImageID, path: "assets/sheep-done.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepDone}},
	{id: kittyDoneAltID, path: "assets/sheep-done-breathe.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepDoneAlt}},
	{id: kittyShepherdID, path: "assets/shepherd.png", class: kittyShepherd | kittyForeground, poses: []pose{poseShepherdCalm}},
	{id: kittyGrassImageID, path: "assets/grass.png", class: kittyLayout},
	{id: kittyDirtID, path: "assets/dirt.png", class: kittyLayout | kittyForeground},
	{id: kittyFenceHorizID, path: "assets/fence-horizontal.png", class: kittyLayout | kittyForeground},
	{id: kittyFenceVertID, path: "assets/fence-vertical.png", class: kittyLayout | kittyForeground},
	{id: kittyTargetSignID, path: "assets/target-sign.png", class: kittyLayout | kittyForeground},
	{id: kittyCastleID, path: "assets/castle.png", class: kittyLayout | kittyForeground},
	{id: kittyLordID, path: "assets/lord.png", class: kittyLord | kittyForeground, poses: []pose{poseLordCalm}},
	{id: kittyRoyalBgID, path: "assets/royal-background.png", class: kittyLayout | kittyForeground},
	{id: kittyRoadVertID, path: "assets/road-vertical.png", class: kittyLayout | kittyForeground},
	{id: kittyRoadHorizID, path: "assets/road-horizontal.png", class: kittyLayout | kittyForeground},
	{id: kittyFenceGateID, path: "assets/fence-gate.png", class: kittyLayout | kittyForeground},
	{id: kittyWorkingID, path: "assets/sheep-working.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepWorking}},
	{id: kittyWorkingAltID, path: "assets/sheep-working-bob.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepWorkingAlt}},
	{id: kittyBlockedID, path: "assets/sheep-blocked-stomp.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepBlocked}},
	{id: kittyStompID, path: "assets/sheep-blocked-bob.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepStomp}},
	{id: kittyWobbleID, path: "assets/sheep-dying-wobble.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepWobble}},
	{id: kittyFallenID, path: "assets/sheep-dying-fallen.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepFallen}},
	{id: kittySpiritID, path: "assets/sheep-dying-spirit.png", class: kittySheep | kittyForeground, poses: []pose{poseSheepSpirit}},
	{id: kittyShepherdWorkID, path: "assets/shepherd-working.png", class: kittyShepherd | kittyForeground, poses: []pose{poseShepherdWorking}},
	{id: kittyShepherdAlertID, path: "assets/shepherd-alert.png", class: kittyShepherd | kittyForeground, poses: []pose{poseShepherdAlert}},
	{id: kittyLordAlertID, path: "assets/lord-alert.png", class: kittyLord | kittyForeground, poses: []pose{poseLordAlert}},
	{id: kittyLoomID, path: "assets/loom.png", class: kittyLoom | kittyForeground, poses: []pose{poseLoomWorking}},
	{id: kittyLoomAltID, path: "assets/loom-alt.png", class: kittyLoom | kittyForeground, poses: []pose{poseLoomWorkingAlt}},
	{id: kittyLoomJammedID, path: "assets/loom-jammed.png", class: kittyLoom | kittyForeground, poses: []pose{poseLoomJammed}},
	{id: kittyLordMapID, path: "assets/lord-command-map.png", class: kittyLord | kittyForeground, poses: []pose{poseLordDirecting}},
	// Keep the retired ID reserved so old image data is purged from the terminal.
	{id: kittyRetiredImageID},
}

var kittyPoseImageIDs = func() map[pose]int {
	images := make(map[pose]int, int(poseCount))
	for _, sprite := range kittySprites {
		for _, pose := range sprite.poses {
			images[pose] = sprite.id
		}
	}
	return images
}()
