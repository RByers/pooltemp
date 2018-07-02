package main

import (
    "fmt"
    "context"
    "errors"
    "strings"
    "strconv"
    "time"
    "encoding/json"
    "io/ioutil"
    "net/http"
    
    "google.golang.org/appengine"
    "google.golang.org/appengine/datastore"
    "google.golang.org/appengine/log"
    "google.golang.org/appengine/urlfetch"
)

const apiKey = "EOOEMOW4YR6QNB07"

// TODO: Take timeZone as a parameter?
const timeZone = "Canada/Eastern"

// Some invalid temp value to indicate error
const tempErr = -99

type Session struct {
    AuthenticationToken string  `datastore:"authentication_token" json:"authentication_token"`
    DeviceSerial        string  `datastore:"device_serial"`
    Id                  string  `datastore:"id" json:"session_id"` 
    UserId              int     `datastore:"user_id" json:"id"`
}

type Temps struct {
    Air         int         `datastore:"air"`
    Heater      int         `datastore:"heater"`
    Pool        int         `datastore:"pool"`
    Timestamp   time.Time   `datastore:"timestamp"` 
}

type LatestTemps struct {
    Temps
    Keep     bool   `datastore:"keep"`
}

func logHandler(response http.ResponseWriter, request *http.Request) {
    ctx := appengine.NewContext(request)
    err := doLog(ctx, response)
    if err != nil {
        log.Criticalf(ctx, err.Error())
        http.Error(response, err.Error(), http.StatusInternalServerError)
    }
}

func tempStr(i int) string {
    if (i < 0) {
        return ""
    }
    return strconv.Itoa(i)
}

func doLog(ctx context.Context, response http.ResponseWriter) error {
    loc, err := time.LoadLocation(timeZone)
    if err != nil {
        return errors.New("Failed to load timezone: " + err.Error())
    }
    
    // After this point errors won't actually prevent the success status
    // But we still get the errors in the log, so it's not a huge problem
    response.Header().Set("Content-Type", "text/csv; charset=utf-8")

    query := datastore.NewQuery("Temps").
        Order("-timestamp")
    fmt.Fprintf(response, "timesamp, air, pool, heater\n")
    for it := query.Run(ctx); ; {
        var temps Temps
        // TODO: Handle type mismatch for legacy entries with empty strings
        temps.Air = tempErr
        temps.Heater = tempErr
        temps.Pool = tempErr
        _, err := it.Next(&temps)
        if err == datastore.Done {
            break
        }
        if _, ok := err.(*datastore.ErrFieldMismatch); ok {
            // TODO: Migrate data to use tempErr for missing
            //if ferr.FieldName == "keep" {
                // Ignore the Keep field for the "latest" entry
                err = nil
            //}
        }
        if err != nil {
            return errors.New("Query Next failed: " + err.Error())
        }
        // TODO: Timezone
        fmt.Fprintf(response, "%s, %s, %s, %s\n",
            temps.Timestamp.In(loc).Format("1/2/2006 15:04"),
            tempStr(temps.Air), tempStr(temps.Pool), tempStr(temps.Heater))
    }
   return nil
 }

func sessionKey(ctx context.Context) *datastore.Key {
    return datastore.NewKey(ctx, "Session", "default", 0, nil);
}

func updateHandler(response http.ResponseWriter, request *http.Request) {
    ctx := appengine.NewContext(request)
    err := doUpdate(ctx, response)
    if err != nil {
        log.Criticalf(ctx, err.Error())
        http.Error(response, err.Error(), http.StatusInternalServerError)
    }
}

func doUpdate(ctx context.Context, response http.ResponseWriter) error {
    session, err := getSession(ctx)
    if err != nil {
        return err
    }
    
    temps, err := getTemps(ctx, session, 2)
    if err != nil {
        return err
    }
    temps.Timestamp = time.Now()
    
    // Get the running entry
    latestKey := datastore.NewKey(ctx, "Temps", "latest", 0, nil)
    var latest LatestTemps
    err = datastore.Get(ctx, latestKey, &latest)
    if err != nil && err != datastore.ErrNoSuchEntity {
        return errors.New("Failed to get latest Temps: " + err.Error())
    }
    haveLatest := err == nil

    // If the temperatures haven't changed and we don't need to keep the running entry
    same := haveLatest && temps.Air == latest.Air && temps.Heater == latest.Heater && temps.Pool == latest.Pool
    if same && !latest.Keep {
        // Update the running entry with the new timestamp
        latest.Timestamp = temps.Timestamp
        _, err = datastore.Put(ctx, latestKey, &latest);
        if err != nil {
            return errors.New("Failed to update latest Temps: " + err.Error())
        }
        fmt.Fprintf(response, "No change: %+v", temps)
        return nil
    }

    if haveLatest {
        // There's been a change, permanently save the prior entry (without 'keep').
        _, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, "Temps", nil), &latest.Temps);
        if err != nil {
            return errors.New("Failed to insert Temps: " + err.Error())
        }
    }

    // Setup a new running entry.
    // If there's actually a temp change, mark the new running entry to prevent
    // coalescing to ensure we keep the timestamp immediately after a change.
    latest.Temps = temps
    latest.Keep = !same
    _, err = datastore.Put(ctx, latestKey, &latest);
    if err != nil {
        return errors.New("Failed to update latest Temps: " + err.Error())
    }
    
    fmt.Fprintf(response, "Added entry: %+v", temps)
    return nil
}

