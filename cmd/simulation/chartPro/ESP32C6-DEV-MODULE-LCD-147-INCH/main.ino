// Demo: fonte de dados ChartPro (porte do case2.go)
// Placa: Waveshare ESP32-C6-LCD-1.47
//
// Envia seno em s0 e cosseno em s1 (90° defasados) para um servidor
// ChartPro via POST com header X-API-Key. A tela mostra os valores
// atuais, contagem de ticks e erros em tempo real.

#include <Adafruit_GFX.h>
#include <Adafruit_ST7789.h>
#include <SPI.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include <math.h>

// ===== CONFIGURE =====
const char* WIFI_SSID     = "VIVOFIBRA-E151";
const char* WIFI_PASSWORD = "naotenhosenha1";

// IP do PC dev rodando o ChartPro (mesma rede da placa)
const char* SERVER_HOST = "192.168.15.5";
const int   SERVER_PORT = 8080;

const char* PROJECT_ID = "e12cbe6e04034c27eb79a5f311d3ecbf";
const char* API_KEY    = "f160ecfac3c85719776a19a626e22aff7d5f2327beaa1112140c280ee4be6131";
const char* DEVICE_ID  = "chartPro_1";

// Geracao de sinal (mesmos valores do case2.go)
const unsigned long INTERVAL_MS    = 50;     // 20Hz
const float         SINE_PERIOD_S  = 3.0f;
const float         AMPLITUDE      = 50.0f;
const float         OFFSET         = 50.0f;
// =====================

// Display
#define TFT_SCLK  7
#define TFT_MOSI  6
#define TFT_RST   21
#define TFT_DC    15
#define TFT_CS    14
#define TFT_BL    22
#define TFT_W  172
#define TFT_H  320

#define COR_GRID 0x4208   // cinza escuro 565

Adafruit_ST7789 tft = Adafruit_ST7789(&SPI, TFT_CS, TFT_DC, TFT_RST);

// Estado
String        webhookURL;
unsigned long startMillis = 0;
unsigned long lastSend    = 0;
unsigned long tick        = 0;
unsigned long erros       = 0;
int           ultimoCode  = 0;

// HTTPClient global pra reusar conexao TCP (importante a 20Hz)
HTTPClient    http;
WiFiClient    wifiClient;

// ----- UI helpers -----

void escreve(int x, int y, const char* txt, uint16_t cor, uint8_t size = 1) {
  tft.setTextColor(cor);
  tft.setTextSize(size);
  tft.setCursor(x, y);
  tft.print(txt);
}

void desenhaUI() {
  tft.fillScreen(ST77XX_BLACK);
  escreve(10, 10, "ChartPro Sim", ST77XX_WHITE, 2);
  escreve(10, 35, "case2: sin/cos", ST77XX_CYAN);
  tft.drawFastHLine(0, 50, TFT_W, COR_GRID);

  escreve(10, 60,  "IP:",       ST77XX_YELLOW);
  escreve(10, 100, "Ticks:",    ST77XX_YELLOW);
  escreve(10, 140, "s0 (sin):", ST77XX_YELLOW);
  escreve(10, 195, "s1 (cos):", ST77XX_YELLOW);
  escreve(10, 250, "Erros:",    ST77XX_YELLOW);
}

void atualizaIP() {
  String ip = WiFi.localIP().toString();
  escreve(10, 75, ip.c_str(), ST77XX_WHITE);
}

void atualizaTela(float s0, float s1) {
  char buf[32];

  // Ticks
  tft.fillRect(10, 115, 130, 20, ST77XX_BLACK);
  snprintf(buf, sizeof(buf), "%lu", tick);
  escreve(10, 115, buf, ST77XX_GREEN, 2);

  // s0 valor + barra
  tft.fillRect(10, 155, 140, 30, ST77XX_BLACK);
  snprintf(buf, sizeof(buf), "%.2f", s0);
  escreve(10, 160, buf, ST77XX_RED, 3);
  int barW0 = (int)((s0 / 100.0f) * (TFT_W - 20));
  if (barW0 < 0) barW0 = 0;
  if (barW0 > TFT_W - 20) barW0 = TFT_W - 20;
  tft.fillRect(10, 185, TFT_W - 20, 4, COR_GRID);
  tft.fillRect(10, 185, barW0,      4, ST77XX_RED);

  // s1 valor + barra
  tft.fillRect(10, 210, 140, 30, ST77XX_BLACK);
  snprintf(buf, sizeof(buf), "%.2f", s1);
  escreve(10, 215, buf, ST77XX_BLUE, 3);
  int barW1 = (int)((s1 / 100.0f) * (TFT_W - 20));
  if (barW1 < 0) barW1 = 0;
  if (barW1 > TFT_W - 20) barW1 = TFT_W - 20;
  tft.fillRect(10, 240, TFT_W - 20, 4, COR_GRID);
  tft.fillRect(10, 240, barW1,      4, ST77XX_BLUE);

  // Erros + ultimo HTTP
  tft.fillRect(10, 265, TFT_W - 20, 20, ST77XX_BLACK);
  snprintf(buf, sizeof(buf), "%lu (HTTP %d)", erros, ultimoCode);
  uint16_t cor = (erros == 0) ? ST77XX_GREEN : ST77XX_RED;
  escreve(10, 265, buf, cor, 2);
}

