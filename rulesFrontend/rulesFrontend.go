// /rulesFrontend/rulesFrontend.go

// Package rulesFrontend is the single source of truth for visual,
// numeric, and layout constants shared by every device under
// devices/compFrontend/ (ChartPro, Chart, PieChart, BarGraph, …).
//
// English:
//
//	Before this package existed, each compFrontend device had ~30-50
//	hardcoded numbers scattered through render functions. "The chart
//	looks too cramped" meant grep-and-pray across many large files.
//	Centralising here lets one person change a single number and have
//	the visual change everywhere it should, without missing a spot or
//	breaking unrelated code paths.
//
//	Scope: anything that is reasonably reusable across compFrontend
//	devices (font sizes, plot border styling, grid defaults, RESET/FAIL
//	marker styling, ring buffer limits, panel chrome, …). Constants
//	that are intrinsically specific to one device (pie slice geometry,
//	gauge needle proportions) stay in that device's own file.
//
//	Icon paths for the Pause/Resume/Snapshot/Clear menu items live
//	here because the same items will appear in Chart and PieChart;
//	having the paths in one place avoids drift.
//
// Português:
//
//	Fonte única de verdade para constantes visuais e numéricas
//	compartilhadas entre os devices de devices/compFrontend/.
//	Constantes intrinsecamente específicas de um device (geometria de
//	fatia de PieChart, proporção de ponteiro de Gauge) ficam no
//	arquivo daquele device.
package rulesFrontend

// ─── Series limits ──────────────────────────────────────────────────────────

// KSeriesMin is the minimum number of series the user may configure.
// One series is the bare minimum for a chart to be useful.
const KSeriesMin = 1

// KSeriesMax caps the number of series. Beyond 8 the line palette
// repeats, the legend overflows, and the SVG payload grows past what
// the browser can re-render at 10 fps.
const KSeriesMax = 8

// KSeriesPalette is the default colour assignment per series index.
// Catppuccin Mocha accents. Series N uses index (N mod len).
var KSeriesPalette = [KSeriesMax]string{
	"#f38ba8", // pink
	"#89b4fa", // blue
	"#a6e3a1", // green
	"#fab387", // peach
	"#cba6f7", // mauve
	"#94e2d5", // teal
	"#f9e2af", // yellow
	"#74c7ec", // sapphire
}

// ─── Alert zones ────────────────────────────────────────────────────────────

// KAlertZoneMax is the maximum number of horizontal alert bands.
// Four is enough to express low/normal/warn/critical in any layout.
const KAlertZoneMax = 4

// ─── Buffer sizing ──────────────────────────────────────────────────────────

// KBufferDefault is the default ring buffer size per series (points).
const KBufferDefault = 100

// KBufferMin is the smallest buffer size accepted. Below 10 points
// the chart is meaningless.
const KBufferMin = 10

// KBufferMax caps the buffer to keep SVG payload reasonable.
// 2000 points × 8 series × ~30 bytes per polyline coordinate is
// already ~480 KB of SVG text per frame.
const KBufferMax = 2000

// ─── Render throttling ──────────────────────────────────────────────────────

// KRenderIntervalMs is the minimum time between SVG re-renders during
// live data ingestion. At 20+ updates/sec the SVG would regenerate
// on every single point, causing visible flicker. Capping at 100 ms
// (10 fps) keeps the chart smooth without wasting CPU cycles.
// Data always enters the buffer immediately — only the visual is throttled.
const KRenderIntervalMs = 100

// ─── Backend SVG layout ─────────────────────────────────────────────────────

// KBackendWidth is the fixed width of the backend node in density units.
// Matches the historical visual; do not change without redrawing all
// connector spacings.
const KBackendWidth = 140

// KBackendPadTop is the vertical padding between the body top edge and
// the first port label.
const KBackendPadTop = 16.0

// KBackendPadBottom is the vertical padding below the last port label.
const KBackendPadBottom = 8.0

// KBackendPortSpacing is the vertical distance between consecutive
// port labels in the backend node.
const KBackendPortSpacing = 20.0

// ─── Frontend SVG layout ────────────────────────────────────────────────────

// KFrontendScale is the supersampling factor applied to the frontend
// SVG. The component dimensions in density units are multiplied by
// this factor before drawing, then scaled back by the renderer. Higher
// values reduce aliasing at the cost of SVG payload size.
const KFrontendScale = 2.0

// KPadL is the left padding of the plot area inside the frontend SVG,
// in scaled SVG units. Wide enough for 6-digit Y-axis labels.
const KPadL = 56.0

