package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/tidwall/gjson"
)

func main() {
	c := http.Client{Timeout: time.Duration(3) * time.Second}

	role_id := os.Getenv("VAULT_ROLE_ID")

	if len(role_id) == 0 {
		log.Fatal("Переменная VAULT_ROLE_ID не задана!")
	}

	secret_id := os.Getenv("VAULT_SECRET_ID")

	if len(secret_id) == 0 {
		log.Fatal("Переменная VAULT_SECRET_ID не задана!")
	}

	vault_addr := os.Getenv("VAULT_ADDR")

	if len(vault_addr) == 0 {
		log.Fatal("Переменная VAULT_ADDR не задана!")
	}

	ci_job_token := os.Getenv("CI_JOB_TOKEN")
	ci_api_v4_url := os.Getenv("CI_API_V4_URL")

	req, err := http.NewRequest("GET", ci_api_v4_url+"/job", nil)

	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("JOB-TOKEN", ci_job_token)
	resp, err := c.Do(req)

	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatal("Ваш запрос к Vault отклонён. Средства будут возвращены на вашу карту ;-P")
	} else {
		resp, err := http.PostForm(vault_addr+"/v1/auth/approle/login",
			url.Values{"role_id": {role_id}, "secret_id": {secret_id}})

		if err != nil {
			log.Fatal(err)
		}

		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			log.Fatal(err)
		}
		vault_token := gjson.Get(string(body), "auth.client_token")
		fmt.Println(vault_token.String())
	}
}
