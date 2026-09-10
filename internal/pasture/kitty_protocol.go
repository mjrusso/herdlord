package pasture

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func kittyUploadSprite(imageID int, content []byte) string {
	data := base64.StdEncoding.EncodeToString(content)
	var out strings.Builder
	for offset := 0; offset < len(data); offset += 4096 {
		end := min(offset+4096, len(data))
		more := end < len(data)
		if offset == 0 {
			fmt.Fprintf(&out, "\x1b_Ga=t,f=100,i=%d,q=2,m=%d;%s\x1b\\", imageID, boolInt(more), data[offset:end])
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d,q=2;%s\x1b\\", boolInt(more), data[offset:end])
		}
	}
	return out.String()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func positionKittyLayer(layer string, column, row int) string {
	var out strings.Builder
	if row > 0 {
		fmt.Fprintf(&out, "\x1b[%dB", row)
	}
	if column > 0 {
		fmt.Fprintf(&out, "\x1b[%dC", column)
	}
	out.WriteString(layer)
	if column > 0 {
		fmt.Fprintf(&out, "\x1b[%dD", column)
	}
	if row > 0 {
		fmt.Fprintf(&out, "\x1b[%dA", row)
	}
	return out.String()
}

type placement struct {
	column  int
	row     int
	columns int
	rows    int
	z       int
}

type sourceRect struct {
	x      int
	y      int
	width  int
	height int
}

func writeImagePlacement(out *strings.Builder, imageID int, placementID uint32, destination placement) {
	writeImagePlacementSource(out, imageID, placementID, destination, sourceRect{})
}

func writeImagePlacementSource(out *strings.Builder, imageID int, placementID uint32, destination placement, source sourceRect) {
	var command strings.Builder
	fmt.Fprintf(&command, "\x1b_Ga=p,i=%d,p=%d", imageID, placementID)
	if source.width > 0 && source.height > 0 {
		fmt.Fprintf(&command, ",x=%d,y=%d,w=%d,h=%d", source.x, source.y, source.width, source.height)
	}
	fmt.Fprintf(&command, ",c=%d,r=%d,z=%d,C=1,q=2;\x1b\\", destination.columns, destination.rows, destination.z)
	out.WriteString(positionKittyLayer(command.String(), destination.column, destination.row))
}
