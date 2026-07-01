#!/usr/bin/env bash
# =====================================================================
#  simulate_sensors.sh — Simulates IoT sensor data for IoTMaker dashboard
#
#  Sends realistic sensor values to the webhook endpoint at configurable
#  intervals. Supports all compFrontend components: Gauge, LED, BarGraph,
#  SevenSeg, Knob, Button, TextDisplay, and Chart.
#
#  Usage:
#    1. Create an API key in the IoTMaker portal (Live → API Keys)
#    2. Edit the variables below (API_KEY, PROJECT_ID, device IDs)
#    3. Run: chmod +x simulate_sensors.sh && ./simulate_sensors.sh
#
#  Requires: curl, bc (math), bash 4+
#
#  Português: Simula dados de sensores IoT para o dashboard do IoTMaker.
#  Envia valores realistas para o endpoint webhook em intervalos configuráveis.
# =====================================================================

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────

BASE_URL="${BASE_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-YOUR_API_KEY_HERE}"       # ← paste your API key
PROJECT_ID="${PROJECT_ID:-YOUR_PROJECT_ID}"      # ← paste your project ID

# Update interval in seconds (0.5 = 2 updates/sec, 1 = 1/sec, etc.)
INTERVAL="${INTERVAL:-1}"

# ── Device IDs (must match the IDs on your canvas) ─────────────────────
# Comment out any devices you haven't placed yet.

GAUGE_ID="${GAUGE_ID:-gauge_1}"       # StatementGauge  — temperature (0-50°C)
LED_ID="${LED_ID:-led_1}"           # StatementLED    — alarm indicator
BARGRAPH_ID="${BARGRAPH_ID:-bar_1}"      # StatementBarGraph — humidity (0-100%)
SEVENSEG_ID="${SEVENSEG_ID:-seg_1}"      # StatementSevenSeg — counter
KNOB_ID="${KNOB_ID:-knob_1}"         # StatementKnob   — pressure (900-1100 hPa)
CHART_ID="${CHART_ID:-chart_1}"       # StatementChart   — temperature line
CHART_PORT="${CHART_PORT:-current}"   # Port name: "current" for Chart, "s0"/"s1"/… for ChartPro
TEXT_ID="${TEXT_ID:-text_1}"          # StatementTextDisplay — status message

# ── Simulation mode ───────────────────────────────────────────────────
# "sensors"  — realistic temperature, humidity, pressure data
# "ecg"      — simulated PQRST heartbeat waveform (for Chart in sweep mode)
# "counter"  — simple incrementing counter (for SevenSeg)
# "all"      — all of the above combined

MODE="${MODE:-all}"

# ── Color output ──────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# =====================================================================
#  Helper functions
# =====================================================================

# send_batch sends a JSON array of updates to the webhook endpoint.
# Args: JSON string (array of {device_id, port, value})
send_batch() {
    local payload="$1"
    local response
    response=$(curl -s -w "\n%{http_code}" \
        -X POST "${BASE_URL}/api/v1/webhook/${PROJECT_ID}" \
        -H "Content-Type: application/json" \
        -H "X-API-Key: ${API_KEY}" \
        -d "${payload}" 2>&1)

    local http_code
    http_code=$(echo "$response" | tail -1)
    local body
    body=$(echo "$response" | sed '$d')

    if [[ "$http_code" == "200" ]]; then
        return 0
    else
        echo -e "${RED}HTTP ${http_code}: ${body}${NC}" >&2
        return 1
    fi
}

# random_float generates a random float: base ± variance
# Args: base variance [decimals=1]
random_float() {
    local base=$1
    local variance=$2
    local decimals=${3:-1}
    local delta
    delta=$(echo "scale=4; ($RANDOM / 32767 - 0.48) * $variance * 2" | bc)
    echo "scale=${decimals}; ${base} + ${delta}" | bc
}