func getSession(ctx context.Context) (Session, error) {
    var session Session
    err := datastore.Get(ctx, sessionKey(ctx), &session)
    
    if err == nil {
        return session, nil
    }

    if err != datastore.ErrNoSuchEntity {
        return Session{}, errors.New("Failed to get session from datastore: " + err.Error())            
    }

    return login(ctx)   
}

func login(ctx context.Context) (Session, error) {
    
    session, err := signIn(ctx)
    if err != nil {
        return Session{}, err
    }
    
    session.DeviceSerial, err = getDevice(ctx, session)
    
    _, err = datastore.Put(ctx, sessionKey(ctx), &session)
    if err != nil {
        return Session{}, errors.New("Failed to update Session: " + err.Error())
    }
    
    return session, nil
}

type Secrets struct {
    Email       string  `json:"email"`
    Password    string  `json:"password"`
}

func signIn(ctx context.Context) (Session, error) {
    raw, err := ioutil.ReadFile("./secrets.json")
    
    if err != nil {
        return Session{}, errors.New("Failed to read secrets: " + err.Error())
    }
    
    var secrets Secrets
    if err = json.Unmarshal(raw, &secrets); err != nil {
        return Session{}, errors.New("Failed to parse secrets.json: " + err.Error())
    }

    var request = `{ "api_key":"` + apiKey + 
        `", "email":"` + secrets.Email +
        `", "password":"` + secrets.Password +
        `"}`
    
    client := urlfetch.Client(ctx)
    resp, err := client.Post("https://support.iaqualink.com/users/sign_in.json", 
        "application/json", 
        strings.NewReader(request));
    if err != nil {
        return Session{}, errors.New("sign_in.json failed: " + err.Error())
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return Session{}, errors.New("sign_in.json failed: " + resp.Status)
    }
    
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return Session{}, errors.New("sign_in.json read failed: " + err.Error())        
    }
    
    var session Session
    if err = json.Unmarshal(body, &session); err != nil {
        return Session{}, errors.New("sign_in.json JSON parse failure: " + err.Error())             
    }
    
    if session.AuthenticationToken == "" || session.Id == "" || session.UserId == 0 {
        return Session{}, errors.New("sign_in.json unexpected result")                      
    }

    log.Infof(ctx, "New session created: " + session.Id)
    return session, nil     
}

func getDevice(ctx context.Context, session Session) (string, error) {
    var url =  "https://support.iaqualink.com/devices.json" + 
        "?api_key=" + apiKey +
        "&authentication_token=" + session.AuthenticationToken +
        "&user_id=" + strconv.Itoa(session.UserId);
    client := urlfetch.Client(ctx)
    resp, err := client.Get(url)
    if err != nil {
        return "", errors.New("devices.json failed: " + err.Error())
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return "", errors.New("devices.json failed: " + resp.Status)
    }
        
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return "", errors.New("devices.json read failed: " + err.Error())       
    }
    
    var devices []struct {
        SerialNumber string `json:"serial_number"`
    }
    if err = json.Unmarshal(body, &devices); err != nil {
        return "", errors.New("devices.json JSON parse failure: " + err.Error())                
    }
    
    if len(devices) < 1 || devices[0].SerialNumber == "" {
        return "", errors.New("devices.json unexpected result")                     
    }
    
    return devices[0].SerialNumber, nil
}

