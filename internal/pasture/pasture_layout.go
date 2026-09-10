package pasture

import (
	"fmt"

	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func (p *State) layout(frame Frame) grid {
	grid := buildGrid(contentWidth(frame.Viewport.Width), frame.Snapshot.Targets, frame.Snapshot.Statuses, frame.Viewport.Height, p.scrollRow)
	p.scrollRow = grid.firstRow
	return grid
}

func buildGrid(width int, targets []target.Target, statuses map[string]poll.TargetStatus, availableHeight, scrollRow int) grid {
	minimumWidth := sheepAreaOrigin + sheepColumns + fenceColumns
	targetCount := max(1, len(targets))
	maximumColumns := min(targetCount, max(1, (width+penGapColumns)/(minimumWidth+penGapColumns)))
	columns := 1
	bestDistance := maximumPenColumns
	for candidate := 1; candidate <= maximumColumns; candidate++ {
		candidateWidth := min(maximumPenColumns, (width-(candidate-1)*penGapColumns)/candidate)
		distance := abs(candidateWidth - preferredPenColumns)
		if distance <= bestDistance {
			columns = candidate
			bestDistance = distance
		}
	}
	penWidth := min(maximumPenColumns, max(minimumWidth, (width-(columns-1)*penGapColumns)/columns))
	rowCount := (len(targets) + columns - 1) / columns
	grid := grid{
		columns: columns, penWidth: penWidth,
		heights: make([]int, len(targets)), offsets: make([]int, len(targets)), rowHeights: make([]int, rowCount),
	}
	minimumHeight := 0
	for i, configured := range targets {
		minimumHeight = max(minimumHeight, penHeight(max(1, len(statuses[configured.Name].Agents)), penWidth))
		row, column := i/columns, i%columns
		grid.offsets[i] = []int{2, 0, 3, 1}[(column+row)%4]
	}
	availableRows := max(0, availableHeight-royalSceneRows)
	maximumPenHeight := availableRows
	penHeight := max(preferredPenRows, min(minimumHeight, maximumPenHeight))
	for i := range grid.heights {
		grid.heights[i] = penHeight
	}
	maximumOffset := max(0, availableRows-penHeight)
	for i := range grid.offsets {
		grid.offsets[i] = min(grid.offsets[i], maximumOffset)
	}
	for row := range grid.rowHeights {
		maximumOffset := 0
		for i := row * columns; i < min((row+1)*columns, len(grid.offsets)); i++ {
			maximumOffset = max(maximumOffset, grid.offsets[i])
		}
		grid.rowHeights[row] = penHeight + maximumOffset + penGapRows
	}
	visibleRows := 0
	used := 0
	for visibleRows < rowCount && used+grid.rowHeights[visibleRows]-penGapRows <= availableRows {
		used += grid.rowHeights[visibleRows]
		visibleRows++
	}
	grid.visibleRows = visibleRows
	maximumFirst := max(0, rowCount-visibleRows)
	grid.firstRow = max(0, min(scrollRow, maximumFirst))
	if rowCount == 0 {
		grid.visibleRows = 0
	}
	return grid
}

func (grid grid) roadCenters(row, width int) []int {
	start := row * grid.columns
	end := min(start+grid.columns, len(grid.heights))
	count := end - start
	rowWidth := count*grid.penWidth + max(0, count-1)*penGapColumns
	left := max(0, (width-rowWidth)/2)
	centers := make([]int, count)
	for i := range centers {
		centers[i] = left + i*(grid.penWidth+penGapColumns) + gateCenter(grid.penWidth)
	}
	return centers
}

func (grid grid) visibleRowRange() (int, int) {
	return grid.firstRow, min(len(grid.rowHeights), grid.firstRow+grid.visibleRows)
}

func (p *State) scroll(grid grid, rows int) {
	maximumFirst := max(0, len(grid.rowHeights)-grid.visibleRows)
	p.scrollRow = max(0, min(p.scrollRow+rows, maximumFirst))
}

func (p *State) scrollPage(grid grid, direction int) {
	p.scroll(grid, direction*max(1, grid.visibleRows))
}

func pageLabel(grid grid, targetCount int) string {
	if targetCount == 0 || grid.visibleRows == 0 {
		return ""
	}
	if grid.visibleRows >= len(grid.rowHeights) {
		return ""
	}
	first := grid.firstRow*grid.columns + 1
	last := min(targetCount, (grid.firstRow+grid.visibleRows)*grid.columns)
	return fmt.Sprintf("%d–%d/%d", first, last, targetCount)
}

func contentWidth(width int) int {
	return max(sheepAreaOrigin+sheepColumns+fenceColumns, width-2)
}

func gateColumn(width int) int {
	return max(fenceColumns, min(width-fenceColumns-gateColumns, width*3/4-gateColumns/2))
}

func gateCenter(width int) int {
	return gateColumn(width) + gateColumns/2
}

func penHeight(count, width int) int {
	columns, _ := grazingGrid(width)
	rows := max(1, (count+columns-1)/columns)
	return top + fenceRows + rows*(sheepBodyRows+2) + workyardRows(count, width) + fenceRows
}

func workyardRows(count, width int) int {
	columns, _ := workyardGrid(width)
	return max(1, (count+columns-1)/columns) * sheepBodyRows
}

func grazingGrid(width int) (int, int) {
	available := max(sheepColumns, width-sheepAreaOrigin-fenceColumns)
	columns := max(1, available/(sheepColumns+4))
	return columns, max(sheepColumns, available/columns)
}

func workyardGrid(width int) (int, int) {
	available := max(sheepColumns, width-fenceColumns*2)
	columns := max(1, available/(sheepColumns+2))
	return columns, max(sheepColumns, available/columns)
}
