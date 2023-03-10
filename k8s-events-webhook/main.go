package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

type Event struct {
	Eventmeta struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Reason    string `json:"reason"`
	} `json:"eventmeta"`
	Text string    `json:"text"`
	Time time.Time `json:"time"`
}

type DBConn struct {
	DB_HOST, DB_NAME, DB_PORT string
	DB_USER, DB_PASS, CACERT  string
	DB_TABLE                  string
	BATCH                     int
	certpool                  *x509.CertPool
	conn                      *http.Client
	Events                    []Event
}

func (dbconn *DBConn) Connect() {
	log.Println("Подключаюсь к ClickHouse серверу")
	log.Println("Создаю структуру с корневым сертификатом яндекса")
	dbconn.certpool = x509.NewCertPool()
	dbconn.certpool.AppendCertsFromPEM([]byte(dbconn.CACERT))
	log.Println("Инициализирую http клиента")
	dbconn.conn = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: dbconn.certpool,
			},
		},
	}
}

func (dbconn *DBConn) SetVariables() {
	log.Println("Считываю переменные окружения")
	dbconn.DB_HOST = getVariable("DB_HOST", true)
	dbconn.DB_PORT = getVariable("DB_PORT", true)
	dbconn.DB_NAME = getVariable("DB_NAME", true)
	dbconn.DB_TABLE = getVariable("DB_TABLE", true)
	dbconn.DB_USER = getVariable("DB_USER", true)
	dbconn.DB_PASS = getVariable("DB_PASS", true)
	dbconn.CACERT = getVariable("CACERT", true)
	dbconn.BATCH, _ = strconv.Atoi(getVariable("BATCH", true))
}

func (dbconn *DBConn) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	event := Event{}
	err := json.Unmarshal(b, &event)
	checkError(err)
	// log.Printf("В слайсе %v элемент(а/ов)\n", len(dbconn.Events)+1)
	dbconn.Events = append(dbconn.Events, event)
	if len(dbconn.Events) >= dbconn.BATCH {
		log.Println("Отправляем накопленные данные в количестве " + strconv.Itoa(dbconn.BATCH) + " событий в БД и очищаем структуру")
		dbconn.SendDataToClickHouseDB()
		dbconn.Events = nil
	}
}

func (dbconn *DBConn) about_handler(w http.ResponseWriter, r *http.Request) {
	log.Println("Запросили корневой URL. Отображаем статистику ")
	fmt.Fprintf(w, `Описание:

Поднят endpoint /webhook - который ожидает вывода с kubewatch
Переменные окружения:

DB_HOST=`+dbconn.DB_HOST+`
DB_PORT=`+dbconn.DB_PORT+`
DB_NAME=`+dbconn.DB_NAME+`
DB_TABLE=`+dbconn.DB_TABLE+`
DB_USER=`+dbconn.DB_USER+`
DB_PASS=[masked]
CACERT=YandexRootCA
BATCH=`+strconv.Itoa(dbconn.BATCH)+`

`)

}

func (dbconn *DBConn) PrepareEventsAsString() string {
	var values string
	for _, m := range dbconn.Events {
		values = values + "('" + m.Eventmeta.Kind + "','" + m.Eventmeta.Name + "','" + m.Eventmeta.Namespace + "','" + m.Eventmeta.Reason + "','" + m.Text + "','" + strconv.FormatInt(m.Time.UnixNano(), 10) + "'),"
	}
	return values
}

func (dbconn *DBConn) SendHTTPRequest(m string, q string) string {
	req, _ := http.NewRequest(m, fmt.Sprintf("https://%s:%s/", dbconn.DB_HOST, dbconn.DB_PORT), nil)
	query := req.URL.Query()
	query.Add("database", dbconn.DB_NAME)
	query.Add("query", q)
	req.URL.RawQuery = query.Encode()
	req.Header.Add("X-ClickHouse-User", dbconn.DB_USER)
	req.Header.Add("X-ClickHouse-Key", dbconn.DB_PASS)
	resp, err := dbconn.conn.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	checkError(err)
	return string(data)
}

func (dbconn *DBConn) IsTableExist() bool {
	log.Println("Проверяю существует ли таблица")
	data := dbconn.SendHTTPRequest("GET", "Exists table "+dbconn.DB_NAME+"."+dbconn.DB_TABLE)
	ret, err := strconv.Atoi(strings.Trim(string(data), "\n"))
	checkError(err)
	if ret == 0 {
		log.Println("Таблица не существует.")
		return false
	} else {
		log.Println("Таблица существует")
		return true
	}
}

func (dbconn *DBConn) CreateTable() {
	log.Println("Пробую создать")
	_ = dbconn.SendHTTPRequest("POST", "CREATE TABLE "+dbconn.DB_NAME+"."+dbconn.DB_TABLE+" (`kind` String, `name` String, `namespace` String, `reason` String, `text` String, `time` Int64) ENGINE = Log;")
	log.Println("Таблица создана")
}

func (dbconn *DBConn) SendDataToClickHouseDB() {
	_ = dbconn.SendHTTPRequest("POST", "INSERT INTO events (kind,name,namespace,reason,text,time) values "+dbconn.PrepareEventsAsString())
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func getVariable(curVar string, mandatory bool) string {
	log.Println("Считываю переменную " + curVar)
	tmpVar := os.Getenv(curVar)
	if len(tmpVar) == 0 && mandatory {
		log.Fatal("Переменная " + curVar + " не задана! Параметр обязательный.")
	}
	return tmpVar
}

func main() {
	log.Println("Инициализирую структуру")
	dbconn := DBConn{}
	dbconn.SetVariables()
	dbconn.Connect()

	if !dbconn.IsTableExist() {
		dbconn.CreateTable()
	}
	httpServer := &http.Server{
		Addr:           ":8000",
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	http.HandleFunc("/webhook", dbconn.handler)
	http.HandleFunc("/", dbconn.about_handler)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log.Println("Получен сигнал на прерывание отправляем накопленные данные")
	dbconn.SendDataToClickHouseDB()
	log.Println("Завершаем работу")
	if err := httpServer.Shutdown(ctx); err != nil {

		log.Fatal(err)
	}
}
