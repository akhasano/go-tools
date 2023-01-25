package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	shortCommitSha string
	namespace      string
	attempts       int
	timeout        int
)

type JsonResp struct {
	Running     bool   `json:"running"`
	ErrorReason string `json:"errorReason"`
}

const url = "https://status.devtest.k8s.test.com/v1/dynamic_deployment" //API server url. Сервис проверяющий доступность пода.

func checkStatus() bool {
	resp, err := http.Get(url + "?namespace=" + namespace + "&shortCommitSha=" + shortCommitSha)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Printf(" body: %v\n", string(body))
	var jsonData JsonResp
	err = json.Unmarshal(body, &jsonData)
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Println("Debug: jsonData.Running: ", jsonData.Running)
	return jsonData.Running
}

func getEnvs() {
	if os.Getenv("ATTEMPTS") != "" {
		attempts, _ = strconv.Atoi(os.Getenv("ATTEMPTS"))
	} else {
		attempts = 60
	}
	if os.Getenv("TIMEOUT") != "" {
		timeout, _ = strconv.Atoi(os.Getenv("TIMEOUT"))
	} else {
		timeout = 2
	}
}

func main() {
	if len(os.Args) == 3 {
		getEnvs()
		namespace = os.Args[1]
		shortCommitSha = os.Args[2]
		for i := 1; i < attempts; i++ {
			if checkStatus() {
				log.Printf("API сервер вернул true. Pod запущен приблизительно за %v сек.", i*timeout)
				os.Exit(0)
			}
			log.Printf("Попытка №%v из %v с паузой %v сек. API сервер вернул ответ: false. Запрашиваем снова.", i, attempts, timeout)
			time.Sleep(time.Duration(timeout) * time.Second)
		}
		log.Printf("За %v сек. API сервер не сообщил об успешном запуске. Для деталей, обратитесь в девопс команду.", timeout*attempts)
		os.Exit(1)
	} else {
		log.Fatal("Отсутствуют 2 обязательных аргументов. Использование: pod-ci-checker namespace sha_commit")
	}
}