// KPadR is the right padding. Increased to KPadRWithRightAxis when a
// series is assigned to the right Y axis.
const KPadR = 16.0

// KPadRWithRightAxis is the right padding used when at least one series
// has YAxis == "right". Matches KPadL for visual symmetry.
const KPadRWithRightAxis = 56.0

// KPadT is the top padding of the plot area, leaving room for the
// title and latest-value overlays.
const KPadT = 16.0

// KPadB is the bottom padding of the plot area, leaving room for the
// X-axis time labels and stats line.
const KPadB = 32.0

// KLegendHeight is the extra vertical space reserved for the legend
// strip when showLegend is enabled and seriesCount > 1.
const KLegendHeight = 28.0

// KPanelBg is the background color of the entire frontend SVG.
const KPanelBg = "#0d1117"

// KPanelCornerRadius is the corner rounding of the frontend SVG panel.
const KPanelCornerRadius = 6.0

// KPlotBorderColor is the colour of the rectangle drawn around the plot
// area.
const KPlotBorderColor = "#333"

// KPlotBorderWidth is the stroke width of the plot border.
const KPlotBorderWidth = 1.0

// ─── Grid ───────────────────────────────────────────────────────────────────

// KGridDefaultColor is the colour used when the user has not set one
// explicitly via Inspect.
const KGridDefaultColor = "#ffffff"

// KGridDefaultOpacityPct is the default grid line opacity in percent.
// 25 means a faint guide that is visible but does not compete with the
// data.
const KGridDefaultOpacityPct = 25

// KGridDefaultWidth is the default grid stroke width.
const KGridDefaultWidth = 1.0

// KGridHorizontalDivisions is the number of horizontal grid lines
// drawn between top and bottom of the plot.
const KGridHorizontalDivisions = 4

// KGridVerticalDivisions is the number of vertical grid lines drawn
// between left and right of the plot.
const KGridVerticalDivisions = 6

// ─── Data rendering ─────────────────────────────────────────────────────────

// KLineWidth is the stroke width of polyline charts.
const KLineWidth = 3.0

// KLineGlowWidth is the wider stroke drawn beneath the main line when
// GlowLine is enabled for a series, producing a soft halo effect.
const KLineGlowWidth = 6.0

// KLineGlowOpacity is the opacity of the glow stroke. Kept as a string
// because SVG attribute values are strings.
const KLineGlowOpacity = "0.2"

// KScatterPointRadius is the radius of dots drawn for scatter series.
const KScatterPointRadius = 3.0

// KScatterGlowRadius is the wider circle drawn beneath the main point
// when GlowLine is enabled for a scatter series.
const KScatterGlowRadius = 6.0

// ─── Hover overlay ──────────────────────────────────────────────────────────

// KHoverDashStroke is the dasharray pattern used for the vertical
// crosshair line drawn at the hovered X position.
const KHoverDashStroke = "4,3"

// KHoverLineOpacity is the opacity of the crosshair line.
const KHoverLineOpacity = "0.3"

// KHoverPointRadius is the radius of the highlight circles drawn at
// the intersection of each series with the crosshair.
const KHoverPointRadius = 5.0

// KHoverTooltipBg is the background colour of the value tooltip near
// the crosshair, with alpha for translucency.
const KHoverTooltipBg = "#1e1e2eee"

// ─── Reset marker (timeline) ────────────────────────────────────────────────
//
// A RESET marker is rendered when the hardware's `t` value goes backwards.
// Per the §4.6.1 decision, we do not distinguish overflow from reboot —
// either is "something happened on the device" and the user gets a single
// visual signal.

// KResetMarkColor is the colour of the RESET vertical line. Amber
// (not red) to avoid confusion with red alert zones.
const KResetMarkColor = "#FFB400"

// KResetMarkDashStroke is the dash pattern of the RESET line.
const KResetMarkDashStroke = "6,4"

// KResetMarkWidth is the stroke width of the RESET line.
const KResetMarkWidth = 2.0

// KResetMarkOpacity is the opacity of the RESET line and label.
const KResetMarkOpacity = "0.85"

// KResetLabelBg is the background colour of the "RESET" label box at
// the top of the line, with alpha for translucency.
const KResetLabelBg = "#0d1117CC"

// KResetLabelText is the text shown on the marker label.
const KResetLabelText = "RESET"

// ─── Fail marker (timeline) ─────────────────────────────────────────────────
//
// A FAIL marker is rendered when the WebSocket reconnects after a drop.
// This is a server-side / network outage, not a hardware fault.

