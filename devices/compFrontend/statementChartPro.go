// /devices/compFrontend/statementChartPro.go

package compFrontend

// statementChartPro.go — Dual device: backend identity node + frontend
// multi-series real-time chart.
//
// English:
//
//	The ChartPro is a frontend display device. The backend node exists
//	purely for identity and configuration — it has no wireable
//	connectors (decision §2.1 of CHARTPRO_REFACTOR.md). The frontend
//	element renders the chart and accepts live data via LiveUpdate.
//
// Data flow:
//
//	hardware ──webhook──▶ server ──WebSocket──▶ live.Client
//	                                                  │
//	                                                  ▼
//	                                          LiveUpdate(port, value)
//	                                                  │
//	                                                  ▼
//	                                          ChartPro.addPoint
//
//	Per series, the ChartPro maintains a ring buffer of (value,
//	timestamp) pairs. Three payload formats are accepted (§4.5 of
//	CHARTPRO_REFACTOR.md):
//
//	  - bare number "23.5"               → ts = receive time (legacy)
//	  - {"v":23.5,"t":45123}             → t  = ms since hardware boot
//	  - {"v":23.5,"ts":1700000000123}    → ts = epoch ms absolute
//
//	When `t` is used, the client maintains an unwrap state per series:
//	the first packet calibrates `tBootReal = now - t`; subsequent
//	retrograde packets (t < lastT) recalibrate and signal a RESET event
//	for the operator. No distinction is made between millis() overflow
//	and hardware reboot — they are presented to the user identically
//	(decision §6.4).
//
// Events on the timeline:
//
//	Two kinds of discontinuity markers may appear over the chart:
//	  - RESET (amber):  `t` went backwards. Hardware reset or millis()
//	                    overflow.
//	  - FAIL  (red):    WebSocket reconnected after a drop. The live
//	                    Client invokes a callback registered via
//	                    OnReconnect. A FAIL badge also appears in the
//	                    top-right corner for KFailBadgeFadeMs ms.
//
// Two independent locks:
//
//	`lockLayout` — bocks the frontend operator from moving, resizing
//	               or altering the visual.
//	`lockSend`   — blocks SendValue (no-op on ChartPro, present for
//	               consistency with other compFrontend devices).
//
// Português:
//
//	Display de gráfico multi-série em tempo real. Backend só tem
//	identidade e Inspect; sem conectores wireáveis. Aceita timestamps
//	no payload para preservar timeline em caso de perdas. Marcas RESET
//	(âmbar) e FAIL (vermelho) sinalizam descontinuidades. Dois flags
//	independentes: lockLayout (trava interação visual) e lockSend
//	(reservado para componentes interativos).

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesDevice"
	"github.com/helmutkemper/iotmakerio/rulesFrontend"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
	"github.com/helmutkemper/iotmakerio/rulesZIndex"
	"github.com/helmutkemper/iotmakerio/scene"
	"github.com/helmutkemper/iotmakerio/scenegraph"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
	"github.com/helmutkemper/iotmakerio/utilsDraw"
	"github.com/helmutkemper/iotmakerio/utilsText"
	"github.com/helmutkemper/iotmakerio/wire"
)

// ─── Local constants not promoted to rulesFrontend ──────────────────────────
//
// Event kinds and payload kinds are internal-only enums tied to the
// algorithm in this file. They do not belong in rulesFrontend because
// rulesFrontend is a "rule book of values"; these are types.

const (
	// eventKindReset is set on a chartProEvent when the hardware `t`
	// retrograded. Rendered as an amber vertical line.
	eventKindReset = "reset"

	// eventKindFail is set when the WebSocket reconnected after a drop.
	// Rendered as a red vertical line plus a temporary corner badge.
	eventKindFail = "fail"
)

const (
	// payloadKindLegacy means the payload was a bare number (or string
	// containing a number). No timestamp came from the hardware; the
	// receive time is used.
	payloadKindLegacy = iota

	// payloadKindRelative means the payload was {v, t} where t is ms
	// since hardware boot. Unwrap algorithm applies.
	payloadKindRelative

	// payloadKindAbsolute means the payload was {v, ts} where ts is an
	// epoch ms timestamp. Used directly without unwrap.
	payloadKindAbsolute
)

// kEventRetention is how many recent events to keep per chart. Older
// events fall off when this limit is exceeded. Set generously — events
// are cheap and dropping them gives the operator a worse picture.
const kEventRetention = 64

// ─── Type definitions ───────────────────────────────────────────────────────

// chartProDataPoint is a single sample in a series buffer.
type chartProDataPoint struct {
	// Value is the raw measurement reported by the hardware.
	Value float64

	// Timestamp is the absolute epoch ms at which this sample is
	// believed to have been measured. May be receive time (legacy
	// payloads), an absolute ts (payloadKindAbsolute), or computed
	// from the unwrap state (payloadKindRelative).
	Timestamp int64
}

// chartProAlertZone is a horizontal band painted behind the data,
// communicating thresholds (e.g. "anything in this band is critical").
type chartProAlertZone struct {
	MinY    float64
	MaxY    float64
	Color   string
	Opacity int
	Label   string
	Enabled bool
}

// chartProSeries is one of the up-to-KSeriesMax series rendered.
type chartProSeries struct {
	Label     string
	Color     string
	Buffer    []chartProDataPoint
	GlowLine  bool
	YAxis     string
	ChartType string // KChartTypeLine or KChartTypeScatter

	// ── Unwrap state for payloadKindRelative ────────────────────────────
	//
	// Each series has independent state because different series may
	// originate from different hardware modules, each with its own
	// millis() clock and reboot history.

	// lastT is the most recent `t` value received from the hardware
	// for this series. Used to detect retrograde events.
	lastT uint64

	// tBootReal is the calibrated epoch ms at which the hardware's
	// `t` would have been zero. Recalibrated on the first packet and
	// on every detected retrograde event.
	tBootReal int64

	// hasUnwrapState marks whether tBootReal has been calibrated.
	// False until the first payloadKindRelative packet for this series.
	hasUnwrapState bool
}

// chartProEvent is a timeline annotation (RESET or FAIL).
type chartProEvent struct {
	Kind      string // eventKindReset or eventKindFail
	Timestamp int64  // absolute epoch ms when it occurred
}

// parsedPayload is the result of parseLivePayload. The fields used
// depend on Kind.
type parsedPayload struct {
	// val is always set: the measurement value.
	val float64

	// kind tells the caller how to interpret t/ts.
	kind int

	// t is the raw `t` from a payloadKindRelative payload.
	t uint64

	// ts is the absolute epoch ms timestamp, either from
	// payloadKindAbsolute or computed (legacy = receive time).
	ts int64
}

// StatementChartPro is the device implementation. Composed of two
// sprite elements (backend identity + frontend chart), shared state,
// and the unwrap/event/throttle machinery.
type StatementChartPro struct {
	// ── Sprite refs ──────────────────────────────────────────────────────
	backendStage  sprite.Stage
	frontendStage sprite.Stage
	backendElem   sprite.Element
	frontendElem  sprite.Element

	// ── Identity ─────────────────────────────────────────────────────────
	name        string
	id          string
	label       string
	initialized bool

	// ── Layout state (interaction) ───────────────────────────────────────
	selected          bool
	selectLocked      bool
	dragEnabled       bool
	dragLocked        bool
	resizeLocked      bool
	pendingDragEnable *bool

	// ── Size ─────────────────────────────────────────────────────────────
	backendWidth   rulesDensity.Density
	backendHeight  rulesDensity.Density
	frontendWidth  rulesDensity.Density
	frontendHeight rulesDensity.Density

	// ── Injected helpers ─────────────────────────────────────────────────
	resizerButton   block.ResizeButton
	backendCtxMenu  *contextMenu.Controller
	frontendCtxMenu *contextMenu.Controller
	wireMgr         *wire.Manager // kept for API parity; never used to register
	canvasEl        js.Value
	gridAdjust      grid.Adjust
	tooltipEl       js.Value

	// ── Series / data ────────────────────────────────────────────────────
	series        []chartProSeries
	seriesCount   int
	bufferSize    int
	timeWindowSec int
	autoScale     bool
	minY          float64
	maxY          float64
	gridColor     string
	gridOpacity   int
	gridWidth     float64
	chartTitle    string
	chartUnit     string
	showStats     bool
	showTimestamp bool
	showLegend    bool
	lastUpdateTs  int64
	alertZones    [rulesFrontend.KAlertZoneMax]chartProAlertZone

	// ── Hover ────────────────────────────────────────────────────────────
	//
	// hoverIndex is mutated from two contexts: the main thread (cursor
	// hit test) and goroutines spawned from JS callbacks (LiveUpdate).
	// Protected by hoverMu. See §9.1 of CHARTPRO_REFACTOR.md.
	hoverIndex int
	hoverMu    sync.Mutex

	// ── Locks (decision §2.3) ────────────────────────────────────────────
	//
	// Two independent flags. lockLayout blocks the frontend operator
	// from moving/resizing. lockSend blocks outbound data — kept for
	// schema consistency with other compFrontend devices even though
	// ChartPro never sends.
	lockLayout bool
	lockSend   bool

	// ── Pause / clear (frontend menu actions §4) ─────────────────────────
	//
	// paused: when true, LiveUpdate still appends to the buffer but
	// recacheFrontend is suppressed. Resume re-renders. The buffer is
	// never paused — only the visual is.
	paused bool

	// ── Timeline events (RESET/FAIL markers) ─────────────────────────────
	events []chartProEvent

	// ── FAIL badge in the corner ─────────────────────────────────────────
	//
	// failBadgeUntilMs is the epoch ms at which the corner badge should
	// stop being rendered. Set on reconnect; 0 means no badge.
	failBadgeUntilMs int64

	// failBadgeTs is the epoch ms of the reconnect itself — used to
	// stamp the badge text ("FAIL · HH:MM:SS"). Lets the operator
	// glance at the badge and immediately know when the drop happened,
	// even after they look away and come back. Cleared by Clear().
	failBadgeTs int64

	// ── Gap detection threshold ──────────────────────────────────────────
	//
	// gapThresholdMs is the time delta (in ms) between two consecutive
	// samples above which the segment connecting them is drawn in the
	// "gap" style (grey, dashed). Zero disables the feature — all
	// segments use the series colour. Default is set in Init() from
	// rulesFrontend.KGapDefaultThresholdMs.
	//
	// Distinct from RESET (firmware retrograde detected by the server)
	// and FAIL (WebSocket reconnect callback): gap detection is a pure
	// client-side heuristic over sample cadence. Catches outages that
	// the FAIL handler misses.
	gapThresholdMs int

	// ── Render throttle ──────────────────────────────────────────────────
	//
	// lastRenderMs is the epoch ms of the last recacheFrontend call.
	// Used to throttle re-renders at high data rates.
	lastRenderMs int64

	// ── Callbacks ────────────────────────────────────────────────────────
	sceneNotify func()
	onRemove    func(id string)
	iconStatus  int
}

// ─── Dependency injection ───────────────────────────────────────────────────

func (e *StatementChartPro) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementChartPro) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementChartPro) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementChartPro) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementChartPro) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller that handles backend
// stage context menus. Must be called before Init().
//
// Português: Injeta o controller do menu de contexto do stage backend.
func (e *StatementChartPro) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}

// SetFrontendContextMenu injects the controller for the frontend stage.
// May be left nil in backend-only compile targets.
//
// Português: Injeta o controller do menu de contexto do stage frontend.
func (e *StatementChartPro) SetFrontendContextMenu(c *contextMenu.Controller) {
	e.frontendCtxMenu = c
}

func (e *StatementChartPro) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementChartPro) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// SetReconnectRegistrar wires this device into the live.Client's
// reconnect notification system. The factory passes
// liveClient.OnReconnect as the argument; any function with that
// signature works for testing.
//
// When the WebSocket recovers after a drop, the registered callback
// adds a FAIL event to the timeline and lights up the corner badge
// for KFailBadgeFadeMs ms.
//
// Português: Registra um callback no live.Client para ser notificado
// quando o WebSocket recupera depois de uma queda. Marca FAIL na
// timeline e acende o badge no canto.
func (e *StatementChartPro) SetReconnectRegistrar(register func(callback func())) {
	if register == nil {
		return
	}
	register(e.handleLiveReconnect)
}

// handleLiveReconnect is the callback invoked by live.Client on
// recovery. Internal — exposed via SetReconnectRegistrar.
func (e *StatementChartPro) handleLiveReconnect() {
	now := time.Now().UnixMilli()
	log.Printf("[ChartPro/live] %s: WebSocket reconnected — adding FAIL marker", e.id)
	e.addEvent(eventKindFail, now)
	e.failBadgeUntilMs = now + rulesFrontend.KFailBadgeFadeMs
	e.failBadgeTs = now
	if !e.paused {
		go e.recacheFrontend()
	}
}

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// Append makes both stage elements visible again after a previous
// hide. Called by the scene loader when a saved device is restored.
func (e *StatementChartPro) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