# clamp keeps a value within min..max
# Args: value min max
clamp() {
    local val=$1 min=$2 max=$3
    echo "$val $min $max" | awk '{v=$1; if(v<$2) v=$2; if(v>$3) v=$3; printf "%.1f", v}'
}

# =====================================================================
#  ECG waveform generator
#
#  Generates a single PQRST beat cycle as a float value.
#  phase: 0.0 to 1.0 within one beat period.
# =====================================================================

ecg_value() {
    local phase=$1
    # Simplified PQRST using awk for float math
    echo "$phase" | awk '{
        p = $1
        if (p < 0.08)       v = 0.12 * sin(p / 0.08 * 3.14159)
        else if (p < 0.12)  v = 0
        else if (p < 0.16)  v = -0.08
        else if (p < 0.20)  v = -0.08 + (p - 0.16) / 0.04 * 1.08
        else if (p < 0.24)  v = 1.0 - (p - 0.20) / 0.04 * 1.3
        else if (p < 0.28)  v = -0.3 + (p - 0.24) / 0.04 * 0.3
        else if (p < 0.36)  v = 0
        else if (p < 0.50)  v = 0.18 * sin((p - 0.36) / 0.14 * 3.14159)
        else                v = 0
        printf "%.2f", v * 100
    }'
}

# =====================================================================
#  Main simulation loop
# =====================================================================

echo -e "${GREEN}╔════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  IoTMaker Sensor Simulator                    ║${NC}"
echo -e "${GREEN}╠════════════════════════════════════════════════╣${NC}"
echo -e "${GREEN}║${NC}  Server:   ${CYAN}${BASE_URL}${NC}"
echo -e "${GREEN}║${NC}  Project:  ${CYAN}${PROJECT_ID}${NC}"
echo -e "${GREEN}║${NC}  Mode:     ${CYAN}${MODE}${NC}"
echo -e "${GREEN}║${NC}  Interval: ${CYAN}${INTERVAL}s${NC}"
echo -e "${GREEN}║${NC}  ChartPort:${CYAN}${CHART_PORT}${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop${NC}"
echo ""

# State variables (persist across iterations for smooth curves)
temp=23.0
humidity=65.0
pressure=1013.0
light=500.0
counter=0
ecg_phase=0.0
ecg_step=0.02       # phase increment per tick (adjust for heart rate)
led_state="false"
tick=0