// KFailMarkColor is the colour of the FAIL vertical line. Red to
// communicate "infrastructure problem" distinctly from RESET.
const KFailMarkColor = "#E84545"

// KFailMarkDashStroke is the dash pattern of the FAIL line.
const KFailMarkDashStroke = "6,4"

// KFailMarkWidth is the stroke width of the FAIL line.
const KFailMarkWidth = 2.0

// KFailMarkOpacity is the opacity of the FAIL line and label.
const KFailMarkOpacity = "0.85"

// KFailLabelBg is the background colour of the "FAIL" label box.
const KFailLabelBg = "#0d1117CC"

// KFailLabelText is the text shown on the marker label.
const KFailLabelText = "FAIL"

// ─── Fail badge (corner of frontend panel) ──────────────────────────────────
//
// In addition to the timeline marker, a temporary badge appears in the
// top-right corner of the frontend panel so an operator who isn't
// staring at the X axis still sees that something happened.

// KFailBadgeBg is the badge background colour.
const KFailBadgeBg = "#E84545"

// KFailBadgeText is the badge text colour.
const KFailBadgeText = "#FFFFFF"

// KFailBadgeFadeMs is how long the badge stays visible after the
// connection drop event, in milliseconds.
const KFailBadgeFadeMs = 30_000

// ─── Gap indicator (between consecutive samples) ────────────────────────────
//
// Distinct from RESET (firmware time retrograde) and FAIL (WebSocket
// reconnect): a gap is detected purely by looking at the time delta
// between two consecutive samples in a series buffer. When the delta
// exceeds KGapDefaultThresholdMs, the segment connecting those two
// samples is drawn in a faded grey dashed line instead of the series
// colour — telling the operator "this stretch of the chart is
// interpolated across a hole; treat it as uncertain".
//
// Why this is useful in addition to FAIL:
//
//	The FAIL marker depends on the WebSocket reconnect handler
//	firing. In practice that handler can miss events: brief
//	connectivity blips that don't close the socket, server-side
//	pauses, very fast reconnects, etc. Detecting gaps by sample
//	cadence catches all of those without requiring a server event,
//	at the cost of being a heuristic (a slow sampler can look like
//	a gap).
//
// Per-device override:
//
//	StatementChartPro exposes "gapThresholdMs" in its Inspect.
//	Setting it to 0 disables gap rendering (all segments use the
//	series colour, legacy behaviour). Setting it too low for a
//	slow sampler will paint everything grey — the operator is
//	expected to tune it for their data rate.

// KGapDefaultThresholdMs is the default time delta (ms) between two
// consecutive samples above which the segment connecting them is
// drawn in the gap style. 1 second is conservative: it catches all
// real network drops at 5+ Hz while leaving normal samplers alone.
// Hardware sampling at 1 Hz or slower needs a higher value via Inspect.
const KGapDefaultThresholdMs = 1000

// KGapLineColor is the colour used to connect two samples across a
// detected gap. Medium grey — visible against the dark panel, not
// loud enough to compete with real data.
const KGapLineColor = "#777777"

// KGapLineDashStroke is the dasharray for the gap segment. Dashed
// reinforces "this is not a normal data segment".
const KGapLineDashStroke = "4,4"

// KGapLineWidth matches the regular line width so the gap segment
// reads as part of the same time series, just with degraded confidence.
const KGapLineWidth = 3.0

// KGapLineOpacity slightly fades the gap segment so the eye reads it
// as background context rather than primary data.
const KGapLineOpacity = "0.7"

// ─── Pause indicator (corner of frontend panel) ─────────────────────────────

// KPauseIndicatorBg is the background colour of the pause indicator
// shown in the top-right corner while paused.
const KPauseIndicatorBg = "#FFB400"

// KPauseIndicatorText is the text colour of the pause indicator.
const KPauseIndicatorText = "#000000"

// KPauseIndicatorLabel is the text shown on the pause indicator.
const KPauseIndicatorLabel = "PAUSED"

// ─── Font sizes (all in scaled SVG units, divide by KFrontendScale for CSS) ─

const (
	KFontSizeYAxis       = 22.0
	KFontSizeXAxis       = 16.0
	KFontSizeAwaitingMsg = 26.0
	KFontSizeTitle       = 24.0
	KFontSizeUnitTitle   = 22.0
	KFontSizeLatestValue = 36.0
	KFontSizeLatestUnit  = 20.0
	KFontSizeStats       = 20.0
	KFontSizeTimestamp   = 18.0
	KFontSizeLegend      = 18.0
	KFontSizeTooltip     = 16.0
	KFontSizeAlertLabel  = 16.0
	KFontSizeModeBadge   = 20.0
	KFontSizeResetLabel  = 16.0
	KFontSizeFailLabel   = 16.0
	KFontSizeFailBadge   = 18.0
	KFontSizePauseBadge  = 18.0
	KFontSizeLockIcon    = 18.0
)

