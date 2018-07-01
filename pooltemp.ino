#include <WiFi101.h>
#include <Wire.h>
#include <Adafruit_GFX.h>
#include "Adafruit_LEDBackpack.h"
#include "secrets.h"

Adafruit_AlphaNum4 alpha4 = Adafruit_AlphaNum4();
WiFiClient client;

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
  Serial.begin(9600);
  // Give the serial port a chance to connect so we don't miss messages.
  // But don't block for long in case there's none connected.
  delay(100);
  Serial.println("Starting up");
  
  alpha4.begin(0x70);
  alpha4.clear();
  alpha4.writeDisplay();

  //Configure pins for Adafruit ATWINC1500 Feather
  WiFi.setPins(8,7,4,2);

  showMessage("WiFi");

  // check for the presence of the shield:
  if (WiFi.status() == WL_NO_SHIELD) {
    Serial.println("No WiFi found");
    showMessage("NOWS");
    // don't continue:
    while (true);
  }

  // attempt to connect to WiFi network:
  int status;
  do {
    Serial.print("Trying to connect to SSID: ");
    Serial.println(SECRET_SSID);
    status = WiFi.begin(SECRET_SSID, SECRET_PASS);
    if (status != WL_CONNECTED)
      delay(10000);
  } while (status != WL_CONNECTED);

  Serial.println("Connected to WiFi");
  showMessage("HTTP");  
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

void loop() {
  unsigned long time = millis();
  if (lastAttempt > time || lastUpdate > time) {
    // Overflow protection (~50 days)
    lastAttempt = 0;
    lastUpdate = 0;
  }
  // Only attempt an update every 5 minutes
  if (lastAttempt && time - lastAttempt < 60 * 5 * 1000)
    return;
  lastAttempt = time;

  Serial.println("Attempting to connect");

  if (!client.connect("rbyers-pooltemp.appspot.com", 80)) {
    Serial.println("Connection failed");
    if (time - lastUpdate > 60 * 15 * 1000) {
      // Been more than 15 minutes since we got the data, show error
      showMessage("FAIL");
    }
    return;
  }

  // Make the HTTP request
  client.println("GET /display HTTP/1.1");
  client.println("Host: rbyers-pooltemp.appspot.com");
  client.println("Connection: close");
  client.println("User-Agent: RByers Arduino pooltemp"); 
  client.println();

  Serial.println("Request sent");

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
        Serial.print("Invalid HTTP response: ");
        Serial.println(s);
        showMessage("HTER");
        break;
      }
      String status = s.substring(i+1, i+4);
      if (status != "200") {
        Serial.print("HTTP Failed: ");
        Serial.println(s);
        showMessage("HSER");
        break;
      }
    }
    if (doneHeaders) {
      // First line of the body - just show it
      Serial.print("Updating display: ");
      Serial.println(s);
      showMessage(s.c_str());
      lastUpdate = time;
      break;
    }
    if (!doneHeaders && s == "") {
      doneHeaders = true;
    }
  }
  
  client.stop();
}
