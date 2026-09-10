package pasture

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	asciiTop = 2
)

func (asciiRenderer) enter() (string, error) { return "", nil }

func (asciiRenderer) leave() string { return "" }

func (asciiRenderer) render(scene Scene) string {
	rows := make([]string, 0, len(scene.rows))
	for rowIndex, row := range scene.rows {
		left := 0
		if len(row.pens) > 0 {
			left = row.pens[0].bounds.x
		}
		pens := []string{strings.Repeat(" ", left)}
		for i, penScene := range row.pens {
			if i > 0 {
				pens = append(pens, strings.Repeat(" ", penGapColumns))
			}
			offset := penScene.bounds.y - row.top
			pen := asciiPen(penScene)
			pens = append(pens, asciiApproach(penScene.bounds.width, offset)+pen)
		}
		rowView := lipgloss.JoinHorizontal(lipgloss.Top, pens...)
		if rowIndex < len(scene.rows)-1 {
			rowView += "\n" + strings.Repeat(" ", scene.width)
		}
		rows = append(rows, rowView)
	}
	sections := []string{asciiRoyalScene(scene)}
	if scene.gridTop > 0 {
		sections = append(sections, asciiMeadow(scene.width, scene.gridTop, scene.roadCenters))
	}
	sections = append(sections, rows...)
	if scene.gridBottom > 0 {
		sections = append(sections, asciiMeadow(scene.width, scene.gridBottom, nil))
	}
	return strings.Join(sections, "\n")
}

func asciiMeadow(width, height int, roadCenters []int) string {
	cells := asciiCanvas(width, height, '.')
	for _, center := range roadCenters {
		if center < 0 || center >= width {
			continue
		}
		for row := range cells {
			cells[row][center] = '#'
		}
	}
	return asciiLines(cells)
}

func asciiRoyalScene(scene Scene) string {
	width, roadCenters := scene.width, scene.roadCenters
	cells := asciiCanvas(width, royalSceneRows, ' ')
	castle := []string{"  _|_|_  ", " |[_ _]| ", "_|_###_|_"}
	castleX, lordX := asciiCommandStationColumns(width)
	asciiDraw(cells, castle, castleX, 2)
	if len(roadCenters) > 0 {
		center := width / 2
		first, last := roadCenters[0], roadCenters[len(roadCenters)-1]
		for x := min(center, first); x <= max(center, last) && x < width; x++ {
			cells[royalSceneRows-2][x] = '#'
		}
		for _, x := range roadCenters {
			if x >= 0 && x < width {
				cells[royalSceneRows-1][x] = '#'
			}
		}
	}
	lord := []string{" o>", "/|]", "/ \\"}
	switch scene.lord.pose {
	case poseLordAlert:
		lord = []string{" !>", "/|]", "/ \\"}
	case poseLordDirecting:
		lord = []string{" o]", "/|=", "/ \\"}
	}
	asciiDraw(cells, lord, lordX, 2)
	lordText := scene.lordBubble
	drawASCIISideBubble(cells, lordText, lordX, 2, 3, 3, width, royalSceneRows)
	return asciiLines(cells)
}

func asciiCommandStationColumns(width int) (int, int) {
	return commandStationColumns(width, 9, 3, 2)
}

func asciiApproach(width, height int) string {
	if height == 0 {
		return ""
	}
	cells := asciiCanvas(width, height, ' ')
	center := gateCenter(width)
	for y := range cells {
		cells[y][center] = '#'
	}
	return asciiLines(cells) + "\n"
}

func asciiPen(pen penScene) string {
	width, height := pen.bounds.width, pen.bounds.height
	cells := asciiCanvas(width, height, '.')
	for y := 0; y < asciiTop; y++ {
		for x := range cells[y] {
			cells[y][x] = ' '
		}
		cells[y][gateCenter(width)] = '#'
	}
	asciiFence(cells, width, height)
	asciiSign(cells, pen.name, width)
	workTop := asciiWorkyardTop(height)
	for y := workTop; y < height-1; y++ {
		for x := fenceColumns; x < width-fenceColumns; x++ {
			cells[y][x] = '='
		}
	}
	for _, loom := range pen.looms {
		asciiOverlay(cells, asciiLoom(loom.pose), loom.position.x, asciiSheepRow(loom.position.y, height))
	}
	asciiWorkshop(cells, width, workTop)
	asciiOverlay(cells, asciiShepherdPose(pen.shepherd.pose), pen.shepherd.position.x, asciiShepherdRow(pen.shepherd.position.y, height))
	for _, sheep := range pen.sheep {
		y := asciiSheepRow(sheep.position.y, height)
		asciiOverlay(cells, asciiSheepPose(sheep.pose, sheep.agent.Status == "unknown" && sheep.transition == present), sheep.position.x, y)
	}
	renderASCIISceneBubbles(cells, pen, height)
	return asciiLines(cells)
}