func getTemps(ctx context.Context, session Session, attempts int) (Temps, error) {
    var url = "https://iaqualink-api.realtime.io/v1/mobile/session.json" +
            "?actionID=command&command=get_home" +
            "&serial=" + session.DeviceSerial +
            "&sessionID=" + session.Id;
    client := urlfetch.Client(ctx)
    
    resp, err := client.Get(url)
    if err != nil {
        // Can fail with deadline exceeded (default 5s)
        log.Errorf(ctx, "Fetch session.js failed: " + err.Error())
        return Temps{Air: tempErr, Pool: tempErr, Heater: tempErr}, nil
        //return Temps{}, errors.New("session.json failed: " + err.Error())
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return Temps{}, errors.New("session.json failed: " + resp.Status)
    }

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return Temps{}, errors.New("session.json read failed: " + err.Error())      
    }
    
    if len(body) == 0 {
        // Empty body seems to imply a bad session ID
        if attempts > 0 {
            log.Warningf(ctx, "session.json returned empty body, loggin in again")                      
            session, err = login(ctx)
            if err != nil {
                return Temps{}, err
            }
            attempts--
            return getTemps(ctx, session, attempts)
        } else {
            return Temps{}, errors.New("session.json empty body")
        }
    }

    var response struct {
        HomeScreen []map[string]string `json:"home_screen"`
    }
    if err = json.Unmarshal(body, &response); err != nil {
        return Temps{}, errors.New("session.json JSON parse failure: " + err.Error())               
    }
    
    // Merge all the home screen entries into a single map
    items := make(map[string]string)
    for _, element := range response.HomeScreen {
        for k, v := range element {
            items[k] = v
        }
    }
    
    status := items["status"]
    if (status != "Online") {
        if attempts > 0 {
            // Failed, try again after a short delay
            log.Warningf(ctx, "Device status " + status + ", trying again")
            time.Sleep(10*time.Second)
            attempts--
            return getTemps(ctx, session, attempts)
        } else {
            log.Errorf(ctx, "Device status " + status + ", giving up")
            return Temps{Air: tempErr, Pool: tempErr, Heater: tempErr}, nil
        }
    }
    
    // Compute heater temperature.
    // "1" means heating, "3" means on but not heating
    // "spa" (temp 1) seems to take precedence when it's on
    heater := 0
    if spa := items["spa_heater"]; spa == "1" {
        heater, err = strconv.Atoi(items["spa_set_point"])
        if err != nil {
            return Temps{}, errors.New("Failed to parse spa_set_point")
        }
    } else if pool := items["pool_heater"]; pool == "1" {
        heater, err = strconv.Atoi(items["pool_set_point"])
        if err != nil {
            return Temps{}, errors.New("Failed to parse pool_set_point")
        }
    } else if spa != "0" && spa != "3" {
        return Temps{}, errors.New("Unexpected spa_heater: " + spa)
    } else if pool != "0" && pool != "3" {
        return Temps{}, errors.New("Unexpected pool_heater: " + pool)
    }

    air := tempErr
    if at := items["air_temp"]; at != "" {
        air, err = strconv.Atoi(at)
        if err != nil {
            return Temps{}, errors.New("Failed to parse air_temp")
        }
    }

    pool := tempErr
    if pt := items["pool_temp"]; pt != "" {
        pool, err = strconv.Atoi(pt)
        if err != nil {
            return Temps{}, errors.New("Failed to parse pool_temp")
        }
    }
    
    return Temps{
        Air: air,
        Pool: pool,
        Heater: heater,
    }, nil
}

func round(x float64) int {
    return int(x+0.5)
}

func ftoc(degf int) int {
    return round(float64(degf-32)*5/9)
}

func displayHandler(response http.ResponseWriter, request *http.Request) {
    ctx := appengine.NewContext(request)
    
    // Get the running entry
    latestKey := datastore.NewKey(ctx, "Temps", "latest", 0, nil)
    var latest LatestTemps
    err := datastore.Get(ctx, latestKey, &latest)
    if err != nil {
        log.Criticalf(ctx, err.Error())
        http.Error(response, err.Error(), http.StatusInternalServerError)
        return
    }
    if latest.Air == tempErr && latest.Pool == tempErr {
        response.Write([]byte("OFFLINE"))
        return
    }
    air := ftoc(latest.Air)
    pool := ftoc(latest.Pool)
    h := ""
    if latest.Heater > 0 {
        h = "."
    }
    fmt.Fprintf(response, "%2d%2d%s", air, pool, h)
}

func main() {
    http.HandleFunc("/log.csv", logHandler)
    http.HandleFunc("/update", updateHandler)
    http.HandleFunc("/display", displayHandler)
    appengine.Main() // Starts the server to receive requests
}
