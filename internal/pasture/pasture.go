package pasture

import (
	"errors"
	"hash/fnv"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
)

const (
	sheepColumns        = 8
	sheepRows           = 4
	sheepBodyRows       = sheepRows + 2
	shepherdRows        = 5
	grassTileColumns    = 16
	grassTileRows       = 8
	grassTilePixels     = 256
	fenceColumns        = 2
	fenceRows           = 2
	fenceTileColumns    = 8
	fenceTileRows       = 8
	targetSignColumns   = 20
	targetSignRows      = 4
	castleColumns       = 24
	castleRows          = 7
	castleSourceX       = 23
	castleSourceY       = 29
	castleSourceWidth   = 335
	castleSourceHeight  = 126
	lordColumns         = 8
	lordRows            = 5
	royalSceneRows      = 12
	roadColumns         = 7
	royalRoadRows       = 2
	penGapColumns       = 2
	penGapRows          = 1
	preferredPenColumns = 44
	maximumPenColumns   = 56
	preferredPenRows    = 22
	gateColumns         = 6
	top                 = targetSignRows
	sheepAreaOrigin     = fenceColumns + sheepColumns + 3
	frameTime           = 400 * time.Millisecond
	controlLifetime     = 50 * time.Millisecond
)

const (
	MinimumViewportWidth  = sheepAreaOrigin + sheepColumns + fenceColumns + 2
	MinimumViewportHeight = royalSceneRows + preferredPenRows
)

type TickMsg struct{ Generation uint64 }

type ControlExpiryMsg struct{ token uint64 }

type phase uint8

const (
	present phase = iota
	entering
	relocating
	dying
)

const (
	deathWobbleEnd      = 2
	deathFallenEnd      = 4
	deathAnimationSteps = 6
	ambientWalkInterval = 50
	chatInterval        = 50
)

type sheep struct {
	x            int
	y            int
	dx           int
	dy           int
	destinationX int
	destinationY int
	ambientSteps int
	phase        phase
	phaseStep    int
	workSlot     int
	agent        herdr.Agent
}

type shepherd struct {
	x            int
	y            int
	destinationX int
	destinationY int
}

type State struct {
	visible        bool
	step           uint64
	generation     uint64
	width          int
	scrollRow      int
	sheep          map[fleet.AgentKey]sheep
	shepherds      map[string]shepherd
	missing        map[fleet.AgentKey]int
	ready          bool
	renderer       renderer
	control        string
	controlToken   uint64
	controlPending bool
	frameInput     pastureFrameInput
	frameGrid      grid
	frameReady     bool
	display        Display
	displayReady   bool
	bubbles        map[bubbleKey]bubble
	bubbleCooldown map[bubbleKey]uint64
}

type pastureFrameInput struct {
	width, height int
	targets       []pastureTargetInput
}

type pastureTargetInput struct {
	name   string
	state  poll.State
	agents []herdr.Agent
}

func (input pastureFrameInput) matches(frame Frame) bool {
	if input.width != frame.Viewport.Width || input.height != frame.Viewport.Height || len(input.targets) != len(frame.Snapshot.Targets) {
		return false
	}
	for i, configured := range frame.Snapshot.Targets {
		status := frame.Snapshot.Statuses[configured.Name]
		current := input.targets[i]
		if current.name != configured.Name || current.state != status.State || !slices.Equal(current.agents, status.Agents) {
			return false
		}
	}
	return true
}

func capturePastureFrame(frame Frame) pastureFrameInput {
	input := pastureFrameInput{width: frame.Viewport.Width, height: frame.Viewport.Height, targets: make([]pastureTargetInput, len(frame.Snapshot.Targets))}
	for i, configured := range frame.Snapshot.Targets {
		status := frame.Snapshot.Statuses[configured.Name]
		input.targets[i] = pastureTargetInput{name: configured.Name, state: status.State, agents: slices.Clone(status.Agents)}
	}
	return input
}

type Renderer uint8

const (
	ASCII Renderer = iota
	Kitty
)