func renderASCIISceneBubbles(cells [][]rune, pen penScene, height int) {
	if bubble, exists := pen.bubbles[bubbleSheep]; exists {
		for _, sheep := range pen.sheep {
			if sheep.agent.PaneID != bubble.pane {
				continue
			}
			y := asciiSheepRow(sheep.position.y, height)
			drawASCIIBubble(cells, bubble.text, sheep.position.x, max(asciiTop+1, y-2), pen.bounds.width, height)
			break
		}
	}
	if bubble, exists := pen.bubbles[bubbleShepherd]; exists {
		y := asciiShepherdRow(pen.shepherd.position.y, height)
		drawASCIIBubble(cells, bubble.text, pen.shepherd.position.x+sheepColumns, max(asciiTop+1, y-1), pen.bounds.width, height)
	}
}

func drawASCIIBubble(cells [][]rune, text string, x, y, width, height int) {
	text = asciiLabel(text)
	line, x, y, tail := bubbleLayout(text, x, y, width, height)
	if line == "" {
		return
	}
	pointer := strings.Repeat(" ", tail-x) + "v"
	asciiDraw(cells, []string{line, pointer}, x, y)
}

func drawASCIISideBubble(cells [][]rune, text string, actorX, actorY, actorWidth, actorHeight, width, height int) {
	text = asciiLabel(text)
	line, x, y, pointerX, pointer := sideBubbleLayout(text, actorX, actorY, actorWidth, actorHeight, width, height)
	if line == "" {
		return
	}
	asciiDraw(cells, []string{line}, x, y)
	cells[y][pointerX] = []rune(pointer)[0]
}

func asciiWorkyardTop(height int) int {
	sourceRows := max(1, height-top-fenceRows*2)
	destinationRows := max(1, height-asciiTop-2)
	workyardRows := height - fenceRows - workyardTop(height)
	rows := max(4, workyardRows*destinationRows/sourceRows)
	return max(asciiTop+1, height-1-rows)
}

func asciiWorkshop(cells [][]rune, width, top int) {
	if top < 0 || top >= len(cells) || width < 12 {
		return
	}
	for x := fenceColumns; x < width-fenceColumns; x++ {
		cells[top][x] = '-'
	}
	label := "[ LOOM ]"
	asciiDraw(cells, []string{label}, max(fenceColumns+1, (width-len(label))/2), top)
	for y := top + 1; y < len(cells)-1; y++ {
		cells[y][fenceColumns], cells[y][width-fenceColumns-1] = '|', '|'
	}
}

func asciiFence(cells [][]rune, width, height int) {
	for x := range width {
		cells[asciiTop][x] = '-'
		cells[height-1][x] = '-'
	}
	for y := asciiTop; y < height; y++ {
		cells[y][0], cells[y][width-1] = '|', '|'
	}
	cells[asciiTop][0], cells[asciiTop][width-1] = '+', '+'
	cells[height-1][0], cells[height-1][width-1] = '+', '+'
	gate := gateColumn(width)
	for x := gate; x < gate+gateColumns; x++ {
		cells[asciiTop][x] = ' '
	}
	cells[asciiTop][gate], cells[asciiTop][gate+gateColumns-1] = '[', ']'
}

func asciiSign(cells [][]rune, name string, width int) {
	name = asciiLabel(name)
	maximum := max(1, min(len(name), width-8))
	name = name[:maximum]
	sign := "+- " + name + " -+"
	x := max(1, (width-len(sign))/2)
	asciiDraw(cells, []string{sign}, x, 0)
	cells[1][x+1], cells[1][x+len(sign)-2] = '|', '|'
}

func asciiActorRow(sourceY, height, spriteRows int) int {
	sourceStart := top + fenceRows
	sourceRange := max(1, height-sourceStart-fenceRows-spriteRows)
	destinationStart := asciiTop + 1
	destinationRange := max(0, height-destinationStart-1-3)
	sourceOffset := min(sourceRange, max(0, sourceY-sourceStart))
	return destinationStart + sourceOffset*destinationRange/sourceRange
}