// ----- WiFi -----

void conectaWiFi() {
  tft.fillScreen(ST77XX_BLACK);
  escreve(10, 10, "Conectando", ST77XX_WHITE, 2);
  escreve(10, 40, WIFI_SSID, ST77XX_CYAN);

  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);

  int t = 0;
  while (WiFi.status() != WL_CONNECTED && t < 30) {
    delay(500);
    Serial.print(".");
    int x = 10 + (t % 20) * 7;
    tft.fillRect(x, 70, 5, 5, ST77XX_GREEN);
    t++;
  }
  Serial.println();

  if (WiFi.status() != WL_CONNECTED) {
    escreve(10, 100, "FALHOU", ST77XX_RED, 2);
    while (true) delay(1000);
  }
  Serial.printf("Conectado. IP: %s  RSSI: %d dBm\n",
                WiFi.localIP().toString().c_str(), WiFi.RSSI());
}

// ----- HTTP POST -----

bool enviaPayload(float s0, float s1) {
  if (WiFi.status() != WL_CONNECTED) return false;

  // monta JSON sem precisar de ArduinoJson — array de 2 itens
  char body[256];
  snprintf(body, sizeof(body),
    "[{\"device_id\":\"%s\",\"port\":\"s0\",\"value\":%.2f},"
     "{\"device_id\":\"%s\",\"port\":\"s1\",\"value\":%.2f}]",
    DEVICE_ID, s0, DEVICE_ID, s1);

  http.begin(wifiClient, webhookURL);
  http.addHeader("Content-Type", "application/json");
  http.addHeader("X-API-Key", API_KEY);
  http.setReuse(true);            // mantem TCP aberto entre requests
  http.setTimeout(2000);          // 2s, igual ao Go

  int code = http.POST(body);
  http.end();

  ultimoCode = code;
  return (code == 200);
}

// ----- Setup / Loop -----

void setup() {
  Serial.begin(115200);
  delay(500);

  SPI.begin(TFT_SCLK, -1, TFT_MOSI, -1);
  pinMode(TFT_BL, OUTPUT);
  digitalWrite(TFT_BL, HIGH);
  tft.init(TFT_W, TFT_H);
  tft.setSPISpeed(40000000);
  tft.setRotation(0);

  conectaWiFi();

  webhookURL = String("http://") + SERVER_HOST + ":" + SERVER_PORT
             + "/api/v1/webhook/" + PROJECT_ID;
  Serial.print("URL: "); Serial.println(webhookURL);

  desenhaUI();
  atualizaIP();

  startMillis = millis();
}

void loop() {
  unsigned long now = millis();
  if (now - lastSend < INTERVAL_MS) return;
  lastSend = now;

  tick++;

  float elapsed = (now - startMillis) / 1000.0f;
  float phase   = (elapsed / SINE_PERIOD_S) * 2.0f * (float)M_PI;

  // arredonda pra 2 casas, mesma logica do Go
  float s0 = roundf((OFFSET + AMPLITUDE * sinf(phase)) * 100.0f) / 100.0f;
  float s1 = roundf((OFFSET + AMPLITUDE * cosf(phase)) * 100.0f) / 100.0f;

  if (!enviaPayload(s0, s1)) {
    erros++;
  }

  // atualiza display a cada 4 ticks (~5Hz). A 20Hz a SPI satura visualmente
  // e nao da pra ler nada de qualquer jeito.
  if (tick % 4 == 0) {
    atualizaTela(s0, s1);
  }

  if (tick % 20 == 0) {
    Serial.printf("tick %lu  s0=%.2f  s1=%.2f  erros=%lu\n",
                  tick, s0, s1, erros);
  }
}
