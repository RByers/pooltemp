#include <Adafruit_SleepyDog.h>
#include <WiFi101.h>
#include <Wire.h>
#include <Adafruit_GFX.h>
#include "Adafruit_LEDBackpack.h"
#include "secrets.h"

Adafruit_AlphaNum4 alpha4 = Adafruit_AlphaNum4();

const char* server = "rbyers-pooltemp.appspot.com";

// Update data every 2 minutes
const int updateIntervalSec = 2 * 60;
//const int updateIntervalSec = 10;

// Don't display data older than 15 minutes
const int maxStaleSec = 15 * 60;

void log(String msg, bool newline = true) {
  unsigned long time = millis() / 1000;
  unsigned int sec = time % 60;
  time /= 60;
  unsigned int min = time % 60;
  time /= 60;
  Serial.print(time);
  Serial.print(":");
  if (min < 10)
    Serial.print("0");
  Serial.print(min);
  Serial.print(":");
  if (sec < 10)
    Serial.print("0");
  Serial.print(sec);
  Serial.print(": ");
  Serial.print(msg);
  if (newline)
    Serial.println();
}

void showMessage(const char* msg) {
  alpha4.clear();
  const char* p = msg;
  for (int i = 0; i < 4 && *p; i++, p++) {
    bool dot = false;
    if (*(p+1) == '.') {
      dot = true;
    }
    alpha4.writeDigitAscii(i, *p, dot);
    if (dot)
      p++;
  }
  alpha4.writeDisplay();
}

void setup() {
  alpha4.begin(0x70);
  alpha4.clear();
  alpha4.writeDisplay();
  showMessage("BOOT");
  
  Serial.begin(9600);
  
  //Configure pins for Adafruit ATWINC1500 Feather
  WiFi.setPins(8,7,4,2);

  // check for the presence of the shield:
  if (WiFi.status() == WL_NO_SHIELD) {
    log("No WiFi found");
    showMessage("NOWS");
    // don't continue:
    while (true);
  }
  
  // Limits receives to the 100ms beacon - should be fine for our purposes.
  WiFi.maxLowPowerMode(); 

  // Give the serial port a chance to connect so we don't miss messages.
  // But don't block for long in case there's none connected.
  delay(2000);
  log("Starting up");  
}

String readLine(WiFiClient client) {
  String s;
  while (client.available() || client.connected()) {
    if (client.available()) {
      char c = client.read();
      if (c == '\n')
        break;
      if (c == '\r')
        continue;
      s.concat(c);  
    }
  }
  return s;
}

unsigned long lastUpdate = 0;
unsigned long lastAttempt = 0;

bool haveTemps() {
  return (lastUpdate && millis() - lastUpdate < maxStaleSec * 1000);
}

String WiFiStatus(int status) {
  switch(status) {
  case WL_IDLE_STATUS:
    return "WL_IDLE_STATUS";  
  case WL_NO_SSID_AVAIL:
    return "WL_NO_SSID_AVAIL";  
  case WL_CONNECTED:
    return "WL_CONNECTED";  
  case WL_CONNECT_FAILED:
    return "WL_CONNECT_FAILED";  
  case WL_CONNECTION_LOST:
    return "WL_CONNECTION_LOST";  
  case WL_DISCONNECTED:
    return "WL_DISCONNECTED";  
  default:
    return String(status);
  }
}

void loop() {
  unsigned long time = millis();
  
  if (lastAttempt > time || lastUpdate > time) {
    // Overflow protection (~50 days)
    lastAttempt = 1;
    lastUpdate = 1;
  }
  if (lastAttempt && time - lastAttempt < updateIntervalSec * 1000)
    return;
  lastAttempt = time;

  // attempt to connect to WiFi network:
  int status;
  while(status = WiFi.status() != WL_CONNECTED) {
    if (!haveTemps())
      showMessage("WiFi");
    log("WiFi Status: " + WiFiStatus(status));
    log(String("Trying to connect to SSID: ") + SECRET_SSID);
    status = WiFi.begin(SECRET_SSID, SECRET_PASS);
    if (status == WL_CONNECTED) {
      log(String("Connected to WiFi, RSSI:") + WiFi.RSSI());
    } else {
      log("WiFi connect failed: " + WiFiStatus(status));
      if (!haveTemps())
        showMessage("WiFL");
      delay(2000);
    }
  }

  WiFiClient client;
  log(String("WiFi RSSI: ") + WiFi.RSSI());
  log(String("Attempting to connect to ") + server);

  if (!haveTemps())
      showMessage("HTTP");

  if (!client.connect(server, 80)) {
    log("Connection failed");
    if (!haveTemps())
      showMessage("FAIL");
    client.stop();
  } else {
    // Make the HTTP request
    client.println("GET /display HTTP/1.1");
    client.print("Host: ");
    client.println(server);
    client.println("Connection: close");
    client.println("User-Agent: RByers Arduino pooltemp"); 
    client.println();
  
    log("Request sent");
  
    bool doneHeaders = false;
    bool doneStatus = false;
    while(client.connected()) {
      String s = readLine(client);
      //Serial.print("Read line: ");
      //Serial.println(s);
      
      if (!doneStatus) {
        doneStatus = true;
        int i = s.indexOf(" ");
        if (i == -1) {
          log("Invalid HTTP response: " + s);
          if (!haveTemps())
            showMessage("HTER");
          break;
        }
        String status = s.substring(i+1, i+4);
        if (status != "200") {
          log("HTTP Failed: " + s);
          if (!haveTemps())
            showMessage("HERR");
          break;
        }
      }
      if (doneHeaders) {
        // First line of the body - just show it
        if (haveTemps() && s == "OFFLINE") {
          log("Got offline");
        } else {
          log("Updating display: " + s);
          showMessage(s.c_str());
          lastUpdate = time;
        }
        break;
      }
      if (!doneHeaders && s == "") {
        doneHeaders = true;
      }
    }
    client.stop();
  }

  // The WiFi chip appears to draw ~100mA even in max power saving mode
  // (at least according to my cheap USB power monitor). Just disconnect
  // between update intervals to save power.
  WiFi.disconnect();

  // If we've been running long enough that a sketch upload is unlikely,
  // go to sleep until the next update time.
  if (millis() > 60 * 1000) {
    log(String("Sleeping for ") + updateIntervalSec + " seconds");
    Watchdog.sleep(updateIntervalSec * 1000);
  }
}