// ─── Defaults ───────────────────────────────────────────────────────────────

// KFrontendInitialWidth is the default frontend width on creation.
const KFrontendInitialWidth = 300

// KFrontendInitialHeight is the default frontend height on creation.
const KFrontendInitialHeight = 180

// KFrontendMinWidth is the minimum width allowed when resizing.
const KFrontendMinWidth = 140

// KFrontendMinHeight is the minimum height allowed when resizing.
const KFrontendMinHeight = 80

// KAlertZoneDefaultColor is the colour assigned to alert zones at
// creation, before the user customises one.
const KAlertZoneDefaultColor = "#FF3333"

// KAlertZoneDefaultOpacityPct is the default opacity in percent for
// alert zone bands.
const KAlertZoneDefaultOpacityPct = 15

// ─── Y axis enum values ─────────────────────────────────────────────────────

// KYAxisLeft is the value of the YAxis field meaning "plot this series
// against the left Y axis".
const KYAxisLeft = "left"

// KYAxisRight assigns a series to the right Y axis.
const KYAxisRight = "right"

// ─── Chart type enum values ─────────────────────────────────────────────────

// KChartTypeLine renders the buffer of a series as a connected polyline.
const KChartTypeLine = "line"

// KChartTypeScatter renders the buffer of a series as individual dots.
const KChartTypeScatter = "scatter"

// ─── Menu icon SVG paths ────────────────────────────────────────────────────
//
// FontAwesome 6 Free paths inlined here rather than registered in the
// global rulesIcon package, to keep ChartPro-specific menu items
// self-contained. If/when these are reused elsewhere, move them.

// KIconPathPause is the pause icon used in the frontend context menu
// while the chart is live.
const KIconPathPause = "M48 32C21.5 32 0 53.5 0 80L0 432c0 26.5 21.5 48 48 48l64 0c26.5 0 48-21.5 48-48l0-352c0-26.5-21.5-48-48-48L48 32zm224 0c-26.5 0-48 21.5-48 48l0 352c0 26.5 21.5 48 48 48l64 0c26.5 0 48-21.5 48-48l0-352c0-26.5-21.5-48-48-48l-64 0z"

// KIconViewBoxPause is the viewBox attribute matching KIconPathPause.
const KIconViewBoxPause = "0 0 384 512"

// KIconPathPlay is the play (resume) icon used while paused.
const KIconPathPlay = "M73 39c-14.8-9.1-33.4-9.4-48.5-.9S0 62.6 0 80L0 432c0 17.4 9.4 33.4 24.5 41.9s33.7 8.1 48.5-.9L361 297c14.3-8.7 23-24.2 23-41s-8.7-32.2-23-41L73 39z"

// KIconViewBoxPlay matches KIconPathPlay.
const KIconViewBoxPlay = "0 0 384 512"

// KIconPathCamera is the camera icon used for the snapshot menu item.
const KIconPathCamera = "M149.1 64.8L138.7 96 64 96C28.7 96 0 124.7 0 160L0 416c0 35.3 28.7 64 64 64l384 0c35.3 0 64-28.7 64-64l0-256c0-35.3-28.7-64-64-64l-74.7 0-10.4-31.2C356.4 45.2 338.1 32 317.4 32L194.6 32c-20.7 0-39 13.2-45.5 32.8zM256 192a96 96 0 1 1 0 192 96 96 0 1 1 0-192z"

// KIconViewBoxCamera matches KIconPathCamera.
const KIconViewBoxCamera = "0 0 512 512"

// KIconPathEraser is the eraser icon used for the clear menu item.
const KIconPathEraser = "M178.5 416l123 0 65.3-65.3-173.5-173.5-126.7 126.7 112 112zM224 480l-45.5 0c-17 0-33.3-6.7-45.3-18.7L17 345C6.1 334.1 0 319.4 0 304s6.1-30.1 17-41L263 17C273.9 6.1 288.6 0 304 0s30.1 6.1 41 17L527 199c10.9 10.9 17 25.6 17 41s-6.1 30.1-17 41l-135 135 120 0c17.7 0 32 14.3 32 32s-14.3 32-32 32l-288 0z"

// KIconViewBoxEraser matches KIconPathEraser.
const KIconViewBoxEraser = "0 0 576 512"
