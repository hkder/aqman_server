package main

import(
	"database/sql"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"net/http"
	"encoding/json"
	"log"
	"time"
	"strings"
	"io/ioutil"
	"fmt"
	"os"
    "gopkg.in/natefinch/lumberjack.v2"
)

var db *sql.DB

type Device struct {
	AqmanSerial string `json:"aqman_serial"`
	DsmSerial string `json:"dsm_serial"`
	FwVersion string `json:"fw_version"`
}

type DeviceDetailed struct {
	Device Device
	InstallTime time.Time `json:"install_time"`
}

type Devices struct{
	DeviceList []string `json:"devices"`
}

type ErrorDict struct {
	Code string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

type CustomError struct {
	ErrorDict ErrorDict `json:"error"`
}

type DeviceInfo struct {
	Aqm101_sn string `json:"sn"`
	Dsm101_sn string `json:"dsm101_sn"`
	UpdateTime time.Time `json:"dt"`
	Temperature float64 `json:"temp"`
	Humidity float64 `json:"humi"`
	Co2 int `json:"co2"`
	Pm1 int `json:"pm1"`
	Pm2d5 int `json:"pm2d5"`
	Pm10 int `json:"pm10"`
	Radon int `json:"radon"`
    Tvoc int `json:"tvoc"`
}

type NetworkInfo struct {
	IP string `json:"ip"`
	Netmask string `json:"netmask"`
	Gateway string `json:"gateway"`
	Nameserver string `json:"nameserver"`
	Port string `json:"port"`
	AqmSerial string `json:"sn"`
	UpdateTime time.Time `json:"dt"`
}

func main(){
    // Configure Logging
    LOG_FILE_LOCATION := os.Getenv("LOG_FILE_LOCATION")
    if LOG_FILE_LOCATION != "" {
        log.SetOutput(&lumberjack.Logger{
            Filename: LOG_FILE_LOCATION,
            MaxSize: 500,
            MaxBackups: 3,
            MaxAge: 28,
            Compress: true,
        })
    }

	// Create directory
	var dbpath string
	dbpath = "/usr/src/aqmandb"

	if _, err := os.Stat(dbpath); os.IsNotExist(err){
		err = os.Mkdir(dbpath, 0755)
		if err != nil{
			log.Println(err)
		}
	}

	// Initialize Database
	var err error
	db, err = sql.Open("sqlite3", "/usr/src/aqmandb/aqman.db")
	if err != nil{
		log.Fatalln(err)
	}

	// Create Table aqman
	sql_table := `CREATE TABLE IF NOT EXISTS aqman (id INTEGER PRIMARY KEY, Serial TEXT, Ip TEXT, Port TEXT, InsertedDateTime DATETIME);`
	statement,_ := db.Prepare(sql_table)
	statement.Exec()

	// Create Router
	r := mux.NewRouter()
	r.HandleFunc("/api/devices", getDeviceList).Methods("GET")
	r.HandleFunc("/api/device/{sn}", getDeviceState).Methods("GET")
	r.HandleFunc("/api/device/{sn}", postDeviceState).Methods("POST")

	// Start Web Server
    PORT := os.Getenv("SERVER_PORT")
    startLog := fmt.Sprintf("Starting Server at port %v", PORT)
	log.Println(startLog)

    serverAddr := fmt.Sprintf(":%v", PORT)
	http.ListenAndServe(serverAddr, r)
}

func getDeviceList(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT Serial FROM aqman")
	if err != nil{
		log.Println(err)
	}

	var devices []string
	var customDevices Devices

	var serial string
	for rows.Next(){
		rows.Scan(&serial)
		devices = append(devices, serial)
	}

	customDevices.DeviceList = devices

	json.NewEncoder(w).Encode(customDevices)
}

func getDeviceState(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	ss := strings.Split(r.URL.Path, "/")
	s := ss[len(ss)-1]

	// Get IP Addr from DB
	rows, err := db.Query("SELECT Serial, Ip, Port FROM aqman WHERE Serial=$1", s)
	if err != nil{
		log.Println(err)
	}
	var serial string
	var ip string
	var port string

	for rows.Next(){
		rows.Scan(&serial, &ip, &port)
	}

	addr := fmt.Sprintf("http://%s:%s", ip, port)

	if (ip == "" || port == ""){
		w.WriteHeader(400)
		var customError CustomError
		customError = CustomError{
			ErrorDict: ErrorDict{
				Code: "NoDeviceFound",
				Message: "The request is malformed.",
				Details: "The requested Aqman is Not Yet Installed. Not Present in DB",
			},
		}
		json.NewEncoder(w).Encode(customError)
		return
	}

	resp, err := http.Get(addr)
	if err != nil{
		// log.Println(err)
		// w.WriteHeader(400)
		// var customError CustomError
		// customError = CustomError{
		// 	ErrorDict: ErrorDict{
		// 		Code: "NoResponseFromDevice",
		// 		Message: "Possible Aqman Edge Problem",
		// 		Details: "The Device is Registered in DB but Aqman Edge does not respond. Possible Wrong IP Set for AqmanEdge. Please Reset AqmanEdge and Check AqmanEdge IP",
		// 	},
		// }
		// json.NewEncoder(w).Encode(customError)

		var deviceinfo DeviceInfo
		deviceinfo.Aqm101_sn = s
		deviceinfo.Dsm101_sn = s
		deviceinfo.Temperature = -1
		deviceinfo.Humidity = -1
		deviceinfo.Co2 = -1
		deviceinfo.Pm1 = -1
		deviceinfo.Pm2d5 = -1
		deviceinfo.Pm10 = -1
		deviceinfo.Radon = -1
		deviceinfo.Tvoc = -1
		deviceinfo.UpdateTime = time.Now()
		json.NewEncoder(w).Encode(deviceinfo)

		return
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil{
		log.Println(err)
	}

	var deviceinfo DeviceInfo

	json.Unmarshal(body, &deviceinfo)
	deviceinfo.UpdateTime = time.Now()

	json.NewEncoder(w).Encode(deviceinfo)
}

func postDeviceState(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	ss := strings.Split(r.URL.Path, "/")
	s := ss[len(ss)-1]

	var networkinfo NetworkInfo
	_ = json.NewDecoder(r.Body).Decode(&networkinfo)
	networkinfo.UpdateTime = time.Now()
	log.Println(networkinfo)

	if s != networkinfo.AqmSerial{
		log.Fatal("Device Response does not match Post Request")
	} else{
		if DeviceExists(db, networkinfo.AqmSerial){
			statement,_ := db.Prepare(`UPDATE aqman SET Ip=?, Port=?, InsertedDateTime=? WHERE Serial=?`)
			statement.Exec(networkinfo.IP, networkinfo.Port, networkinfo.UpdateTime, networkinfo.AqmSerial)
			statement.Close()
		} else{
			statement,_ := db.Prepare(`INSERT INTO aqman (Serial,Ip,Port,InsertedDateTime) VALUES (?,?,?,?)`)
			statement.Exec(networkinfo.AqmSerial, networkinfo.IP, networkinfo.Port, networkinfo.UpdateTime)
			statement.Close()
		}
	}
}

func DeviceExists(db * sql.DB, deviceserial string) bool {
    sqlStmt := `SELECT Serial FROM aqman WHERE Serial = ?;`
	err := db.QueryRow(sqlStmt, deviceserial).Scan(&deviceserial)
    if err != nil {
		if err != sql.ErrNoRows {
            // a real error happened! you should change your function return
            // to "(bool, error)" and return "false, err" here
            log.Print(err)
        }
        return false
	}
    return true
}