// Remove tears down both stage elements and releases the tooltip DOM
// node. Safe to call multiple times.
func (e *StatementChartPro) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	// Backend never registered with wireMgr anymore (decision §2.1),
	// but call UnregisterElement just in case the device was loaded
	// from a legacy scene that did register. It is a no-op when no
	// connectors are registered for this ID.
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	if e.backendElem != nil {
		e.backendElem.SetVisible(false)
		elem := e.backendElem
		e.backendElem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(false)
		elem := e.frontendElem
		e.frontendElem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
	if e.tooltipEl.Truthy() && e.tooltipEl.Get("parentNode").Truthy() {
		e.tooltipEl.Get("parentNode").Call("removeChild", e.tooltipEl)
	}
}

// SetName assigns a unique device name derived from a base string.
func (e *StatementChartPro) SetName(n string) { e.name = rulesSequentialId.GetIdFromBase(n) }

// Get is part of the Device interface. ChartPro does not use the
// returned SVG node directly — both stages own their own elements.
func (e *StatementChartPro) Get() *html.TagSvg { return nil }

// ─── Position helpers ───────────────────────────────────────────────────────

func (e *StatementChartPro) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}

func (e *StatementChartPro) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementChartPro) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}

func (e *StatementChartPro) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}

func (e *StatementChartPro) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}

func (e *StatementChartPro) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}

func (e *StatementChartPro) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// ─── Backend geometry helpers ───────────────────────────────────────────────
//
// The backend node lays out N port labels vertically. Each port label
// occupies KBackendPortSpacing px. With KBackendPadTop and
// KBackendPadBottom around the stack, total body height = top pad +
// N * spacing + bottom pad. Total node height adds a label strip
// below (KLabelHeight).
//
// Before the §2.1 refactor each port had a wireable connector drawn
// as a coloured circle on the left edge, with the port name (s0, s1)
// and an axis tag [L]/[R] beside it. After the refactor the connectors
// became decorative dots; after the follow-up "tirar s0 do backend"
// decision the whole port column was removed entirely.
//
// The body HEIGHT is still proportional to seriesCount via
// KBackendPortSpacing — that empty space stays reserved so a 3-series
// chart looks the same size as a 1-series one would have with three
// port rows. The horizontal area where the dots and labels used to
// live stays blank on the left; the type tag and ID continue to be
// right-aligned and unchanged.
//
// Português: A altura do body continua proporcional ao número de
// séries para preservar o tamanho visual. O espaço onde antes
// ficavam os dots/labels permanece vazio à esquerda — sem nada
// desenhado, mas reservado.

func (e *StatementChartPro) backendBodyHeight() float64 {
	return rulesFrontend.KBackendPadTop +
		float64(e.seriesCount)*rulesFrontend.KBackendPortSpacing +
		rulesFrontend.KBackendPadBottom
}

func (e *StatementChartPro) backendTotalHeight() float64 {
	return e.backendBodyHeight() + float64(rulesDevice.KLabelHeight)
}

func seriesPortName(i int) string { return fmt.Sprintf("s%d", i) }

func (e *StatementChartPro) getSeriesColor(i int) string {
	if i < len(e.series) && e.series[i].Color != "" {
		return e.series[i].Color
	}
	return rulesFrontend.KSeriesPalette[i%rulesFrontend.KSeriesMax]
}

// ─── Backend SVG ────────────────────────────────────────────────────────────
//
// The backend SVG shows:
//
//	┌──────────────────────────┐
//	│              CHART PRO   │   ← type tag (right-aligned)
//	│                          │   ← empty rows, one per series
//	│                          │     (the vertical space is
//	│                          │      reserved by backendBodyHeight)
//	│                          │
//	└──────────────────────────┘
//	     myChart_1                 ← device label (id)
//
// The port column that used to live on the left (decorative dot +
// "sN" + axis tag) was removed entirely per follow-up to §2.1. The
// space stays reserved vertically so a multi-series chart visually
// reads as a bigger device, but nothing is drawn in that area. The
// firmware author maps series to ports via the Inspect overlay
// (Series tab) and the payload example field in Properties.
//
// Português: A coluna de portas (dot + nome + eixo) foi removida.
// O espaço continua reservado verticalmente. Quem precisa do nome
// técnico da porta (s0, s1, ...) consulta o Inspect.

