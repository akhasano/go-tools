package main

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tidwall/gjson"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	vault_addr      string // VAULT_ADDR=https://...
	token_name      string // accessor of
	token_namespace string // namespace где токен
	token_role_name string // srv-external-secret-operator-policy
	clientset       *kubernetes.Clientset
	vault_token     string
	ttl_days        int // ттл в днях когда пересоздать токен
)

const debug = false

func getVariable(curVar string, mandatory bool) string {
	tmpVar := os.Getenv(curVar)
	if len(tmpVar) == 0 && mandatory {
		log.Fatal("Переменная " + curVar + " не задана! Параметр обязательный.")
	}
	return tmpVar
}

func setVariables() {
	vault_addr = getVariable("VAULT_ADDR", true)
	token_name = getVariable("TOKEN_NAME", true)
	token_namespace = getVariable("TOKEN_NAMESPACE", true)
	token_role_name = getVariable("TOKEN_ROLE_NAME", true)
	ttl_days, _ = strconv.Atoi(getVariable("TTL_DAYS", true))

}

func buildConfigUseContext(context, kubeconfigPath string) (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func GetKubernetesClient() *kubernetes.Clientset {
	var config *rest.Config
	var err error
	kubeConfig := os.Getenv("KUBECONFIG")
	if debug {
		kubeConfig = "/home/aleksey/.kube/config"
	}
	if kubeConfig != "" {
		if debug {
			config, err = buildConfigUseContext("yc-devtest", kubeConfig)
		} else {
			config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		}
	} else {
		debugOutput("Похоже нас уже не тестируют. И запускают из пода кластера. Подключаемся через сервисную учётку;")
		config, err = rest.InClusterConfig()
	}
	checkError(err)

	ClientSet, err := kubernetes.NewForConfig(config)
	checkError(err)
	return ClientSet
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func getKubernetesSecret(token_name string) *apiv1.Secret {
	debugOutput("Функция для получения секрета, ищем секрет в namespace: " + token_namespace + " с именем: " + token_name + ";")
	secrets, err := clientset.CoreV1().Secrets(token_namespace).List(context.TODO(), metav1.ListOptions{})
	checkError(err)
	for _, secret := range secrets.Items {
		debugOutput("Смотрим секрет: " + secret.ObjectMeta.Name + ";")
		if secret.ObjectMeta.Name == token_name {
			debugOutput("Секрет найден. Выход из функции;")
			return &secret
		}
	}
	debugOutput("Секрет не найден. Выход из функции;")
	return nil
}

func updateKubernetesSecret(secret *apiv1.Secret, secretValue string) {
	secret.Data["token"] = []byte(secretValue)
	_, err := clientset.CoreV1().Secrets(token_namespace).Update(context.TODO(), &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      token_name,
			Namespace: token_namespace,
		},
		Type: secret.Type,
		Data: secret.Data,
	}, metav1.UpdateOptions{})
	checkError(err)
}

func getVaultTokenTTL() int {
	c := http.Client{Timeout: time.Duration(3) * time.Second}
	req, err := http.NewRequest("GET", vault_addr+"/v1/auth/token/lookup-self", nil)
	checkError(err)
	req.Header.Add("X-Vault-Token", vault_token)
	resp, err := c.Do(req)
	checkError(err)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	checkError(err)
	ttl := (gjson.Get(string(body), `data.ttl`)).Int() / 3600 / 24
	return int(ttl)
}

func createVaultToken() string {
	c := http.Client{Timeout: time.Duration(3) * time.Second}
	req, err := http.NewRequest("POST", vault_addr+"/v1/auth/token/create", nil)
	checkError(err)
	req.Header.Add("X-Vault-Token", vault_token)
	req.Header.Add("role_name", token_role_name)
	resp, err := c.Do(req)
	checkError(err)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	checkError(err)
	newToken := (gjson.Get(string(body), `auth.client_token`)).String()
	return newToken
}

func debugOutput(message string) {
	log.Printf("%s\n", message)
}

func main() {
	if debug {
		vault_addr = "https://vault....."
		token_name = "vault-token-test"
		token_namespace = "external-secrets"
		token_role_name = "srv-external-secret-operator-policy"
		ttl_days = 60
		debugOutput("Запуск локально. Переопределили переменные окружения;")
	} else {
		setVariables()
		debugOutput("Получили переменные окружения;")
	}

	clientset = GetKubernetesClient()
	debugOutput("Подключились к кластеру")
	secret := getKubernetesSecret(token_name)
	if secret != nil {
		debugOutput("Секрет получен с кластера, проверяем время жизни токена;")
		vault_token = string(secret.Data["token"])
		ttl := getVaultTokenTTL()
		if ttl < ttl_days {
			debugOutput("Токен заэкспайрится через " + strconv.Itoa(ttl) + " дн. Создаем новый токен;")
			newToken := createVaultToken()
			debugOutput("Создан новый токен " + newToken[:10] + " ...[masked]... " + newToken[len(newToken)-10:] + ";")
			updateKubernetesSecret(secret, newToken)
			debugOutput("Секрет в кластере обновлён;")
		} else {
			debugOutput("Токен заэкспайрится через " + strconv.Itoa(ttl) + " дн. Действия не требуются. Выходим.")
		}
	} else {
		debugOutput("Указанный секрет не найден в кластере, прекращаем работу.")
	}
}
