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

var devices []DeviceDetailed
var deviceinfos []DeviceInfo

func main(){
	// Initialize Database
	var err error
	db, err = sql.Open("sqlite3", "./aqman.db")
	if err != nil{
		log.Fatalln(err)
	}

	// Create Router
	r := mux.NewRouter()
	// r.HandleFunc("/hello", getHassInfo).Methods("GET")
	r.HandleFunc("/api/devices", getDeviceList).Methods("GET")
	r.HandleFunc("/api/devices", postDeviceToList).Methods("POST")
	r.HandleFunc("/api/device/{sn}", getDeviceState).Methods("GET")
	r.HandleFunc("/api/device/{sn}", postDeviceState).Methods("POST")

	// Start Web Server
	log.Println("Starting Server at port 8297")
	http.ListenAndServe(":8297", r)
}

func getHassInfo(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode("Hello")
}

func getDeviceList(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

func postDeviceToList(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	var device Device
	var deviceDetailed DeviceDetailed

	_ = json.NewDecoder(r.Body).Decode(&device)

	if (device.AqmanSerial == "" || device.FwVersion == "") {
		w.WriteHeader(400)
		var customError CustomError
		customError = CustomError{
			ErrorDict: ErrorDict{
				Code: "ConstraintViolationError",
				Message: "The request is malformed.",
				Details: "need Aqman Serial Number and Firmware Version in request body.",
			},
		}
		json.NewEncoder(w).Encode(customError)
		return
	}	

	deviceDetailed = DeviceDetailed{
		Device: device,
		InstallTime: time.Now(),
	}
	devices = append(devices, deviceDetailed)
	json.NewEncoder(w).Encode(deviceDetailed)
}

func getDeviceState(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	ss := strings.Split(r.URL.Path, "/")
	s := ss[len(ss)-1]
	
	// var deviceinfo DeviceInfo

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
				Details: "The requested Aqman is Not Yet Installed.",
			},
		}
		json.NewEncoder(w).Encode(customError)
		return
	}

	resp, err := http.Get(addr)
	if err != nil{
		log.Println(err)
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
		sql_table := `CREATE TABLE IF NOT EXISTS aqman (id INTEGER PRIMARY KEY, Serial TEXT, Ip TEXT, Port TEXT, InsertedDateTime DATETIME);`
		statement,_ := db.Prepare(sql_table)
		statement.Exec()

		if DeviceExists(db, networkinfo.AqmSerial){
			statement,_ = db.Prepare(`UPDATE aqman SET Ip=?, Port=?, InsertedDateTime=? WHERE Serial=?`)
			statement.Exec(networkinfo.IP, networkinfo.Port, networkinfo.UpdateTime, networkinfo.AqmSerial)
			statement.Close()
		} else{
			statement,_ = db.Prepare(`INSERT INTO aqman (Serial,Ip,Port,InsertedDateTime) VALUES (?,?,?,?)`)
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