func (e *StatementChartPro) renderBackendSVG() string {
	w := float64(rulesFrontend.KBackendWidth)
	bodyH := e.backendBodyHeight()
	totalH := e.backendTotalHeight()
	bw := rulesDevice.KDeviceBorderWidth
	accent := e.getSeriesColor(0)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, bodyH-bw,
		rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, accent, bw)
	svg += fmt.Sprintf(`<text x="%.1f" y="14" font-family="%s" font-size="%d" fill="%s" text-anchor="end" font-weight="bold">CHART PRO</text>`,
		w-12, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag,
		rulesDevice.KColorDeviceTextMuted)

	// Port column intentionally not drawn. The body height (see
	// backendBodyHeight) keeps growing with seriesCount so the device
	// retains its previous proportions — only the visible markings
	// were removed.

	// Lock icon if lockLayout is on. Visible only in the backend SVG
	// because backend is where the operator with full access reads
	// "this device is locked from frontend tampering".
	if e.lockLayout {
		svg += fmt.Sprintf(`<text x="%.1f" y="12" font-family="%s" font-size="10" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-4, rulesDevice.KDeviceFontFamily)
	}

	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, bodyH+3, displayLabel)
	svg += `</svg>`
	return svg
}

// ─── Y axis helpers ─────────────────────────────────────────────────────────

// yAxisRange returns (min, max) for the requested axis. When autoScale
// is on, the range covers all data points across all series assigned
// to that axis, with a 10% padding. When off, returns (e.minY, e.maxY).
func (e *StatementChartPro) yAxisRange(axis string) (float64, float64) {
	if !e.autoScale {
		return e.minY, e.maxY
	}
	hasData := false
	mn, mx := math.MaxFloat64, -math.MaxFloat64
	for _, s := range e.series {
		if s.YAxis != axis {
			continue
		}
		for _, pt := range s.Buffer {
			hasData = true
			if pt.Value < mn {
				mn = pt.Value
			}
			if pt.Value > mx {
				mx = pt.Value
			}
		}
	}
	if !hasData {
		return 0, 100
	}
	pad := (mx - mn) * 0.1
	if pad < 1 {
		pad = 1
	}
	return mn - pad, mx + pad
}

// hasRightAxis returns true when at least one series is assigned to
// the right Y axis. Affects right padding in the frontend SVG.
func (e *StatementChartPro) hasRightAxis() bool {
	for _, s := range e.series {
		if s.YAxis == rulesFrontend.KYAxisRight {
			return true
		}
	}
	return false
}

// visibleTimeRange returns (startTs, endTs) of the visible window in
// absolute epoch ms, valid in BOTH rendering modes:
//
//   - timeWindowSec > 0 (sliding window): startTs = now - window,
//     endTs = now. Pontos mais antigos são descartados.
//
//   - timeWindowSec == 0 (last-N-samples): startTs = oldest visible
//     timestamp in any series, endTs = max(newest visible timestamp,
//     now). The chart still walks left as new samples arrive — the
//     "now" cap ensures an idle chart keeps scrolling, with the void
//     manifesting as a real time gap rather than freezing.
//
// Returns (0, 0) only when there is zero data anywhere — that branch
// is handled by the "awaiting data" fallback in renderFrontendSVG.
//
// Português: Retorna (startTs, endTs) em ambos os modos. No modo
// last-N, deriva os limites dos próprios timestamps do buffer; sem
// isso o gap detectado não tinha como ser desenhado proporcional ao
// tempo perdido (era proporcional ao índice).
func (e *StatementChartPro) visibleTimeRange() (int64, int64) {
	if e.timeWindowSec > 0 {
		now := time.Now().UnixMilli()
		return now - int64(e.timeWindowSec)*1000, now
	}
	// Last-N-samples mode: derive range from buffer extents.
	bufSize := e.bufferSize
	if bufSize < 2 {
		bufSize = rulesFrontend.KBufferDefault
	}
	var minTs, maxTs int64
	found := false
	for _, s := range e.series {
		n := len(s.Buffer)
		if n == 0 {
			continue
		}
		start := 0
		if n > bufSize {
			start = n - bufSize
		}
		for i := start; i < n; i++ {
			ts := s.Buffer[i].Timestamp
			if !found || ts < minTs {
				minTs = ts
			}
			if !found || ts > maxTs {
				maxTs = ts
			}
			found = true
		}
	}
	if !found {
		return 0, 0
	}
	now := time.Now().UnixMilli()
	if now > maxTs {
		maxTs = now
	}
	return minTs, maxTs
}

// ─── Frontend SVG ───────────────────────────────────────────────────────────
//
// The frontend SVG is supersampled by KFrontendScale: the device's
// width/height in density units are multiplied by the scale before
// drawing, which gives the renderer extra resolution and reduces
// aliasing. The browser then scales the SVG back to fit the device's
// real size.
//
// Layout (scaled coordinates):
//
//	┌─────────────────────────────────────────────┐
//	│  Title                          Latest Value│   ← KPadT
//	│ ┌─────────────────────────────────────────┐ │
//	│ │                                         │ │
//	│ │ Y                                       │ │
//	│ │ a                                       │ │
//	│ │ x      data lines / dots                │ │
//	│ │ i                                       │ │
//	│ │ s                                       │ │
//	│ │ │                                       │ │
//	│ │ └───────────────────────────────────────┘ │  ← KPadB
//	│   Min/Avg/Max stats                Last: 12:30:45 │
//	│   ●Series0  ●Series1  ●Series2 (legend if >1)   │
//	└─────────────────────────────────────────────┘
//	 KPadL ←──────── chart area ────────→ KPadR

func (e *StatementChartPro) renderFrontendSVG() string {
	w := e.frontendWidth.GetFloat() * rulesFrontend.KFrontendScale
	h := e.frontendHeight.GetFloat() * rulesFrontend.KFrontendScale
	hasRight := e.hasRightAxis()

	padL, padR, padT, padB :=
		rulesFrontend.KPadL,
		rulesFrontend.KPadR,
		rulesFrontend.KPadT,
		rulesFrontend.KPadB
	if hasRight {
		padR = rulesFrontend.KPadRWithRightAxis
	}
	chartW := w - padL - padR
	chartH := h - padT - padB

	legendH := 0.0
	if e.showLegend && e.seriesCount > 1 {
		legendH = rulesFrontend.KLegendHeight
		chartH -= legendH
	}

	leftMinY, leftMaxY := e.yAxisRange(rulesFrontend.KYAxisLeft)
	leftRangeY := leftMaxY - leftMinY
	if leftRangeY <= 0 {
		leftRangeY = 1
	}

	rightMinY, rightMaxY := 0.0, 100.0
	rightRangeY := 100.0
	if hasRight {
		rightMinY, rightMaxY = e.yAxisRange(rulesFrontend.KYAxisRight)
		rightRangeY = rightMaxY - rightMinY
		if rightRangeY <= 0 {
			rightRangeY = 1
		}
	}

	mapYLeft := func(v float64) float64 {
		return padT + chartH*(1-(v-leftMinY)/leftRangeY)
	}
	mapYRight := func(v float64) float64 {
		return padT + chartH*(1-(v-rightMinY)/rightRangeY)
	}

	startTs, endTs := e.visibleTimeRange()
	// useTimeMode flags the SLIDING-WINDOW mode (filter older points by
	// timestamp). In last-N-samples mode useTimeMode is false but
	// startTs/endTs are still valid — they come from the buffer extents,
	// and the X axis is still drawn proportional to real time. The flag
	// only changes how visible points are SELECTED (by time vs by count).
	useTimeMode := e.timeWindowSec > 0
	// hasRange means we have at least two distinct points in time to
	// project onto the X axis. When false, all data positioning is
	// suppressed (only the "awaiting data" fallback below renders).
	hasRange := endTs > startTs

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(h))
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="%.0f" ry="%.0f" fill="%s"/>`,
		int(w), int(h), rulesFrontend.KPanelCornerRadius, rulesFrontend.KPanelCornerRadius,
		rulesFrontend.KPanelBg)

	// Alert zones — behind everything else.
	for _, az := range e.alertZones {
		if !az.Enabled || az.Opacity <= 0 {
			continue
		}
		y1 := mapYLeft(az.MaxY)
		y2 := mapYLeft(az.MinY)
		if y1 > padT+chartH {
			y1 = padT + chartH
		}
		if y2 < padT {
			y2 = padT
		}
		if y1 >= y2 {
			continue
		}
		op := fmt.Sprintf("%.2f", float64(az.Opacity)/100.0)
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" opacity="%s"/>`,
			padL, y1, chartW, y2-y1, az.Color, op)
		if az.Label != "" {
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="%s" opacity="0.8" dominant-baseline="hanging">%s</text>`,
				padL+4, y1+2, rulesDevice.KDeviceFontFamily,
				rulesFrontend.KFontSizeAlertLabel, az.Color, az.Label)
		}
	}

	// Grid.
	gColor := e.gridColor
	if gColor == "" {
		gColor = rulesFrontend.KGridDefaultColor
	}
	gAlpha := e.gridOpacity
	if gAlpha == 0 {
		gAlpha = rulesFrontend.KGridDefaultOpacityPct
	}
	gStroke := e.gridWidth
	if gStroke == 0 {
		gStroke = rulesFrontend.KGridDefaultWidth
	}
	svg += fmt.Sprintf(`<g opacity="%.2f">`, float64(gAlpha)/100.0)
	for i := 0; i <= rulesFrontend.KGridHorizontalDivisions; i++ {
		y := padT + chartH*float64(i)/float64(rulesFrontend.KGridHorizontalDivisions)
		svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`,
			padL, y, w-padR, y, gColor, gStroke)
	}
	for i := 0; i <= rulesFrontend.KGridVerticalDivisions; i++ {
		x := padL + chartW*float64(i)/float64(rulesFrontend.KGridVerticalDivisions)
		svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`,
			x, padT, x, padT+chartH, gColor, gStroke)
	}
	svg += `</g>`

	// Left Y axis labels.
	for i := 0; i <= rulesFrontend.KGridHorizontalDivisions; i++ {
		y := padT + chartH*float64(i)/float64(rulesFrontend.KGridHorizontalDivisions)
		val := leftMaxY - (leftMaxY-leftMinY)*float64(i)/float64(rulesFrontend.KGridHorizontalDivisions)
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="monospace" font-size="%.0f" fill="#aaa" text-anchor="end" dominant-baseline="central">%s</text>`,
			padL-10, y, rulesFrontend.KFontSizeYAxis,
			strconv.FormatFloat(val, 'f', 0, 64))
	}

	// Right Y axis labels (when applicable).
	if hasRight {
		for i := 0; i <= rulesFrontend.KGridHorizontalDivisions; i++ {
			y := padT + chartH*float64(i)/float64(rulesFrontend.KGridHorizontalDivisions)
			val := rightMaxY - (rightMaxY-rightMinY)*float64(i)/float64(rulesFrontend.KGridHorizontalDivisions)
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="monospace" font-size="%.0f" fill="#aaa" text-anchor="start" dominant-baseline="central">%s</text>`,
				w-padR+10, y, rulesFrontend.KFontSizeYAxis,
				strconv.FormatFloat(val, 'f', 0, 64))
		}
	}

	// X axis time labels. Drawn in both modes — the X axis is always
	// proportional to time. In last-N mode the labels report the actual
	// timestamp span of the visible buffer (so the operator can read
	// "this stretch covers the last 12 seconds, with a gap around T+7s").
	if hasRange {
		for i := 0; i <= rulesFrontend.KGridVerticalDivisions; i++ {
			x := padL + chartW*float64(i)/float64(rulesFrontend.KGridVerticalDivisions)
			ts := startTs + int64(float64(i)/float64(rulesFrontend.KGridVerticalDivisions)*float64(endTs-startTs))
			t := time.UnixMilli(ts)
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="monospace" font-size="%.0f" fill="#666" text-anchor="middle">%02d:%02d:%02d</text>`,
				x, padT+chartH+18, rulesFrontend.KFontSizeXAxis,
				t.Hour(), t.Minute(), t.Second())
		}
	}

	// Data: lines or scatter dots, depending on each series' ChartType.
	//
	// The X axis is ALWAYS proportional to real time, in both modes:
	//
	//   - Sliding window (useTimeMode=true): pontos com timestamp
	//     anterior a startTs são descartados. Coordenada X é
	//     (ts - startTs) / (endTs - startTs) * chartW.
	//
	//   - Last-N-samples (useTimeMode=false): filtra por contagem
	//     (últimos bufferSize pontos do buffer). Coordenada X usa a
	//     MESMA fórmula — startTs e endTs vêm dos extremos do buffer
	//     visível, então pontos com timestamps uniformes ficam
	//     uniformemente espaçados, mas um gap consome espaço X
	//     proporcional ao tempo perdido.
	//
	// Gap detection: when two consecutive samples in a series are
	// separated by more than e.gapThresholdMs in real (timestamp)
	// time, the segment connecting them is drawn in the gap style
	// (grey, dashed) instead of the series colour. gapThresholdMs of
	// 0 disables the feature.
	hasAnyData := false
	if hasRange {
		timeRange := float64(endTs - startTs)
		for si, s := range e.series {
			n := len(s.Buffer)
			if n == 0 {
				continue
			}
			hasAnyData = true
			mapY := mapYLeft
			if s.YAxis == rulesFrontend.KYAxisRight {
				mapY = mapYRight
			}

			// plottedPoint carries everything we need to render one sample:
			// its screen coordinates AND whether the segment leading INTO
			// it crosses a gap. The gap flag is computed once during the
			// projection pass and consumed later when we emit the SVG.
			type plottedPoint struct {
				x, y      float64
				gapBefore bool
			}
			var pts []plottedPoint

			// Iteration window: in useTimeMode we walk the whole buffer
			// and filter by timestamp; in last-N mode we skip the buffer
			// prefix that exceeds bufferSize.
			var iterStart, iterEnd int
			if useTimeMode {
				iterStart, iterEnd = 0, n
			} else {
				bufSize := e.bufferSize
				if bufSize < 2 {
					bufSize = rulesFrontend.KBufferDefault
				}
				iterStart, iterEnd = 0, n
				if n > bufSize {
					iterStart = n - bufSize
				}
			}

			var prevTs int64
			first := true
			for i := iterStart; i < iterEnd; i++ {
				pt := s.Buffer[i]
				if useTimeMode && pt.Timestamp < startTs {
					continue
				}
				x := padL + float64(pt.Timestamp-startTs)/timeRange*chartW
				y := mapY(pt.Value)
				pp := plottedPoint{x: x, y: y}
				if !first && e.gapThresholdMs > 0 &&
					pt.Timestamp-prevTs > int64(e.gapThresholdMs) {
					pp.gapBefore = true
				}
				pts = append(pts, pp)
				prevTs = pt.Timestamp
				first = false
			}

			if len(pts) == 0 {
				continue
			}

			lc := e.getSeriesColor(si)

			if s.ChartType == rulesFrontend.KChartTypeScatter {
				// Scatter: each point is independent. Gap detection has
				// no visual effect (there are no segments to paint).
				for _, pp := range pts {
					if s.GlowLine {
						svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" opacity="%s"/>`,
							pp.x, pp.y,
							rulesFrontend.KScatterGlowRadius, lc, rulesFrontend.KLineGlowOpacity)
					}
					svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s"/>`,
						pp.x, pp.y, rulesFrontend.KScatterPointRadius, lc)
				}
				continue
			}

			// Line: segment the polyline at gap boundaries.
			//
			//   <polyline series-colour: pp[0..k-1]>  (no gaps inside)
			//   <line grey dashed:        pp[k-1] → pp[k]>
			//   <polyline series-colour: pp[k..m-1]>
			//
			// A segment of fewer than 2 points is skipped.
			segStart := 0
			flushSegment := func(endExclusive int) {
				if endExclusive-segStart < 2 {
					return
				}
				var sb strings.Builder
				for i := segStart; i < endExclusive; i++ {
					if i > segStart {
						sb.WriteByte(' ')
					}
					fmt.Fprintf(&sb, "%.1f,%.1f", pts[i].x, pts[i].y)
				}
				joined := sb.String()
				if s.GlowLine {
					svg += fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="%.1f" stroke-opacity="%s" stroke-linejoin="round" stroke-linecap="round"/>`,
						joined, lc, rulesFrontend.KLineGlowWidth, rulesFrontend.KLineGlowOpacity)
				}
				svg += fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="%.1f" stroke-linejoin="round" stroke-linecap="round"/>`,
					joined, lc, rulesFrontend.KLineWidth)
			}

			for i := 1; i < len(pts); i++ {
				if pts[i].gapBefore {
					flushSegment(i)
					// Grey dashed line across the gap. With X-by-time in
					// both modes this segment is now visually wide when
					// the time gap is large — exactly the "incline" the
					// operator expects to see (long flat-ish slope vs a
					// short vertical drop).
					svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f" stroke-dasharray="%s" stroke-opacity="%s" stroke-linecap="round"/>`,
						pts[i-1].x, pts[i-1].y, pts[i].x, pts[i].y,
						rulesFrontend.KGapLineColor, rulesFrontend.KGapLineWidth,
						rulesFrontend.KGapLineDashStroke, rulesFrontend.KGapLineOpacity)
					segStart = i
				}
			}
			flushSegment(len(pts))
		}
	}

	if !hasAnyData {
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#555" text-anchor="middle" dominant-baseline="central">awaiting data</text>`,
			w/2, (padT+padT+chartH)/2, rulesDevice.KDeviceFontFamily,
			rulesFrontend.KFontSizeAwaitingMsg)
	}

	// Event markers (RESET / FAIL) drawn above grid but below labels.
	e.renderEventMarkers(&svg, padL, padT, chartW, chartH, startTs, endTs)

	// Plot border.
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="none" stroke="%s" stroke-width="%.1f"/>`,
		padL, padT, chartW, chartH, rulesFrontend.KPlotBorderColor, rulesFrontend.KPlotBorderWidth)

	// Title.
	if e.chartTitle != "" {
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#ddd" font-weight="bold">%s</text>`,
			padL+8, padT+22, rulesDevice.KDeviceFontFamily,
			rulesFrontend.KFontSizeTitle, e.chartTitle)
		if e.chartUnit != "" {
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#ddd">%s</text>`,
				padL+8+float64(len(e.chartTitle))*15, padT+22, rulesDevice.KDeviceFontFamily,
				rulesFrontend.KFontSizeUnitTitle, e.chartUnit)
		}
	}

	// Latest value of series 0.
	if len(e.series) > 0 && len(e.series[0].Buffer) > 0 {
		lv := e.series[0].Buffer[len(e.series[0].Buffer)-1].Value
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="%s" text-anchor="end" font-weight="bold">%s</text>`,
			w-padR-8, padT+30, rulesDevice.KDeviceFontFamily,
			rulesFrontend.KFontSizeLatestValue, e.getSeriesColor(0),
			strconv.FormatFloat(lv, 'f', 1, 64))
		if e.chartUnit != "" {
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#999" text-anchor="end">%s</text>`,
				w-padR-8, padT+50, rulesDevice.KDeviceFontFamily,
				rulesFrontend.KFontSizeLatestUnit, e.chartUnit)
		}
	}

	// Stats.
	bottomY := padT + chartH - 6.0
	if e.showStats && hasAnyData {
		gMin, gMax, gSum, gCnt := math.MaxFloat64, -math.MaxFloat64, 0.0, 0
		for _, s := range e.series {
			for _, pt := range s.Buffer {
				gSum += pt.Value
				gCnt++
				if pt.Value < gMin {
					gMin = pt.Value
				}
				if pt.Value > gMax {
					gMax = pt.Value
				}
			}
		}
		gAvg := 0.0
		if gCnt > 0 {
			gAvg = gSum / float64(gCnt)
		}
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="monospace" font-size="%.0f" fill="#999">Min: %.1f   Avg: %.1f   Max: %.1f</text>`,
			padL+4, bottomY, rulesFrontend.KFontSizeStats, gMin, gAvg, gMax)
	}
	if e.showTimestamp && e.lastUpdateTs > 0 {
		ts := time.Unix(e.lastUpdateTs, 0)
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="monospace" font-size="%.0f" fill="#888" text-anchor="end">Last: %02d:%02d:%02d</text>`,
			w-padR-4, bottomY, rulesFrontend.KFontSizeTimestamp,
			ts.Hour(), ts.Minute(), ts.Second())
	}

	// Mode badge.
	ml := fmt.Sprintf("%d series", e.seriesCount)
	if useTimeMode {
		ml += fmt.Sprintf(" · %ds", e.timeWindowSec)
	}
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#666" text-anchor="end">%s</text>`,
		w-padR, padT+68, rulesDevice.KDeviceFontFamily,
		rulesFrontend.KFontSizeModeBadge, ml)

	// Hover crosshair + tooltips.
	e.hoverMu.Lock()
	hi := e.hoverIndex
	e.hoverMu.Unlock()
	if hi >= 0 && hasAnyData {
		e.renderHoverOverlay(&svg, padL, padR, padT, chartW, chartH,
			mapYLeft, mapYRight, w, startTs, endTs, hi)
	}

	// Legend.
	if e.showLegend && e.seriesCount > 1 {
		// Sempre 14px abaixo do X-axis label, já que o label aparece
		// em ambos os modos agora.
		ly := padT + chartH + 18.0 + 14.0
		xc := padL + 4.0
		for i := 0; i < e.seriesCount; i++ {
			c := e.getSeriesColor(i)
			l := seriesPortName(i)
			if i < len(e.series) && e.series[i].Label != "" {
				l = e.series[i].Label
				if len(l) > 10 {
					l = l[:10] + "…"
				}
			}
			svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="5" fill="%s"/>`,
				xc+5, ly, c)
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#bbb" dominant-baseline="central">%s</text>`,
				xc+14, ly, rulesDevice.KDeviceFontFamily, rulesFrontend.KFontSizeLegend, l)
			xc += 14 + float64(len(l))*10 + 16
		}
	}

	// Corner indicators. The PAUSED badge stays at the very top-right
	// (operator-initiated state, persists until cleared, deserves the
	// prime spot). The FAIL badge moves DOWN under "1 series" so it
	// doesn't overlap the big "latest value" number — that overlap
	// was the readability complaint from field feedback. FAIL also
	// carries the wall-clock time of the reconnect so the operator
	// can correlate it with whatever they were doing.
	//
	// Layout reference (top-right column, growing downward):
	//
	//   y = padT + 2   PAUSED badge (when paused)
	//   y = padT + 30  latest value (big bold number)
	//   y = padT + 50  unit (small, optional)
	//   y = padT + 68  "1 series" mode badge
	//   y = padT + 88  FAIL badge (when active)   ← new home
	now := time.Now().UnixMilli()
	if e.paused {
		e.renderCornerBadge(&svg, w, padT+2,
			rulesFrontend.KPauseIndicatorLabel,
			rulesFrontend.KPauseIndicatorBg,
			rulesFrontend.KPauseIndicatorText,
			rulesFrontend.KFontSizePauseBadge)
	} else if now < e.failBadgeUntilMs {
		// Stamp the FAIL badge with the wall-clock time of the
		// reconnect, e.g. "FAIL · 21:33:44". Operators consistently
		// asked "when did this happen" when seeing just "FAIL".
		ts := time.UnixMilli(e.failBadgeTs)
		failText := fmt.Sprintf("FAIL · %02d:%02d:%02d",
			ts.Hour(), ts.Minute(), ts.Second())
		e.renderCornerBadge(&svg, w, padT+88,
			failText,
			rulesFrontend.KFailBadgeBg,
			rulesFrontend.KFailBadgeText,
			rulesFrontend.KFontSizeFailBadge)
	}

	if e.lockLayout {
		svg += fmt.Sprintf(`<text x="%.1f" y="20" font-family="%s" font-size="%.0f" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-12, rulesDevice.KDeviceFontFamily, rulesFrontend.KFontSizeLockIcon)
	}

	svg += `</svg>`
	return svg
}

// renderCornerBadge draws a small label box anchored to the top-right
// of the frontend SVG. The yTop parameter is the absolute Y of the
// box's top edge, letting different badges (PAUSED, FAIL) sit at
// different vertical positions without colliding with the latest
// value, mode tag, or each other.
//
// Português: Desenha um pequeno rótulo no canto superior direito.
// O yTop é o Y absoluto do topo da caixa — diferentes badges
// (PAUSED, FAIL) usam Ys diferentes para não se sobreporem entre si
// nem com o valor numérico grande.
func (e *StatementChartPro) renderCornerBadge(svg *string, w, yTop float64,
	text, bg, textColor string, fontSize float64) {
	bw := float64(len(text))*11.0 + 16.0
	bx := w - bw - 12.0
	*svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="22" rx="3" fill="%s"/>`,
		bx, yTop, bw, bg)
	*svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		bx+bw/2, yTop+11, rulesDevice.KDeviceFontFamily, fontSize, textColor, text)
}