func New(selected Renderer) *State {
	return &State{
		sheep:          make(map[fleet.AgentKey]sheep),
		shepherds:      make(map[string]shepherd),
		missing:        make(map[fleet.AgentKey]int),
		renderer:       rendererFor(selected),
		bubbles:        make(map[bubbleKey]bubble),
		bubbleCooldown: make(map[bubbleKey]uint64),
	}
}

type grid struct {
	columns     int
	penWidth    int
	heights     []int
	offsets     []int
	rowHeights  []int
	firstRow    int
	visibleRows int
}

type renderer interface {
	render(Scene) string
	enter() (string, error)
	leave() string
}

type asciiRenderer struct{}

func kittyGraphicsAvailable() bool {
	if os.Getenv("HERDLORD_KITTY") == "1" {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM"))
	termProgram := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	return os.Getenv("KITTY_WINDOW_ID") != "" ||
		strings.Contains(term, "kitty") ||
		strings.Contains(term, "ghostty") ||
		termProgram == "ghostty"
}

func DetectRenderer() Renderer {
	switch strings.ToLower(os.Getenv("HERDLORD_RENDERER")) {
	case "ascii":
		return ASCII
	case "kitty":
		return Kitty
	}
	if kittyGraphicsAvailable() {
		return Kitty
	}
	return ASCII
}

func rendererFor(selected Renderer) renderer {
	if selected == Kitty {
		return newKittyRenderer()
	}
	return asciiRenderer{}
}

func kittyRendererAvailable() bool {
	return strings.EqualFold(os.Getenv("HERDLORD_RENDERER"), "kitty") || kittyGraphicsAvailable()
}

var (
	ErrKittyUnavailable  = errors.New("kitty graphics unavailable")
	ErrNoTargets         = errors.New("pasture requires at least one target")
	ErrViewportTooNarrow = errors.New("pasture viewport is too narrow")
)

type Viewport struct {
	Width  int
	Height int
}

// Frame supplies borrowed, read-only state for one pasture operation. State does not retain Snapshot.
type Frame struct {
	Snapshot fleet.Snapshot
	Viewport Viewport
}

type Display struct {
	View      string
	PageLabel string
}

func (p *State) Visible() bool {
	return p.visible
}

func (p *State) Generation() uint64 {
	return p.generation
}

func (p *State) Open(frame Frame) error {
	if len(frame.Snapshot.Targets) == 0 {
		p.Close()
		return ErrNoTargets
	}
	if frame.Viewport.Width > 0 && frame.Viewport.Width < MinimumViewportWidth {
		p.Close()
		return ErrViewportTooNarrow
	}
	p.visible = true
	p.generation++
	p.step = 0
	p.width = 0
	p.scrollRow = 0
	p.ready = false
	p.frameReady = false
	p.displayReady = false
	clear(p.sheep)
	clear(p.shepherds)
	clear(p.missing)
	clear(p.bubbles)
	clear(p.bubbleCooldown)
	p.Sync(frame)
	control, err := p.renderer.enter()
	if err != nil {
		p.renderer = asciiRenderer{}
		return err
	}
	p.queueControl(control)
	return nil
}

func (p *State) Close() {
	if !p.visible {
		return
	}
	p.visible = false
	p.generation++
	clear(p.bubbles)
	p.queueControl(p.renderer.leave())
	p.frameReady = false
	p.displayReady = false
}

func (p *State) ToggleRenderer() error {
	leave := p.renderer.leave()
	switch p.renderer.(type) {
	case *kittyRenderer:
		p.renderer = asciiRenderer{}
	default:
		if !kittyRendererAvailable() {
			return ErrKittyUnavailable
		}
		p.renderer = newKittyRenderer()
	}
	enter, err := p.renderer.enter()
	if err != nil {
		p.renderer = asciiRenderer{}
		p.queueControl(leave)
		p.displayReady = false
		return err
	}
	p.queueControl(leave + enter)
	p.displayReady = false
	return nil
}

func Tick(generation uint64) tea.Cmd {
	return tea.Tick(frameTime, func(time.Time) tea.Msg {
		return TickMsg{Generation: generation}
	})
}

func (p *State) Tick(frame Frame) {
	p.Sync(frame)
	p.step++
	p.maybeChatter(frame.Snapshot)
	p.advance(frame.Snapshot, p.frameGrid)
	p.displayReady = false
}

func (p *State) Sync(frame Frame) {
	if !p.visible {
		return
	}
	if len(frame.Snapshot.Targets) == 0 || (frame.Viewport.Width > 0 && frame.Viewport.Width < MinimumViewportWidth) {
		p.Close()
		return
	}
	if p.frameReady && p.frameInput.matches(frame) {
		return
	}
	p.frameGrid = p.layout(frame)
	p.syncLayout(frame.Snapshot, p.frameGrid.penWidth, p.frameGrid.heights)
	p.frameInput = capturePastureFrame(frame)
	p.frameReady = true
	p.displayReady = false
}

func (p *State) Display(frame Frame) Display {
	p.Sync(frame)
	if !p.visible {
		return Display{}
	}
	if !p.displayReady {
		p.display = Display{
			View:      p.renderer.render(p.buildScene(frame.Snapshot, contentWidth(frame.Viewport.Width), frame.Viewport.Height, p.frameGrid)),
			PageLabel: pageLabel(p.frameGrid, len(frame.Snapshot.Targets)),
		}
		p.displayReady = true
	}
	return p.display
}

func (p *State) Control() string {
	return p.control
}

func (p *State) queueControl(control string) {
	if control == "" {
		return
	}
	p.control += control
	p.controlToken++
	p.controlPending = true
}

// ControlExpiry keeps terminal commands available long enough for Bubble Tea's renderer to flush them.
func (p *State) ControlExpiry() tea.Cmd {
	if !p.controlPending {
		return nil
	}
	p.controlPending = false
	token := p.controlToken
	return tea.Tick(controlLifetime, func(time.Time) tea.Msg {
		return ControlExpiryMsg{token: token}
	})
}

func (p *State) ExpireControl(msg ControlExpiryMsg) {
	if msg.token == p.controlToken {
		p.control = ""
	}
}

func (p *State) Scroll(frame Frame, rows int) {
	p.scroll(p.layout(frame), rows)
	p.frameReady = false
	p.displayReady = false
}

func (p *State) ScrollPage(frame Frame, direction int) {
	p.scrollPage(p.layout(frame), direction)
	p.frameReady = false
	p.displayReady = false
}

func (p *State) ScrollHalfPage(frame Frame, direction int) {
	grid := p.layout(frame)
	p.scroll(grid, direction*max(1, grid.visibleRows/2))
	p.frameReady = false
	p.displayReady = false
}

func (p *State) ScrollHome(frame Frame) {
	grid := p.layout(frame)
	p.scroll(grid, -len(grid.rowHeights))
	p.frameReady = false
	p.displayReady = false
}

func (p *State) ScrollEnd(frame Frame) {
	p.scroll(p.layout(frame), len(frame.Snapshot.Targets))
	p.frameReady = false
	p.displayReady = false
}

func (p *State) Announce(text string) {
	p.emitLordBubble(text, bubbleNormal)
}

func lordColumn(width int) int {
	_, lord := commandStationColumns(width, min(castleColumns, width), lordColumns, 2)
	return lord
}

func commandStationColumns(width, castleWidth, lordWidth, gap int) (int, int) {
	total := castleWidth + gap + lordWidth
	if total <= width {
		castle := (width - total) / 2
		return castle, castle + castleWidth + gap
	}
	castle := max(0, (width-castleWidth)/2)
	return castle, max(0, width-lordWidth)
}

func targetNeedsAttention(status poll.TargetStatus) bool {
	if agentsHaveStatus(status.Agents, "blocked") {
		return true
	}
	switch status.State {
	case poll.OK, poll.Newer, poll.Checking, poll.Paused:
		return false
	default:
		return true
	}
}

func agentsHaveStatus(agents []herdr.Agent, wanted string) bool {
	for _, agent := range agents {
		if agent.Status == wanted {
			return true
		}
	}
	return false
}

func agentSeed(key fleet.AgentKey) uint64 {
	return seed(sceneObjectID("agent-seed", key.Target, key.Pane))
}

func seed(key string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	return hash.Sum64()
}