while true; do
    tick=$((tick + 1))
    items=""

    # ── Sensor simulation ──────────────────────────────────────────────
    if [[ "$MODE" == "sensors" || "$MODE" == "all" ]]; then
        # Temperature: slow drift around 23°C (range 15-35)
        temp=$(random_float "$temp" 0.3)
        temp=$(clamp "$temp" 15.0 35.0)
        temp_int=$(echo "$temp" | awk '{printf "%d", $1}')

        # Humidity: moderate variation around 65% (range 30-95)
        humidity=$(random_float "$humidity" 0.8)
        humidity=$(clamp "$humidity" 30.0 95.0)
        humidity_int=$(echo "$humidity" | awk '{printf "%d", $1}')

        # Pressure: very slow drift (range 980-1040 hPa)
        pressure=$(random_float "$pressure" 0.2)
        pressure=$(clamp "$pressure" 980.0 1040.0)
        pressure_int=$(echo "$pressure" | awk '{printf "%d", $1}')

        # Light: varies more (range 0-1200 lux)
        light=$(random_float "$light" 15)
        light=$(clamp "$light" 0.0 1200.0)

        # LED alarm: on when temp > 30
        if (( $(echo "$temp > 30" | bc -l) )); then
            led_state="true"
        else
            led_state="false"
        fi

        # Build batch items
        [[ -n "$GAUGE_ID" ]] && items="${items}{\"device_id\":\"${GAUGE_ID}\",\"port\":\"current\",\"value\":${temp_int}},"
        [[ -n "$LED_ID" ]] && items="${items}{\"device_id\":\"${LED_ID}\",\"port\":\"current\",\"value\":${led_state}},"
        [[ -n "$BARGRAPH_ID" ]] && items="${items}{\"device_id\":\"${BARGRAPH_ID}\",\"port\":\"current\",\"value\":${humidity_int}},"
        [[ -n "$KNOB_ID" ]] && items="${items}{\"device_id\":\"${KNOB_ID}\",\"port\":\"current\",\"value\":${pressure_int}},"
        [[ -n "$CHART_ID" ]] && items="${items}{\"device_id\":\"${CHART_ID}\",\"port\":\"${CHART_PORT}\",\"value\":${temp_int}},"

        # Status text (every 5 ticks)
        if [[ -n "$TEXT_ID" ]] && (( tick % 5 == 0 )); then
            status_text="Temp: ${temp_int}°C | Hum: ${humidity_int}% | Press: ${pressure_int}hPa"
            items="${items}{\"device_id\":\"${TEXT_ID}\",\"port\":\"current\",\"value\":\"${status_text}\"},"
        fi

        echo -e "${BLUE}[sensors]${NC} temp=${temp_int}°C  hum=${humidity_int}%  press=${pressure_int}hPa  led=${led_state}"
    fi

    # ── ECG simulation (3 series: s0, s1, s2) ────────────────────────
    if [[ "$MODE" == "ecg" || "$MODE" == "all" ]]; then
        # s0: normal ECG waveform
        ecg_s0=$(ecg_value "$ecg_phase")

        # s1: phase-shifted ECG (delayed heartbeat — like a second lead)
        ecg_s1_phase=$(echo "$ecg_phase + 0.33" | bc)
        if (( $(echo "$ecg_s1_phase >= 1.0" | bc -l) )); then
            ecg_s1_phase=$(echo "$ecg_s1_phase - 1.0" | bc)
        fi
        ecg_s1=$(ecg_value "$ecg_s1_phase")

        # s2: slow sine wave (respiration signal, ~4x slower than heartbeat)
        ecg_s2=$(echo "$ecg_phase" | awk '{printf "%.2f", sin($1 * 2 * 3.14159 * 0.25) * 40}')

        ecg_phase=$(echo "$ecg_phase + $ecg_step" | bc)
        if (( $(echo "$ecg_phase >= 1.0" | bc -l) )); then
            ecg_phase=$(echo "$ecg_phase - 1.0" | bc)
        fi

        if [[ -n "${CHART_ID:-}" ]]; then
            items="${items}{\"device_id\":\"${CHART_ID}\",\"port\":\"s0\",\"value\":${ecg_s0}},"
            items="${items}{\"device_id\":\"${CHART_ID}\",\"port\":\"s1\",\"value\":${ecg_s1}},"
            items="${items}{\"device_id\":\"${CHART_ID}\",\"port\":\"s2\",\"value\":${ecg_s2}},"
        fi

        echo -e "${CYAN}[ecg]${NC} phase=$(printf '%.2f' "$ecg_phase")  s0=${ecg_s0}  s1=${ecg_s1}  s2=${ecg_s2}"
    fi

    # ── Counter simulation ─────────────────────────────────────────────
    if [[ "$MODE" == "counter" || "$MODE" == "all" ]]; then
        counter=$((counter + 1))
        if (( counter > 999 )); then
            counter=0
        fi

        [[ -n "$SEVENSEG_ID" ]] && items="${items}{\"device_id\":\"${SEVENSEG_ID}\",\"port\":\"current\",\"value\":${counter}},"

        echo -e "${YELLOW}[counter]${NC} value=${counter}"
    fi

    # ── Send batch ─────────────────────────────────────────────────────
    if [[ -n "$items" ]]; then
        # Remove trailing comma and wrap in array
        items="${items%,}"
        payload="[${items}]"

        if send_batch "$payload"; then
            echo -e "  ${GREEN}✓ sent${NC} ($(echo "$payload" | wc -c) bytes)"
        else
            echo -e "  ${RED}✗ failed${NC}"
        fi
    fi

    echo ""
    sleep "$INTERVAL"
done