func asciiSheepRow(sourceY, height int) int {
	return asciiActorRow(sourceY, height, sheepBodyRows)
}

func asciiShepherdRow(sourceY, height int) int {
	return asciiActorRow(sourceY, height, shepherdRows)
}

func asciiLabel(value string) string {
	var label strings.Builder
	for _, char := range value {
		if char >= ' ' && char <= '~' {
			label.WriteRune(char)
		} else {
			label.WriteByte('?')
		}
	}
	return label.String()
}

func asciiShepherdPose(pose pose) []string {
	switch pose {
	case poseShepherdAlert:
		return []string{" !|", " /!", " / \\"}
	case poseShepherdWorking:
		return []string{" o|", " /]", " / \\"}
	default:
		return []string{" o/", " /|", " / \\"}
	}
}

func asciiSheepPose(pose pose, unknown bool) []string {
	var sprite []string
	switch pose {
	case poseSheepWorking:
		sprite = []string{" (oo)   ", "(____)>>", " |=|=|  "}
	case poseSheepWorkingAlt:
		sprite = []string{" (oo)   ", "(____)<<", " |=|=|  "}
	case poseSheepBlocked:
		sprite = []string{"!(xx)!  ", "(____) X", " |#X#|  "}
	case poseSheepStomp:
		sprite = []string{"!(xx)!  ", "(____) X", " |X#X|  "}
	case poseSheepDone:
		sprite = []string{" (--)z  ", "(____)  ", "  --    "}
	case poseSheepDoneAlt:
		sprite = []string{" (--)Z  ", "(____)  ", "  --    "}
	case poseSheepWobble:
		sprite = []string{"!(oo)!  ", "(____)  ", " /  \\   "}
	case poseSheepFallen:
		sprite = []string{"        ", " (xx)   ", "(____)__"}
	case poseSheepSpirit:
		sprite = []string{"  ^oo^  ", "   ^^   ", "        "}
	case poseSheepBlinkRight:
		sprite = asciiRestingSheep("--", 1)
	case poseSheepBlinkLeft:
		sprite = asciiRestingSheep("--", -1)
	case poseSheepWalkRight:
		sprite = asciiWalkingSheep("oo", 1)
	case poseSheepWalkLeft:
		sprite = asciiWalkingSheep("oo", -1)
	case poseSheepRestRight:
		sprite = asciiRestingSheep("oo", 1)
	default:
		sprite = asciiRestingSheep("oo", -1)
	}
	if unknown {
		sprite[0] = sprite[0][:7] + "?"
	}
	return sprite
}

func asciiRestingSheep(eyes string, direction int) []string {
	if direction > 0 {
		return []string{" (" + eyes + ")>  ", "(____)  ", " /  \\   "}
	}
	return []string{" <(" + eyes + ")  ", " (____) ", "  /  \\  "}
}

func asciiWalkingSheep(eyes string, direction int) []string {
	sprite := asciiRestingSheep(eyes, direction)
	sprite[2] = "  /\\    "
	return sprite
}

func asciiLoom(pose pose) []string {
	switch pose {
	case poseLoomWorkingAlt:
		return []string{"  |%|   ", "--|||---", "  / \\   "}
	case poseLoomJammed:
		return []string{"  |X|   ", "--|#|---", "  / \\   "}
	default:
		return []string{"  |#|   ", "--|||---", "  / \\   "}
	}
}

func asciiCanvas(width, height int, fill rune) [][]rune {
	cells := make([][]rune, height)
	for y := range cells {
		cells[y] = make([]rune, width)
		for x := range cells[y] {
			cells[y][x] = fill
		}
	}
	return cells
}

func asciiDraw(cells [][]rune, sprite []string, x, y int) {
	for row, line := range sprite {
		if y+row < 0 || y+row >= len(cells) {
			continue
		}
		for column, char := range []rune(line) {
			if x+column >= 0 && x+column < len(cells[y+row]) {
				cells[y+row][x+column] = char
			}
		}
	}
}

func asciiOverlay(cells [][]rune, sprite []string, x, y int) {
	for row, line := range sprite {
		if y+row < 0 || y+row >= len(cells) {
			continue
		}
		for column, char := range []rune(line) {
			if char != ' ' && x+column >= 0 && x+column < len(cells[y+row]) {
				cells[y+row][x+column] = char
			}
		}
	}
}

func asciiLines(cells [][]rune) string {
	lines := make([]string, len(cells))
	for i := range cells {
		lines[i] = string(cells[i])
	}
	return strings.Join(lines, "\n")
}
