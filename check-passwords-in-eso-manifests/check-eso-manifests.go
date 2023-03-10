package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"
)

const debug = false

var lookupFolder, vaultToken, vaultAddr string

func getVariable(curVar string, mandatory bool) string {
	tmpVar := os.Getenv(curVar)
	if len(tmpVar) == 0 && mandatory {
		log.Fatal("Переменная " + curVar + " не задана! Параметр обязательный.")
	}
	return tmpVar
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func debugOutput(message string) {
	log.Printf("%s\n", message)
}

func FilePathWalkDir(root string, extension string) ([]string, error) {
	// Функция рекурсивно пробегает папку и фильтрует по расширению файла
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			if strings.Contains(path, extension) {
				files = append(files, path)
			}
		}
		return nil
	})
	return files, err
}

func IsESOManifest(fileName string) bool {
	// ищем что манифест имеет kind: ExternalSecret
	const signature = "kind: ExternalSecret"
	fl, err := ioutil.ReadFile(fileName)
	checkError(err)
	return strings.Contains(string(fl), signature)
}

func isSecretInVaultExists(path2secret string, secretName string) bool {
	req, err := http.NewRequest("GET", vaultAddr+"/v1/bd/data/"+path2secret, nil)
	checkError(err)
	req.Header.Add("X-Vault-Token", vaultToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	tmpSecret := gjson.Get(string(body), "data.data."+secretName)
	if tmpSecret.Value() == nil {
		return false
	}
	return true
}

func enumVaultSecretsForManifestExists(fileName string) {
	type Manifest struct {
		Spec struct {
			RefreshInterval string `yaml:"refreshInterval"`
			Data            []struct {
				SecretKey string `yaml:"secretKey"`
				RemoteRef struct {
					Key      string `yaml:"key"`
					Property string `yaml:"property"`
				} `yaml:"remoteRef"`
			} `yaml:"data"`
		} `yaml:"spec"`
	}

	fl, err := ioutil.ReadFile(fileName)
	checkError(err)

	dec := yaml.NewDecoder(bytes.NewReader(fl))

	for {
		var manifest Manifest
		if dec.Decode(&manifest) != nil {
			break
		}
		for _, v := range manifest.Spec.Data {
			debugOutput("---> Проверяем наличие секрета: " + v.RemoteRef.Property + " путь в Vault: " + v.RemoteRef.Key)
			if !isSecretInVaultExists(v.RemoteRef.Key, v.RemoteRef.Property) {
				debugOutput("!!! [Fail!] ---> Секрет " + v.RemoteRef.Property + " в ветке " + v.RemoteRef.Key + " не найден. !!!")
				debugOutput(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>><<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<")
				log.Fatal(">>>>         Проверьте корректность секрета! Возможно опечатка. Аварийно завершаем пайплайн.        <<<<<")
				debugOutput(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>><<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<")
			} else {
				debugOutput("---- [Ok!] ---> Секрет " + v.RemoteRef.Key + "/" + v.RemoteRef.Property + " найден в хранилище")
			}
		}
	}
}

func main() {
	if debug {
		lookupFolder = "."
		vaultToken = getVariable("VAULT_TOKEN", false)
		vaultAddr = "https://vault.bd.domain.com"
	} else {
		lookupFolder = getVariable("LOOKUP_FOLDER", true)
		vaultToken = getVariable("VAULT_TOKEN", true)
		vaultAddr = getVariable("VAULT_ADDR", true)
	}

	listOfManifests, _ := FilePathWalkDir(lookupFolder, "yaml")
	for _, manifest := range listOfManifests {
		if IsESOManifest(manifest) {
			debugOutput("Найден новый манифест c 'kind: ExternalSecret', путь к файлу: " + manifest)
			enumVaultSecretsForManifestExists(manifest)
		}
	}
}