// renderEventMarkers draws RESET/FAIL vertical lines on the timeline.
// Works in BOTH modes (sliding window and last-N samples) because the
// X axis is now proportional to real time in both — the marker's X is
// just (ev.Timestamp - startTs) / (endTs - startTs) * chartW, same
// formula used for data points.
//
// Events outside the visible time range are silently skipped. When
// the chart has fewer than two points (endTs <= startTs) nothing is
// drawn — the marker would have no anchor.
//
// Português: Desenha os marcadores verticais RESET/FAIL. Funciona em
// ambos os modos agora que o eixo X é sempre proporcional ao tempo.
func (e *StatementChartPro) renderEventMarkers(svg *string, padL, padT, chartW, chartH float64,
	startTs, endTs int64) {
	if len(e.events) == 0 || endTs <= startTs {
		return
	}
	timeRange := float64(endTs - startTs)
	for _, ev := range e.events {
		if ev.Timestamp < startTs || ev.Timestamp > endTs {
			continue
		}
		x := padL + float64(ev.Timestamp-startTs)/timeRange*chartW

		var color, dash, opacity, label, bg string
		var width float64
		switch ev.Kind {
		case eventKindReset:
			color = rulesFrontend.KResetMarkColor
			dash = rulesFrontend.KResetMarkDashStroke
			opacity = rulesFrontend.KResetMarkOpacity
			width = rulesFrontend.KResetMarkWidth
			label = rulesFrontend.KResetLabelText
			bg = rulesFrontend.KResetLabelBg
		case eventKindFail:
			color = rulesFrontend.KFailMarkColor
			dash = rulesFrontend.KFailMarkDashStroke
			opacity = rulesFrontend.KFailMarkOpacity
			width = rulesFrontend.KFailMarkWidth
			label = rulesFrontend.KFailLabelText
			bg = rulesFrontend.KFailLabelBg
		default:
			continue
		}

		// Vertical line.
		*svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f" stroke-dasharray="%s" opacity="%s"/>`,
			x, padT, x, padT+chartH, color, width, dash, opacity)

		// Label box at the top.
		bw := float64(len(label))*9.0 + 12.0
		bx := x - bw/2
		by := padT + 2.0
		*svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="20" rx="3" fill="%s" opacity="%s"/>`,
			bx, by, bw, bg, opacity)
		*svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
			x, by+10, rulesDevice.KDeviceFontFamily,
			rulesFrontend.KFontSizeResetLabel, color, label)
	}
}

// renderHoverOverlay draws the crosshair and value tooltips at the
// currently-hovered X position. hi is the snapshot of hoverIndex
// captured under the mutex at call time.
func (e *StatementChartPro) renderHoverOverlay(svg *string,
	padL, padR, padT, chartW, chartH float64,
	mapYLeft, mapYRight func(float64) float64,
	totalW float64, startTs, endTs int64, hi int) {
	// Hover X is now always projected from the sample's timestamp —
	// the same formula used by the data lines, so the crosshair lands
	// exactly on the rendered point in both modes. The legacy
	// "X-by-index in last-N mode" path was removed when the X axis
	// became proportional to time everywhere.
	if endTs <= startTs {
		return
	}
	tr := float64(endTs - startTs)
	var hx float64
	hxSet := false
	for _, s := range e.series {
		n := len(s.Buffer)
		if n == 0 {
			continue
		}
		idx := hi
		if idx >= n {
			idx = n - 1
		}
		if idx < 0 {
			idx = 0
		}
		hx = padL + float64(s.Buffer[idx].Timestamp-startTs)/tr*chartW
		hxSet = true
		break
	}
	if !hxSet {
		return
	}

	*svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#fff" stroke-width="1" stroke-opacity="%s" stroke-dasharray="%s"/>`,
		hx, padT, hx, padT+chartH, rulesFrontend.KHoverLineOpacity, rulesFrontend.KHoverDashStroke)

	byOff := 0.0
	for si, s := range e.series {
		n := len(s.Buffer)
		if n == 0 {
			continue
		}
		idx := hi
		if idx >= n {
			idx = n - 1
		}
		if idx < 0 {
			idx = 0
		}
		val := s.Buffer[idx].Value
		mapY := mapYLeft
		if s.YAxis == rulesFrontend.KYAxisRight {
			mapY = mapYRight
		}
		hy := mapY(val)
		c := e.getSeriesColor(si)
		*svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="#fff" stroke-width="1.5"/>`,
			hx, hy, rulesFrontend.KHoverPointRadius, c)
		vs := strconv.FormatFloat(val, 'f', 1, 64)
		if e.chartUnit != "" {
			vs += " " + e.chartUnit
		}
		bw := float64(len(vs))*11 + 14
		bx := hx + 10
		if bx+bw > padL+chartW {
			bx = hx - bw - 10
		}
		by := padT + 8 + byOff
		*svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.0f" height="22" rx="3" fill="%s" stroke="%s" stroke-width="1"/>`,
			bx, by, bw, rulesFrontend.KHoverTooltipBg, c)
		*svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="monospace" font-size="%.0f" fill="#fff" text-anchor="middle" dominant-baseline="central">%s</text>`,
			bx+bw/2, by+11, rulesFrontend.KFontSizeTooltip, vs)
		byOff += 26
	}
}

func (e *StatementChartPro) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementChartPro) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// RefreshVisual is part of the LiveUpdatable contract: external code
// may request a re-render after applying off-band changes.
func (e *StatementChartPro) RefreshVisual() { e.recacheFrontend() }

// ─── Buffer management ──────────────────────────────────────────────────────

// addPoint appends a sample to a series buffer at the given absolute
// timestamp. Trims data outside the time window or buffer size.
func (e *StatementChartPro) addPoint(seriesIdx int, val float64, ts int64) {
	if seriesIdx < 0 || seriesIdx >= len(e.series) {
		return
	}
	s := &e.series[seriesIdx]
	s.Buffer = append(s.Buffer, chartProDataPoint{Value: val, Timestamp: ts})
	if e.timeWindowSec > 0 {
		cutoff := time.Now().UnixMilli() - int64(e.timeWindowSec)*1000
		first := 0
		for first < len(s.Buffer) && s.Buffer[first].Timestamp < cutoff {
			first++
		}
		if first > 0 {
			s.Buffer = s.Buffer[first:]
		}
	}
	if len(s.Buffer) > e.bufferSize {
		s.Buffer = s.Buffer[len(s.Buffer)-e.bufferSize:]
	}
}

// addEvent appends a timeline event and prunes old ones beyond
// kEventRetention.
func (e *StatementChartPro) addEvent(kind string, ts int64) {
	e.events = append(e.events, chartProEvent{Kind: kind, Timestamp: ts})
	if len(e.events) > kEventRetention {
		e.events = e.events[len(e.events)-kEventRetention:]
	}
}

// initSeries (re)allocates the series slice based on the current
// seriesCount and bufferSize. Called from Init and from
// ApplyProperties when the user changes the count.
func (e *StatementChartPro) initSeries() {
	e.series = make([]chartProSeries, e.seriesCount)
	for i := range e.series {
		e.series[i] = chartProSeries{
			Color:     rulesFrontend.KSeriesPalette[i%rulesFrontend.KSeriesMax],
			Buffer:    make([]chartProDataPoint, 0, e.bufferSize),
			YAxis:     rulesFrontend.KYAxisLeft,
			ChartType: rulesFrontend.KChartTypeLine,
		}
	}
}

// ─── Init ───────────────────────────────────────────────────────────────────

func (e *StatementChartPro) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}
	e.SetName("chartPro")
	e.backendWidth = rulesFrontend.KBackendWidth
	e.frontendWidth = rulesFrontend.KFrontendInitialWidth
	e.frontendHeight = rulesFrontend.KFrontendInitialHeight
	e.bufferSize = rulesFrontend.KBufferDefault
	e.seriesCount = 1
	e.timeWindowSec = 0
	e.autoScale = true
	e.minY = 0
	e.maxY = 100
	e.showStats = true
	e.showTimestamp = true
	e.showLegend = true
	e.hoverIndex = -1
	e.gapThresholdMs = rulesFrontend.KGapDefaultThresholdMs
	for i := range e.alertZones {
		e.alertZones[i] = chartProAlertZone{
			Enabled: false,
			Color:   rulesFrontend.KAlertZoneDefaultColor,
			Opacity: rulesFrontend.KAlertZoneDefaultOpacityPct,
		}
	}
	e.initSeries()
	e.backendHeight = rulesDensity.Density(e.backendBodyHeight())
	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id

	if e.backendStage != nil {
		e.backendElem, err = e.backendStage.CreateElement(sprite.ElementConfig{
			ID:     e.id + "_back",
			Width:  float64(rulesFrontend.KBackendWidth),
			Height: e.backendTotalHeight(),
			Index:  rulesZIndex.Display,
			SvgXml: e.renderBackendSVG(),
		})
		if err != nil {
			return fmt.Errorf("backend element: %w", err)
		}
		e.backendElem.SetMinSizeD(100, rulesDensity.Density(e.backendTotalHeight()))
		e.wireBackendEvents()
	}
	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front",
			X:  100, Y: 100,
			Width:  e.frontendWidth.GetFloat(),
			Height: e.frontendHeight.GetFloat(),
			Index:  rulesZIndex.DisplayFrontend,
			SvgXml: e.renderFrontendSVG(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(rulesFrontend.KFrontendMinWidth, rulesFrontend.KFrontendMinHeight)
		if e.resizerButton != nil {
			adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
			if err2 := e.frontendElem.SetResizeButtons(adapter); err2 != nil {
				log.Printf("[ChartPro] ERROR: SetResizeButtons: %v", err2)
			}
			e.frontendElem.ShowResizeButtons(false)
			e.frontendElem.SetResizeEnable(false)
		}
		e.wireFrontendEvents()
	}
	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	log.Printf("[ChartPro/factory] init complete: id=%s seriesCount=%d backend=%v frontend=%v",
		e.id, e.seriesCount, e.backendElem != nil, e.frontendElem != nil)
	log.Printf("[ChartPro/factory] context menus: backend=%v frontend=%v",
		e.backendCtxMenu != nil, e.frontendCtxMenu != nil)
	return nil
}

// ─── Backend events ─────────────────────────────────────────────────────────
//
// The backend node has no wireable connectors, so SetOnClick only ever
// opens the body menu. SetCursorHitTest always returns the empty
// CursorStyle because there is nothing to highlight.

func (e *StatementChartPro) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			log.Printf("[ChartPro/backend] %s: click ignored (backendCtxMenu is nil)", e.id)
			return
		}
		ex, ey := e.backendElem.GetPosition()
		mx, my := ex+event.LocalX, ey+event.LocalY
		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}
		if event.LocalY <= e.backendBodyHeight() {
			log.Printf("[ChartPro/backend] %s: opening body menu at (%.1f, %.1f)", e.id, mx, my)
			go e.backendCtxMenu.OpenAtWorld(e.backendBodyItems(), mx, my)
		}
	})

	// Real-time conflict feedback during drag.
	e.backendElem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.backendElem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.backendElem.GetPositionD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
		// Backend no longer registers connectors, but other devices may
		// have legacy wires pointing here; recalculate so any drawn
		// stubs follow the move.
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// No connector hit-test — body is the only interactive zone.
	e.backendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		return ""
	})
}

// ─── Frontend events ────────────────────────────────────────────────────────
//
// The frontend element accepts:
//   - drag (when not lockLayout)
//   - resize via corner handles (when not lockLayout)
//   - hover for crosshair (always)
//   - click to open the context menu (always — even when lockLayout,
//     because we want to expose Snapshot, Pause, Clear still)

func (e *StatementChartPro) wireFrontendEvents() {
	e.frontendElem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.frontendElem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.frontendElem.GetPositionD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.frontendElem.SetPositionD(nx, ny)
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.frontendElem.SetOnResizeStart(func(event sprite.ResizeEvent) {})
	e.frontendElem.SetOnResizeMove(func(event sprite.ResizeEvent) {})
	e.frontendElem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		wD, hD := e.frontendElem.GetSizeD()
		nw, nh := e.gridAdjust.AdjustCenterD(wD, hD)
		e.frontendElem.SetSizeD(nw, nh)
		e.frontendWidth = nw
		e.frontendHeight = nh
		e.SetResizeEnable(false)
		e.SetDragEnable(true)
		go func() {
			e.recacheFrontend()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	})

	e.createTooltip()

	// Hover handling.
	e.frontendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		if len(e.series) == 0 {
			return ""
		}
		const sc = rulesFrontend.KFrontendScale
		cPL := rulesFrontend.KPadL / sc
		cPR := rulesFrontend.KPadR / sc
		if e.hasRightAxis() {
			cPR = rulesFrontend.KPadRWithRightAxis / sc
		}
		cPT := rulesFrontend.KPadT / sc
		cPB := rulesFrontend.KPadB / sc
		cW := e.frontendWidth.GetFloat() - cPL - cPR
		cH := e.frontendHeight.GetFloat() - cPT - cPB
		rx, ry := lx-cPL, ly-cPT
		if rx < 0 || rx > cW || ry < 0 || ry > cH {
			// Outside plot area — clear hover.
			e.hoverMu.Lock()
			changed := e.hoverIndex != -1
			e.hoverIndex = -1
			e.hoverMu.Unlock()
			if changed {
				go e.recacheFrontend()
			}
			return ""
		}
		ratio := rx / cW
		maxN := 0
		for _, s := range e.series {
			if len(s.Buffer) > maxN {
				maxN = len(s.Buffer)
			}
		}
		if maxN == 0 {
			return ""
		}
		bs := e.bufferSize
		start, count := 0, maxN
		if count > bs {
			start = count - bs
			count = bs
		}
		idx := start + int(ratio*float64(count-1)+0.5)
		if idx < 0 {
			idx = 0
		}
		if idx >= maxN {
			idx = maxN - 1
		}
		e.hoverMu.Lock()
		changed := idx != e.hoverIndex
		e.hoverIndex = idx
		e.hoverMu.Unlock()
		if changed {
			go e.recacheFrontend()
		}
		return sprite.CursorPointer
	})

	// Click → open context menu.
	//
	// LOG INSTRUMENTATION: this is the path that the §3.1 bug appears
	// to break. Every entry here is logged so we can correlate the
	// missing menu to either nil controller, never-fired event, or
	// silent failure of OpenAtWorld.
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		log.Printf("[ChartPro/frontend] %s: click received at local (%.1f, %.1f)",
			e.id, event.LocalX, event.LocalY)
		if e.frontendCtxMenu == nil {
			log.Printf("[ChartPro/frontend] %s: WARN frontendCtxMenu is nil, menu cannot open",
				e.id)
			return
		}
		elemX, elemY := e.frontendElem.GetPosition()
		clickWX := elemX + event.LocalX
		clickWY := elemY + event.LocalY
		log.Printf("[ChartPro/frontend] %s: opening frontend menu at world (%.1f, %.1f)",
			e.id, clickWX, clickWY)
		go e.frontendCtxMenu.OpenAtWorld(e.frontendContextItems(), clickWX, clickWY)
	})
}

// createTooltip creates the DOM tooltip used for hover values.
// Kept as legacy infrastructure — the new SVG hover overlay covers
// most cases but createTooltip is referenced by other call sites.
func (e *StatementChartPro) createTooltip() {
	doc := js.Global().Get("document")
	tip := doc.Call("createElement", "div")
	tip.Get("style").Set("cssText", "position:fixed;display:none;pointer-events:none;z-index:9999;background:#1e1e2eee;border:1px solid #444;border-radius:4px;padding:4px 10px;font-family:monospace;font-size:14px;color:#fff;white-space:nowrap;box-shadow:0 2px 8px rgba(0,0,0,0.5);")
	doc.Get("body").Call("appendChild", tip)
	e.tooltipEl = tip
}

// ─── Frontend context menu ──────────────────────────────────────────────────
//
// Four items: Resize, Pause/Resume (toggles), Snapshot, Clear.
// Pause shows "Pause" when running and "Resume" when paused.

func (e *StatementChartPro) frontendContextItems() []contextMenu.Item {
	// Resize uses the standard builder.
	resize := mainMenu.ResizeItem(func() {
		e.SetResizeEnable(!e.GetResizeEnable())
	})

	// Pause / Resume — same slot, label and icon swap based on state.
	var pauseLabel, pauseHelp string
	var pausePath, pauseViewBox string
	if e.paused {
		pauseLabel = translate.T("menuChartProResume", "Resume")
		pauseHelp = "Resumes live rendering of the chart. Data was still being recorded while paused — the chart will jump to the latest state."
		pausePath = rulesFrontend.KIconPathPlay
		pauseViewBox = rulesFrontend.KIconViewBoxPlay
	} else {
		pauseLabel = translate.T("menuChartProPause", "Pause")
		pauseHelp = "Freezes the chart visual so you can study a snapshot. Data continues to be received and stored — only the drawing pauses."
		pausePath = rulesFrontend.KIconPathPause
		pauseViewBox = rulesFrontend.KIconViewBoxPause
	}
	pause := contextMenu.Item{
		ID:              "pause",
		Label:           pauseLabel,
		FontAwesomePath: pausePath,
		ViewBox:         pauseViewBox,
		HelpKey:         "helpChartProPause",
		HelpFallback:    pauseHelp,
		OnClick:         func() { e.TogglePause() },
	}

	snapshot := contextMenu.Item{
		ID:              "snapshot",
		Label:           translate.T("menuChartProSnapshot", "Snapshot"),
		FontAwesomePath: rulesFrontend.KIconPathCamera,
		ViewBox:         rulesFrontend.KIconViewBoxCamera,
		HelpKey:         "helpChartProSnapshot",
		HelpFallback:    "Captures the current chart view as a PNG image and prompts the browser to download it.",
		OnClick:         func() { e.Snapshot() },
	}

	clear := contextMenu.Item{
		ID:              "clear",
		Label:           translate.T("menuChartProClear", "Clear"),
		FontAwesomePath: rulesFrontend.KIconPathEraser,
		ViewBox:         rulesFrontend.KIconViewBoxEraser,
		HelpKey:         "helpChartProClear",
		HelpFallback:    "Empties the chart visually. Persisted data on the server is NOT affected — only what you see on this device. New incoming data starts a fresh chart.",
		OnClick:         func() { e.Clear() },
	}

	return []contextMenu.Item{resize, pause, snapshot, clear}
}

// ─── Backend body menu ──────────────────────────────────────────────────────

func (e *StatementChartPro) backendBodyItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ─── Pause / Snapshot / Clear ───────────────────────────────────────────────

// TogglePause flips the paused state. While paused, recacheFrontend is
// suppressed by LiveUpdate; the buffer continues to fill.
// On resume, a single recacheFrontend brings the visual back in sync.
func (e *StatementChartPro) TogglePause() {
	e.paused = !e.paused
	log.Printf("[ChartPro/menu] %s: paused=%v", e.id, e.paused)
	// On resume we want an immediate refresh; on pause we also redraw
	// once to show the "PAUSED" badge. Both cases want a recache.
	go e.recacheFrontend()
}

// Snapshot grabs the current frontend SVG, draws it to an offscreen
// canvas, and triggers a PNG download via an <a download> link.
//
// Why this works in WASM:
//
//	The browser handles SVG → canvas rasterization natively via an
//	Image with src = "data:image/svg+xml;base64,...". After drawing
//	the image into a canvas of the same pixel dimensions, canvas
//	.toDataURL("image/png") gives us a downloadable PNG.
//
// The download triggers an A element with a synthesised click. No
// server round trip; everything happens in the browser.
func (e *StatementChartPro) Snapshot() {
	if e.frontendElem == nil {
		log.Printf("[ChartPro/menu] %s: Snapshot ignored (frontendElem is nil)", e.id)
		return
	}
	svgText := e.renderFrontendSVG()
	w := e.frontendWidth.GetFloat() * rulesFrontend.KFrontendScale
	h := e.frontendHeight.GetFloat() * rulesFrontend.KFrontendScale

	doc := js.Global().Get("document")
	canvas := doc.Call("createElement", "canvas")
	canvas.Set("width", int(w))
	canvas.Set("height", int(h))
	ctx := canvas.Call("getContext", "2d")

	img := js.Global().Get("Image").New()
	loaded := make(chan struct{}, 1)

	onLoad := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ctx.Call("drawImage", img, 0, 0)
		dataURL := canvas.Call("toDataURL", "image/png").String()
		a := doc.Call("createElement", "a")
		a.Set("href", dataURL)
		a.Set("download", fmt.Sprintf("%s_%d.png", e.id, time.Now().Unix()))
		doc.Get("body").Call("appendChild", a)
		a.Call("click")
		a.Get("parentNode").Call("removeChild", a)
		select {
		case loaded <- struct{}{}:
		default:
		}
		return nil
	})

	onError := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Printf("[ChartPro/menu] %s: Snapshot Image.onerror — SVG may be malformed", e.id)
		select {
		case loaded <- struct{}{}:
		default:
		}
		return nil
	})

	img.Set("onload", onLoad)
	img.Set("onerror", onError)
	// btoa would mangle UTF-8; encode using the standard base64 path
	// via JavaScript's btoa applied to a unescaped representation.
	b64 := js.Global().Call("btoa", svgText).String()
	img.Set("src", "data:image/svg+xml;base64,"+b64)

	// Release callbacks once the image has loaded (or errored).
	go func() {
		<-loaded
		onLoad.Release()
		onError.Release()
	}()
}

// Clear empties all series buffers and the event log. Useful after a
// known event ("operator is now starting a new test run") to get a
// fresh visual without restarting the firmware.
//
// Persisted data (on the server, in a future feature) is NOT touched.
func (e *StatementChartPro) Clear() {
	log.Printf("[ChartPro/menu] %s: Clear — emptying %d series + %d events",
		e.id, len(e.series), len(e.events))
	for i := range e.series {
		e.series[i].Buffer = e.series[i].Buffer[:0]
		// Reset the unwrap state: a Clear is essentially a "start
		// fresh", and we want the next packet to recalibrate as if
		// this were a new device.
		e.series[i].hasUnwrapState = false
		e.series[i].lastT = 0
		e.series[i].tBootReal = 0
	}
	e.events = e.events[:0]
	e.failBadgeUntilMs = 0
	e.failBadgeTs = 0
	e.hoverMu.Lock()
	e.hoverIndex = -1
	e.hoverMu.Unlock()
	go e.recacheFrontend()
}

// ─── Inspect overlay ────────────────────────────────────────────────────────
//
// The Inspect overlay is the canonical place to edit every property
// of the chart. Backend-only by decision D10. Four tabs:
//
//   1. Properties — global chart config (title, buffer, axes, ...).
//   2. Series     — per-series config (label, colour, type, axis).
//   3. Alerts     — alert zone bands.
//   4. Help       — markdown documentation, fetched from /help/devices/...
//
// Two checkboxes appear in the Properties tab — `lockLayout` and
// `lockSend` — even though `lockSend` has no effect on ChartPro
// (which never sends). Keeping it in the Inspect makes the schema
// uniform across all compFrontend devices, simplifying both saved-
// scene migration and operator mental model.
//
// The webhook URL is shown as an informative read-only field so the
// firmware author can copy it without leaving the IDE.

func (e *StatementChartPro) showInspectOverlay() {
	overlay.Show(e.GetInspectConfig().(overlay.Config))
}

// webhookURL synthesises the HTTP endpoint that firmware POSTs to in
// order to push samples to this project. The endpoint pattern is:
//
//	http://<host>:<port>/api/v1/webhook/<projectID>
//
// Notice: the device ID is NOT in the path. The endpoint accepts a
// JSON array of envelopes, each carrying its own device_id, so a
// single POST can update many devices at once (efficient for
// hardware that drives N sensors simultaneously).
//
// Required HTTP header:
//
//	X-API-Key: <project API key>
//
// Example body for the batch endpoint:
//
//	[
//	  {"device_id":"chartPro_1","port":"s0","value":{"v":50.00,"t":12345678901}},
//	  {"device_id":"chartPro_1","port":"s1","value":{"v":99.99,"t":12345678901}}
//	]
//
// The "value" can be a bare number (legacy), {"v":..., "t":...}
// (timestamp ms since hardware boot — preserves timeline integrity
// across packet loss), or {"v":..., "ts":...} (absolute epoch ms).
// See parseLivePayload for details.
//
// Read-only and best-effort. The actual endpoint exposed by the
// server is decided by the server, not the IDE. If the pattern
// changes there, update this helper to match.
//
// Português: URL HTTP para a qual o firmware faz POST. O device ID
// vai DENTRO do body (em cada item do array), NÃO no path. Aceita
// batch para vários devices num único request.
func (e *StatementChartPro) webhookURL() string {
	origin := js.Global().Get("location").Get("origin").String()
	projectID := ""
	if storage := js.Global().Get("localStorage"); storage.Truthy() {
		if v := storage.Call("getItem", "liveProjectID"); v.Truthy() {
			projectID = v.String()
		}
	}
	if projectID == "" {
		projectID = "<set project ID in Live Config>"
	}
	return fmt.Sprintf("%s/api/v1/webhook/%s", origin, projectID)
}

// webhookAPIKey reads the project's webhook API key from localStorage
// (key: "liveApiKey"). This key is what the firmware sends in the
// X-API-Key HTTP header on every POST. Without it the server rejects
// the request with 401.
//
// The key is stored client-side because the IDE is the only place a
// human-readable copy makes sense — the firmware author needs to see
// it once to paste into their sketch. After that the key lives on the
// device in flash, not in the browser.
//
// Returns a placeholder string when the key is not set; the operator
// is expected to configure it via the Live Config dialog before any
// firmware can POST successfully.
//
// SECURITY NOTE: this value is shown in plain text in the Inspect
// overlay. Anyone with access to the open IDE session can read it.
// Treat the IDE session as you would a password manager session.
//
// Português: Lê a chave API do projeto do localStorage. Mostrada em
// texto plano no Inspect para o autor do firmware copiar — quem tem
// acesso à sessão do IDE consegue ler. Trate a sessão do IDE como
// um gerenciador de senhas aberto.
func (e *StatementChartPro) webhookAPIKey() string {
	if storage := js.Global().Get("localStorage"); storage.Truthy() {
		if v := storage.Call("getItem", "liveApiKey"); v.Truthy() {
			if s := v.String(); s != "" {
				return s
			}
		}
	}
	return "<set in Live Config>"
}

// webhookPayloadExample renders a representative POST body for THIS
// device, using its actual ID and listing all currently-configured
// ports (s0..sN). The firmware author copies this string verbatim
// and just plugs in real sensor values where the placeholders are.
//
// Why the example uses {"v":..., "t":...} rather than a bare number:
//
//	The {v, t} shape is the supported way to preserve timeline
//	integrity across packet loss. Bare numbers still work (legacy),
//	but the chart will distort under network jitter. The example
//	steers people toward the good path.
//
// The "t" placeholder is shown as the literal text "millis()" instead
// of a number — Arduino developers immediately recognise it, and it
// signals that this is the API's expected value rather than a magic
// constant. On ESP32 prefer esp_timer_get_time()/1000 for uint64
// monotonic ms (no overflow at 49 days).
//
// Português: Renderiza um exemplo de POST body para ESTE device, com
// o ID real e todas as portas configuradas (s0..sN). Use {v, t} para
// preservar a timeline em caso de perdas — números puros ainda
// funcionam mas distorcem com jitter de rede.
func (e *StatementChartPro) webhookPayloadExample() string {
	if e.seriesCount <= 0 {
		return `[{"device_id":"` + e.id + `","port":"s0","value":{"v":42.0,"t":millis()}}]`
	}
	parts := make([]string, 0, e.seriesCount)
	for i := 0; i < e.seriesCount; i++ {
		parts = append(parts,
			fmt.Sprintf(`{"device_id":"%s","port":"s%d","value":{"v":42.0,"t":millis()}}`,
				e.id, i))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (e *StatementChartPro) GetInspectConfig() interface{} {
	bs := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}
	saveFn := func(v map[string]string) { e.ApplyProperties(v) }

	// ── Tab 1: Properties (global chart configuration) ────────────────────
	globalFields := []overlay.Field{
		{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id},
		{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
		{Key: "chartTitle", Label: translate.T("propChartTitle", "Title"), Type: overlay.FieldText, Value: e.chartTitle, Placeholder: "Temperature"},
		{Key: "chartUnit", Label: translate.T("propChartUnit", "Unit"), Type: overlay.FieldText, Value: e.chartUnit, Placeholder: "°C"},
		{Key: "seriesCount", Label: translate.T("propSeriesCount", "Series Count"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.seriesCount), Min: strconv.Itoa(rulesFrontend.KSeriesMin), Max: strconv.Itoa(rulesFrontend.KSeriesMax)},
		{Key: "bufferSize", Label: translate.T("propBuffer", "Buffer"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.bufferSize), Min: strconv.Itoa(rulesFrontend.KBufferMin), Max: strconv.Itoa(rulesFrontend.KBufferMax), Placeholder: strconv.Itoa(rulesFrontend.KBufferDefault)},
		{Key: "timeWindowSec", Label: translate.T("propTimeWindow", "Time Window (sec)"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.timeWindowSec), Min: "0", Max: "3600", Placeholder: "0=off"},
		{Key: "autoScale", Label: translate.T("propAutoScale", "Auto Scale"), Type: overlay.FieldCheckbox, Value: bs(e.autoScale)},
		{Key: "minY", Label: "Min Y", Type: overlay.FieldNumber, Value: strconv.FormatFloat(e.minY, 'f', 1, 64)},
		{Key: "maxY", Label: "Max Y", Type: overlay.FieldNumber, Value: strconv.FormatFloat(e.maxY, 'f', 1, 64)},
		{Key: "gridColor", Label: translate.T("propGridColor", "Grid Color"), Type: overlay.FieldColor, Value: e.gridColor},
		{Key: "gridOpacity", Label: translate.T("propGridOpacity", "Grid Opacity %"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.gridOpacity), Min: "0", Max: "100"},
		{Key: "gridWidth", Label: translate.T("propGridWidth", "Grid Width"), Type: overlay.FieldNumber, Value: strconv.FormatFloat(e.gridWidth, 'f', 1, 64), Min: "0", Max: "5"},
		{Key: "showStats", Label: translate.T("propShowStats", "Show Min/Avg/Max"), Type: overlay.FieldCheckbox, Value: bs(e.showStats)},
		{Key: "showTimestamp", Label: translate.T("propShowTimestamp", "Show Timestamp"), Type: overlay.FieldCheckbox, Value: bs(e.showTimestamp)},
		{Key: "showLegend", Label: translate.T("propShowLegend", "Show Legend"), Type: overlay.FieldCheckbox, Value: bs(e.showLegend)},

		// Two independent locks (§2.3 of CHARTPRO_REFACTOR.md).
		// lockLayout blocks frontend drag/resize. lockSend would block
		// outbound data — kept here for consistency though ChartPro
		// never sends.
		{Key: "lockLayout", Label: translate.T("propLockLayout", "Lock Layout (frontend drag/resize)"), Type: overlay.FieldCheckbox, Value: bs(e.lockLayout)},
		{Key: "lockSend", Label: translate.T("propLockSend", "Lock Send (no-op on this device)"), Type: overlay.FieldCheckbox, Value: bs(e.lockSend)},

		// ── Gap detection ──────────────────────────────────────────────
		//
		// When two consecutive samples in a series differ by more than
		// this many milliseconds, the segment connecting them is drawn
		// in a faded grey dashed line — telling the operator that
		// stretch is interpolated across a connectivity hole. Zero
		// disables the feature (legacy behaviour, all segments use the
		// series colour).
		//
		// Default 1000 ms suits 5+ Hz samplers. Lower it for fast
		// streams (e.g. 200 ms for 50 Hz), raise it for slow ones
		// (e.g. 5000 ms for 1 Hz).
		{Key: "gapThresholdMs", Label: translate.T("propGapThreshold", "Gap threshold (ms, 0=off)"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.gapThresholdMs), Min: "0", Max: "60000", Placeholder: strconv.Itoa(rulesFrontend.KGapDefaultThresholdMs)},

		// ── Informative webhook section ────────────────────────────────
		//
		// All four fields are read-only — the firmware author copies them
		// into the sketch. Order from "where to send" to "what to send":
		//
		//   1. Webhook URL          → endpoint to POST to
		//   2. API Key              → goes in X-API-Key header
		//   3. Content-Type header  → JSON
		//   4. Payload example      → exact JSON shape expected
		//
		// The payload example uses this device's actual ID and lists all
		// configured ports (s0..sN) so the operator can paste-and-go.
		{Key: "webhookURL", Label: translate.T("propWebhookURL", "Webhook URL"), Type: overlay.FieldText, Value: e.webhookURL(), ReadOnly: true},
		{Key: "webhookAPIKey", Label: translate.T("propWebhookAPIKey", "API Key (header: X-API-Key)"), Type: overlay.FieldText, Value: e.webhookAPIKey(), ReadOnly: true},
		{Key: "webhookContentType", Label: translate.T("propWebhookContentType", "Content-Type header"), Type: overlay.FieldText, Value: "application/json", ReadOnly: true},
		{Key: "webhookBody", Label: translate.T("propWebhookBody", "Payload example"), Type: overlay.FieldText, Value: e.webhookPayloadExample(), ReadOnly: true},
	}

	// ── Tab 2: Series (per-series configuration) ──────────────────────────
	var seriesFields []overlay.Field
	for i := 0; i < e.seriesCount; i++ {
		p := fmt.Sprintf("series_%d_", i)
		sl, sc := "", rulesFrontend.KSeriesPalette[i%rulesFrontend.KSeriesMax]
		sg, sy, sct := false, rulesFrontend.KYAxisLeft, rulesFrontend.KChartTypeLine
		if i < len(e.series) {
			sl = e.series[i].Label
			if e.series[i].Color != "" {
				sc = e.series[i].Color
			}
			sg = e.series[i].GlowLine
			sy = e.series[i].YAxis
			if e.series[i].ChartType != "" {
				sct = e.series[i].ChartType
			}
		}
		seriesFields = append(seriesFields,
			overlay.Field{Key: p + "header", Label: fmt.Sprintf("Series %d", i), Type: overlay.FieldText, Value: fmt.Sprintf("port: %s", seriesPortName(i)), ReadOnly: true},
			overlay.Field{Key: p + "label", Label: translate.T("propSeriesLabel", "Label"), Type: overlay.FieldText, Value: sl, Placeholder: fmt.Sprintf("Series %d", i)},
			overlay.Field{Key: p + "color", Label: translate.T("propSeriesColor", "Color"), Type: overlay.FieldColor, Value: sc},
			overlay.Field{Key: p + "type", Label: translate.T("propChartType", "Type"), Type: overlay.FieldSelect, Value: sct, Options: []overlay.Option{{Value: rulesFrontend.KChartTypeLine, Label: "Line"}, {Value: rulesFrontend.KChartTypeScatter, Label: "Scatter"}}},
			overlay.Field{Key: p + "glow", Label: translate.T("propGlow", "Glow Effect"), Type: overlay.FieldCheckbox, Value: bs(sg)},
			overlay.Field{Key: p + "yaxis", Label: translate.T("propYAxis", "Y Axis"), Type: overlay.FieldSelect, Value: sy, Options: []overlay.Option{{Value: rulesFrontend.KYAxisLeft, Label: "Left"}, {Value: rulesFrontend.KYAxisRight, Label: "Right"}}},
		)
	}

	// ── Tab 3: Alert Zones ────────────────────────────────────────────────
	var alertFields []overlay.Field
	for i := 0; i < rulesFrontend.KAlertZoneMax; i++ {
		p := fmt.Sprintf("az_%d_", i)
		az := e.alertZones[i]
		alertFields = append(alertFields,
			overlay.Field{Key: p + "enabled", Label: fmt.Sprintf("Zone %d", i), Type: overlay.FieldCheckbox, Value: bs(az.Enabled)},
			overlay.Field{Key: p + "label", Label: "Label", Type: overlay.FieldText, Value: az.Label, Placeholder: "e.g. Critical"},
			overlay.Field{Key: p + "minY", Label: "Min Y", Type: overlay.FieldNumber, Value: strconv.FormatFloat(az.MinY, 'f', 1, 64)},
			overlay.Field{Key: p + "maxY", Label: "Max Y", Type: overlay.FieldNumber, Value: strconv.FormatFloat(az.MaxY, 'f', 1, 64)},
			overlay.Field{Key: p + "color", Label: "Color", Type: overlay.FieldColor, Value: az.Color},
			overlay.Field{Key: p + "opacity", Label: "Opacity %", Type: overlay.FieldNumber, Value: strconv.Itoa(az.Opacity), Min: "0", Max: "100"},
		)
	}

	return overlay.Config{Title: e.id, Width: "560px", Tabs: []overlay.Tab{
		{Label: translate.T("tabProperties", "Properties"), Type: overlay.TabForm, Fields: globalFields},
		{Label: translate.T("tabSeries", "Series"), Type: overlay.TabForm, Fields: seriesFields},
		{Label: translate.T("tabAlertZones", "Alerts"), Type: overlay.TabForm, Fields: alertFields},
		{Label: translate.T("tabHelp", "Help"), Type: overlay.TabMarkdown, ContentURL: "/help/devices/display/statementChartPro.md"},
	}, OnSave: saveFn}
}

// ApplyProperties updates this device from a map of property values
// (typically produced by the Inspect overlay's onSave).
//
// Accepts both new keys (`lockLayout`, `lockSend`) and the legacy
// `interactionLocked` for backward-compatibility with scenes saved
// before the §2.3 refactor — the legacy key maps to lockLayout.
//
// When a layout change happens (id, seriesCount, frontend dimensions)
// we also rerender both stages and notify the scene so neighbours can
// react. The recache is wrapped in a 200ms delayed goroutine because
// the overlay closes asynchronously and we want the user to see the
// modal disappear before the chart flickers.
func (e *StatementChartPro) ApplyProperties(v map[string]string) {
	ch, rc := false, false

	if val, ok := v["id"]; ok && val != "" && val != e.id {
		old := e.id
		if e.wireMgr != nil {
			e.wireMgr.UnregisterElement(old)
		}
		e.id = val
		if e.label == old {
			e.label = val
		}
		rc = true
		ch = true
	}
	if val, ok := v["label"]; ok && val != e.label {
		e.label = val
		ch = true
	}
	if val, ok := v["chartTitle"]; ok && val != e.chartTitle {
		e.chartTitle = val
		ch = true
	}
	if val, ok := v["chartUnit"]; ok && val != e.chartUnit {
		e.chartUnit = val
		ch = true
	}
	if val, ok := v["seriesCount"]; ok {
		if n, err := strconv.Atoi(val); err == nil &&
			n >= rulesFrontend.KSeriesMin && n <= rulesFrontend.KSeriesMax &&
			n != e.seriesCount {
			e.seriesCount = n
			e.initSeries()
			rc = true
			ch = true
			e.backendHeight = rulesDensity.Density(e.backendBodyHeight())
			if e.backendElem != nil {
				e.backendElem.SetSizeD(rulesFrontend.KBackendWidth, rulesDensity.Density(e.backendTotalHeight()))
			}
		}
	}
	if val, ok := v["bufferSize"]; ok {
		if n, err := strconv.Atoi(val); err == nil &&
			n >= rulesFrontend.KBufferMin && n <= rulesFrontend.KBufferMax &&
			n != e.bufferSize {
			e.bufferSize = n
			for i := range e.series {
				if len(e.series[i].Buffer) > n {
					e.series[i].Buffer = e.series[i].Buffer[len(e.series[i].Buffer)-n:]
				}
			}
			ch = true
		}
	}
	if val, ok := v["timeWindowSec"]; ok {
		if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= 3600 && n != e.timeWindowSec {
			e.timeWindowSec = n
			ch = true
		}
	}
	if val, ok := v["autoScale"]; ok {
		b := val == "true"
		if b != e.autoScale {
			e.autoScale = b
			ch = true
		}
	}
	if val, ok := v["minY"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil && n != e.minY {
			e.minY = n
			ch = true
		}
	}
	if val, ok := v["maxY"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil && n != e.maxY {
			e.maxY = n
			ch = true
		}
	}
	if val, ok := v["gridColor"]; ok && val != e.gridColor {
		e.gridColor = val
		ch = true
	}
	if val, ok := v["gridOpacity"]; ok {
		if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= 100 && n != e.gridOpacity {
			e.gridOpacity = n
			ch = true
		}
	}
	if val, ok := v["gridWidth"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil && n >= 0 && n <= 5 && n != e.gridWidth {
			e.gridWidth = n
			ch = true
		}
	}
	if val, ok := v["showStats"]; ok {
		b := val == "true"
		if b != e.showStats {
			e.showStats = b
			ch = true
		}
	}
	if val, ok := v["showTimestamp"]; ok {
		b := val == "true"
		if b != e.showTimestamp {
			e.showTimestamp = b
			ch = true
		}
	}
	if val, ok := v["showLegend"]; ok {
		b := val == "true"
		if b != e.showLegend {
			e.showLegend = b
			ch = true
		}
	}

	// Lock flags — accept both new and legacy keys. If both are
	// present, the new key wins. Order of evaluation: legacy first
	// (so it can be overridden by the new key in the same map).
	if val, ok := v["interactionLocked"]; ok {
		b := val == "true"
		if b != e.lockLayout {
			log.Printf("[ChartPro/inspect] %s: migrating legacy interactionLocked=%v → lockLayout", e.id, b)
			e.lockLayout = b
			ch = true
		}
	}
	if val, ok := v["lockLayout"]; ok {
		b := val == "true"
		if b != e.lockLayout {
			e.lockLayout = b
			ch = true
			// Re-apply drag/resize state so the new lock takes
			// effect on the frontend immediately.
			if b {
				e.SetResizeEnable(false)
				if e.frontendElem != nil {
					e.frontendElem.SetDragEnable(false)
				}
			} else if e.frontendElem != nil {
				e.frontendElem.SetDragEnable(e.dragEnabled)
			}
		}
	}
	if val, ok := v["lockSend"]; ok {
		b := val == "true"
		if b != e.lockSend {
			e.lockSend = b
			ch = true
		}
	}
	if val, ok := v["gapThresholdMs"]; ok {
		// Parse and clamp to the same range advertised in Inspect.
		// Negative or non-numeric values fall back to default.
		// Zero is intentionally allowed — it disables the feature.
		if n, err := strconv.Atoi(val); err == nil {
			if n < 0 {
				n = 0
			} else if n > 60000 {
				n = 60000
			}
			if n != e.gapThresholdMs {
				e.gapThresholdMs = n
				ch = true
			}
		}
	}

	if val, ok := v["frontendWidth"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			e.frontendWidth = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			ch = true
		}
	}
	if val, ok := v["frontendHeight"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			e.frontendHeight = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			ch = true
		}
	}

	// Per-series.
	for i := 0; i < e.seriesCount && i < len(e.series); i++ {
		p := fmt.Sprintf("series_%d_", i)
		if val, ok := v[p+"label"]; ok && val != e.series[i].Label {
			e.series[i].Label = val
			ch = true
		}
		if val, ok := v[p+"color"]; ok && val != "" && val != e.series[i].Color {
			e.series[i].Color = val
			ch = true
		}
		if val, ok := v[p+"glow"]; ok {
			b := val == "true"
			if b != e.series[i].GlowLine {
				e.series[i].GlowLine = b
				ch = true
			}
		}
		if val, ok := v[p+"type"]; ok &&
			(val == rulesFrontend.KChartTypeLine || val == rulesFrontend.KChartTypeScatter) &&
			val != e.series[i].ChartType {
			e.series[i].ChartType = val
			ch = true
		}
		if val, ok := v[p+"yaxis"]; ok &&
			(val == rulesFrontend.KYAxisLeft || val == rulesFrontend.KYAxisRight) &&
			val != e.series[i].YAxis {
			e.series[i].YAxis = val
			ch = true
		}
	}

	// Alert zones.
	for i := 0; i < rulesFrontend.KAlertZoneMax; i++ {
		p := fmt.Sprintf("az_%d_", i)
		if val, ok := v[p+"enabled"]; ok {
			b := val == "true"
			if b != e.alertZones[i].Enabled {
				e.alertZones[i].Enabled = b
				ch = true
			}
		}
		if val, ok := v[p+"label"]; ok && val != e.alertZones[i].Label {
			e.alertZones[i].Label = val
			ch = true
		}
		if val, ok := v[p+"minY"]; ok {
			if n, err := strconv.ParseFloat(val, 64); err == nil && n != e.alertZones[i].MinY {
				e.alertZones[i].MinY = n
				ch = true
			}
		}
		if val, ok := v[p+"maxY"]; ok {
			if n, err := strconv.ParseFloat(val, 64); err == nil && n != e.alertZones[i].MaxY {
				e.alertZones[i].MaxY = n
				ch = true
			}
		}
		if val, ok := v[p+"color"]; ok && val != "" && val != e.alertZones[i].Color {
			e.alertZones[i].Color = val
			ch = true
		}
		if val, ok := v[p+"opacity"]; ok {
			if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= 100 && n != e.alertZones[i].Opacity {
				e.alertZones[i].Opacity = n
				ch = true
			}
		}
	}
	if rc {
		e.RegisterConnectors() // no-op, kept for API parity
	}
	if ch {
		go func() {
			time.Sleep(200 * time.Millisecond)
			e.recacheBackend()
			e.recacheFrontend()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	}
}

// ─── Wire registration (legacy API kept as no-op) ───────────────────────────
//
// Per decision §2.1 of CHARTPRO_REFACTOR.md, ChartPro no longer
// registers wireable connectors. This method is preserved as a no-op
// so callers (factory, ApplyProperties, scene loader) compile without
// changes. A log line records any unexpected invocation, which would
// indicate code we missed during the migration.

func (e *StatementChartPro) RegisterConnectors() {
	if e.wireMgr != nil {
		// Defensive: a previous version of the IDE may have left
		// connectors registered for this ID. Unregister them so the
		// backend node truly has no wires.
		e.wireMgr.UnregisterElement(e.id)
	}
}

// ─── Live communication ─────────────────────────────────────────────────────

// parseLivePayload decodes a value RawMessage into one of three
// recognised shapes:
//
//	bare number           → payloadKindLegacy
//	bare string number    → payloadKindLegacy (some firmware double-encodes)
//	{"v":..., "t":...}    → payloadKindRelative
//	{"v":..., "ts":...}   → payloadKindAbsolute
//
// Returns an error only when the bytes cannot be classified as any of
// the above (e.g. a JSON object missing "v", or random text).
func parseLivePayload(value []byte) (parsedPayload, error) {
	var pp parsedPayload

	// 1) Bare number.
	if err := json.Unmarshal(value, &pp.val); err == nil {
		pp.kind = payloadKindLegacy
		pp.ts = time.Now().UnixMilli()
		return pp, nil
	}

	// 2) Bare string containing a number (some firmware double-encodes).
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		if v, parseErr := strconv.ParseFloat(s, 64); parseErr == nil {
			pp.val = v
			pp.kind = payloadKindLegacy
			pp.ts = time.Now().UnixMilli()
			return pp, nil
		}
	}

	// 3) Object form. Use a map to remain tolerant of unknown extra fields.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(value, &obj); err != nil {
		return pp, fmt.Errorf("payload not number, string, or object: %w", err)
	}
	rawV, ok := obj["v"]
	if !ok {
		return pp, fmt.Errorf("payload object missing 'v' field")
	}
	if err := json.Unmarshal(rawV, &pp.val); err != nil {
		return pp, fmt.Errorf("invalid 'v' field: %w", err)
	}

	// Prefer `ts` (absolute) over `t` (relative) when both present.
	if rawTs, ok := obj["ts"]; ok {
		if err := json.Unmarshal(rawTs, &pp.ts); err != nil {
			return pp, fmt.Errorf("invalid 'ts' field: %w", err)
		}
		pp.kind = payloadKindAbsolute
		return pp, nil
	}
	if rawT, ok := obj["t"]; ok {
		if err := json.Unmarshal(rawT, &pp.t); err != nil {
			return pp, fmt.Errorf("invalid 't' field: %w", err)
		}
		pp.kind = payloadKindRelative
		return pp, nil
	}

	// Object with only "v" — treat as legacy (no timestamp from device).
	pp.kind = payloadKindLegacy
	pp.ts = time.Now().UnixMilli()
	return pp, nil
}

// resolveTimestamp converts a parsedPayload into an absolute epoch ms
// timestamp, applying the millis() unwrap algorithm (§6.4 of
// CHARTPRO_REFACTOR.md) for relative payloads. Returns (ts, isReset);
// the caller adds a RESET marker when isReset is true.
//
// The algorithm is intentionally simple: any time `t` retrogrades
// (t_new < t_last) we recalibrate the "boot real" anchor and emit a
// reset event. We do not distinguish hardware reboot from millis()
// overflow — they are presented to the operator identically as "a
// discontinuity occurred". This is the explicit decision in §6.4.
func (e *StatementChartPro) resolveTimestamp(seriesIdx int, pp parsedPayload) (int64, bool) {
	if seriesIdx < 0 || seriesIdx >= len(e.series) {
		return time.Now().UnixMilli(), false
	}
	s := &e.series[seriesIdx]
	switch pp.kind {
	case payloadKindLegacy, payloadKindAbsolute:
		return pp.ts, false
	case payloadKindRelative:
		t := pp.t
		if !s.hasUnwrapState {
			// First {v, t} for this series — calibrate.
			s.tBootReal = time.Now().UnixMilli() - int64(t)
			s.lastT = t
			s.hasUnwrapState = true
			return s.tBootReal + int64(t), false
		}
		if t < s.lastT {
			// Retrograde — overflow or reboot, treated identically.
			s.tBootReal = time.Now().UnixMilli() - int64(t)
			s.lastT = t
			return s.tBootReal + int64(t), true
		}
		s.lastT = t
		return s.tBootReal + int64(t), false
	}
	return time.Now().UnixMilli(), false
}

// LiveUpdate is called by live.Client whenever an inbound message
// arrives for this device. The port must match an existing series
// (sN where 0 <= N < seriesCount). The payload is parsed per
// parseLivePayload, the timestamp is resolved per resolveTimestamp,
// and the point is appended to the series buffer.
//
// Data ALWAYS enters the buffer immediately; only the visual refresh
// is throttled to KRenderIntervalMs (default 100ms / 10 fps).
//
// The flag `paused` suppresses recache but never the buffer write —
// pausing freezes the visual, not the data acquisition.
//
// Note: ignores both lockLayout and lockSend. Inbound data must
// always reach the chart; locks only affect outbound and visual
// manipulation. See §9.4.
func (e *StatementChartPro) LiveUpdate(port string, value []byte) error {
	log.Printf("[ChartPro/live] %s: LiveUpdate received: port=%s raw=%s",
		e.id, port, string(value))

	if !strings.HasPrefix(port, "s") {
		return fmt.Errorf("chartPro %s: unknown port %q", e.id, port)
	}
	idx, err := strconv.Atoi(port[1:])
	if err != nil || idx < 0 || idx >= e.seriesCount {
		return fmt.Errorf("chartPro %s: invalid series in port %q", e.id, port)
	}

	pp, err := parseLivePayload(value)
	if err != nil {
		log.Printf("[ChartPro/live] %s: parse error: %v", e.id, err)
		return fmt.Errorf("chartPro %s port %s: %w", e.id, port, err)
	}
	log.Printf("[ChartPro/live] %s: parsed val=%v kind=%d t=%d ts=%d",
		e.id, pp.val, pp.kind, pp.t, pp.ts)

	ts, isReset := e.resolveTimestamp(idx, pp)
	log.Printf("[ChartPro/live] %s: resolved ts=%d isReset=%v", e.id, ts, isReset)

	e.addPoint(idx, pp.val, ts)
	if isReset {
		e.addEvent(eventKindReset, ts)
	}
	e.lastUpdateTs = ts / 1000

	// Throttle rendering: only redraw if enough time has passed.
	// Data is already in the buffer; only the visual refresh is capped.
	// Respects pause: when paused, the buffer keeps filling but the
	// frontend stays frozen.
	now := time.Now().UnixMilli()
	if now-e.lastRenderMs >= rulesFrontend.KRenderIntervalMs {
		e.lastRenderMs = now
		if !e.paused {
			e.recacheFrontend()
		}
	}
	return nil
}

// SendValue would emit a value from this device to external hardware.
// ChartPro is read-only — it does not send. This method is intentionally
// absent. Other compFrontend devices (LED, Knob, Button) will define
// their own when the §7.3 work begins.

// ─── Serialization ──────────────────────────────────────────────────────────
//
// GetProperties returns the device's state as a string-keyed map
// suitable for scene persistence. Writes both new keys (lockLayout,
// lockSend) so future loads pick them up; the legacy key
// `interactionLocked` is NOT written — old scenes accepting it via
// ApplyProperties is one-way migration. Once saved post-refactor,
// scenes use the new schema.

func (e *StatementChartPro) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label":          e.label,
		"chartTitle":     e.chartTitle,
		"chartUnit":      e.chartUnit,
		"seriesCount":    e.seriesCount,
		"bufferSize":     e.bufferSize,
		"timeWindowSec":  e.timeWindowSec,
		"autoScale":      e.autoScale,
		"minY":           e.minY,
		"maxY":           e.maxY,
		"gridColor":      e.gridColor,
		"gridOpacity":    e.gridOpacity,
		"gridWidth":      e.gridWidth,
		"showStats":      e.showStats,
		"showTimestamp":  e.showTimestamp,
		"showLegend":     e.showLegend,
		"lockLayout":     e.lockLayout,
		"lockSend":       e.lockSend,
		"gapThresholdMs": e.gapThresholdMs,
		"frontendWidth":  e.frontendWidth.GetFloat(),
		"frontendHeight": e.frontendHeight.GetFloat(),
	}
	for i, s := range e.series {
		p := fmt.Sprintf("series_%d_", i)
		props[p+"label"] = s.Label
		props[p+"color"] = s.Color
		props[p+"glow"] = s.GlowLine
		props[p+"yaxis"] = s.YAxis
		props[p+"type"] = s.ChartType
	}
	for i, az := range e.alertZones {
		p := fmt.Sprintf("az_%d_", i)
		props[p+"enabled"] = az.Enabled
		props[p+"label"] = az.Label
		props[p+"minY"] = az.MinY
		props[p+"maxY"] = az.MaxY
		props[p+"color"] = az.Color
		props[p+"opacity"] = az.Opacity
	}
	return props
}

// ─── State accessors ────────────────────────────────────────────────────────

func (e *StatementChartPro) GetInitialized() bool   { return e.initialized }
func (e *StatementChartPro) GetID() string          { return e.id }
func (e *StatementChartPro) GetName() string        { return e.name }
func (e *StatementChartPro) GetSelected() bool      { return e.selected }
func (e *StatementChartPro) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementChartPro) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementChartPro) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementChartPro) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementChartPro) GetResize() bool        { return false }

func (e *StatementChartPro) GetResizeEnable() bool {
	if e.frontendElem != nil {
		return e.frontendElem.IsResizeEnabled()
	}
	return false
}

func (e *StatementChartPro) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementChartPro) SetSelected(sel bool) {
	e.selected = sel
	e.SetDragEnable(sel)
}

// SetDragEnable enables/disables drag on both stage elements.
//
// Backend drag is independent of lockLayout — lockLayout is a
// frontend-only concern (operators interacting with the deployed
// dashboard should not move the chart). Backend drag obeys whatever
// the designer wants.
//
// Frontend drag is suppressed when lockLayout is on, regardless of
// the requested state.
func (e *StatementChartPro) SetDragEnable(en bool) {
	e.dragEnabled = en
	if e.backendElem == nil {
		e.pendingDragEnable = &en
		return
	}
	e.backendElem.SetDragEnable(en)
	if e.frontendElem != nil {
		// Frontend respects lockLayout.
		e.frontendElem.SetDragEnable(en && !e.lockLayout)
	}
}

// SetResizeEnable toggles the resize handles on the frontend element.
// Frontend-only by design (the backend node auto-sizes to its content).
//
// Suppressed when lockLayout is on.
func (e *StatementChartPro) SetResizeEnable(enabled bool) {
	if e.resizeLocked || e.frontendElem == nil {
		return
	}
	if enabled && e.lockLayout {
		// lockLayout blocks resize even when explicitly requested.
		enabled = false
	}
	if enabled {
		e.frontendElem.SetDragEnable(false)
		e.dragEnabled = false
		e.selected = false
		e.frontendElem.SetResizeEnable(true)
		e.frontendElem.ShowResizeButtons(true)
	} else {
		e.frontendElem.SetResizeEnable(false)
		e.frontendElem.ShowResizeButtons(false)
	}
}

func (e *StatementChartPro) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementChartPro) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}

func (e *StatementChartPro) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}

func (e *StatementChartPro) SetWidth(_ rulesDensity.Density)  {}
func (e *StatementChartPro) SetHeight(_ rulesDensity.Density) {}

func (e *StatementChartPro) SetSize(w, h rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendWidth = w
		e.frontendHeight = h
		e.frontendElem.SetSizeD(w, h)
	}
}

func (e *StatementChartPro) SetStatus(s int) { e.iconStatus = s }
func (e *StatementChartPro) GetStatus() int  { return e.iconStatus }

// ─── Icon ───────────────────────────────────────────────────────────────────

func (e *StatementChartPro) GetIconName() string     { return "Chart Pro" }
func (e *StatementChartPro) GetIconCategory() string { return "Display" }

func (e *StatementChartPro) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).
		Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).
		Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).
		Stroke(data.ColorBorder).
		Fill(data.ColorBackground).
		D(hexPath)
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").
		FontWeight("bold").
		FontSize(rulesIcon.Width.GetInt() / 4).
		Text("≋").
		Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 10).
		Y((rulesIcon.Height / 2).GetInt() + 5)
	wl, _ := utilsText.GetTextSize(data.Label, rulesIcon.FontFamily,
		rulesIcon.FontWeight, rulesIcon.FontStyle, data.LabelFontSize.GetInt())
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).
		FontWeight(rulesIcon.FontWeight).
		FontStyle(rulesIcon.FontStyle).
		FontSize(data.LabelFontSize.GetInt()).
		Text(data.Label).
		Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - wl/2).
		Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, iconLabel, label)
	rw := rulesIcon.Width * rulesIcon.SizeRatio
	rh := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: rw.GetInt(), Height: rh.GetInt()})
}

// ─── Scene export ───────────────────────────────────────────────────────────

func (e *StatementChartPro) GetDeviceType() string { return "StatementChartPro" }

func (e *StatementChartPro) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementChartPro) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}

func (e *StatementChartPro) GetKind() scenegraph.Kind { return scenegraph.KindSimple }

func (e *StatementChartPro) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementChartPro) GetLabel() string      { return e.label }
func (e *StatementChartPro) SetLabel(label string) { e.label = label; e.recacheBackend() }

func (e *StatementChartPro) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		// No connectors are registered, but other devices might have
		// drawn wires pointing at our position. Ask the manager to
		// recalculate so any stale stubs follow us.
		e.wireMgr.RecalculateForElement(e.id)
	}
}
