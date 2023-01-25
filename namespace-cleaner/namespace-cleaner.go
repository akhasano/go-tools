package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/tidwall/gjson"
	"helm.sh/helm/v3/pkg/action"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	rest "k8s.io/client-go/rest"
)

var (
	namespace                 string
	deploymentSubstr          string
	projectId                 string
	gitlabToken               string
	gitlabApiUrl              string
	dbIP                      string
	dbUser                    string
	dbPort                    string
	dbPassword                string
	debug                     int
	deploymentExpirationHours int
	config                    *rest.Config
	clientset                 *kubernetes.Clientset
)

func debugOutput(message string) {
	if debug == 1 {
		log.Printf("%s\n", message)
	}
}

func printEnvironments() {
	debugOutput("Переменные окружения:")
	debugOutput("ENV Namespace: " + namespace)
	debugOutput("ENV Deployment Substr: " + deploymentSubstr)
	debugOutput("ENV Project ID: " + projectId)
	debugOutput("ENV Gitlab API URL: " + gitlabApiUrl)
	if len(dbIP) > 0 {
		debugOutput("ENV dbIP: " + dbIP)
	}
	if len(dbUser) > 0 {
		debugOutput("ENV dbUser: " + dbUser)
	}
	if len(dbPort) > 0 {
		debugOutput("ENV dbPort: " + dbPort)
	}
	if len(dbPassword) > 0 {
		debugOutput("ENV dbPassword: " + dbPassword)
	}
}

func getVariable(curVar string, mandatory bool) string {
	tmpVar := os.Getenv(curVar)
	if len(tmpVar) == 0 && mandatory {
		log.Fatal("Переменная NAMESPACE не задана! Параметр обязательный.")
	}
	return tmpVar
}

func setVariables() {
	namespace = getVariable("NAMESPACE", true)
	deploymentSubstr = getVariable("DEPLOYMENT_SUBSTR", true) // Ожидается "project-front-" или "project-back-" for project_v3
	projectId = getVariable("GITLAB_PROJECT_ID", true)        // Цифровой дентификатор репозитория в гитлаб
	gitlabToken = getVariable("GITLAB_TOKEN", true)
	gitlabApiUrl = getVariable("GITLAB_API_URL", true)
	dbIP = getVariable("DB_IP", false)
	dbUser = getVariable("DB_USER", false)
	dbPort = getVariable("DB_PORT", false)
	dbPassword = getVariable("DB_PASSWORD", false)
	debug, _ = strconv.Atoi(os.Getenv("DEBUG"))
	if len(os.Getenv("DEPLOYMENT_EXPIRATION_HOURS")) > 0 {
		deploymentExpirationHours, _ = strconv.Atoi(os.Getenv("DEPLOYMENT_EXPIRATION_HOURS"))
	} else {
		deploymentExpirationHours = 48
	}

}

func ageDeployment(deploymentName string) int {
	listOptions := metav1.ListOptions{
		Limit: 100,
	}
	deploymentsClient, err := clientset.AppsV1().Deployments(namespace).List(context.TODO(), listOptions)
	checkError(err)

	for _, item := range deploymentsClient.Items {
		if item.Name == deploymentName {
			i := int(time.Now().Sub(item.CreationTimestamp.Rfc3339Copy().Time).Hours())
			return i
		}
	}

	return 10000
}

func getListOfGitlabJobs() []string {
	// Функция возвращает список активных динамических энвайрментов в гитлабе без prod и test
	c := http.Client{Timeout: time.Duration(3) * time.Second}
	req, err := http.NewRequest("GET", gitlabApiUrl+"/projects/"+projectId+"/environments?states=available", nil)
	checkError(err)

	req.Header.Add("PRIVATE-TOKEN", gitlabToken)
	resp, err := c.Do(req)
	checkError(err)

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	checkError(err)

	name := gjson.Get(string(body), `#.name`)
	sliceResult := make([]string, 0, len(name.Array()))

	debugOutput("Получаем список Giltab Environment")

	for _, nm := range name.Array() {
		if strings.Contains(nm.String(), "/") {
			splitted := strings.Split(nm.String(), "/")
			sliceResult = append(sliceResult, splitted[1])
			debugOutput("Gitlab Environment: " + splitted[1])
		}
	}

	return sliceResult
}

func getDeployments() []string {
	deployments, err := clientset.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	deploymentsSlice := make([]string, 0, len(deployments.Items))
	for _, p := range deployments.Items {
		if strings.Contains(p.ObjectMeta.Name, deploymentSubstr) {
			deploymentsSlice = append(deploymentsSlice, p.ObjectMeta.Name)
		}
	}
	return deploymentsSlice
}

func getActionConfig(namespace string) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)
	var kubeConfig *genericclioptions.ConfigFlags
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	kubeConfig = genericclioptions.NewConfigFlags(false)
	kubeConfig.APIServer = &config.Host
	kubeConfig.BearerToken = &config.BearerToken
	kubeConfig.CAFile = &config.CAFile
	kubeConfig.Namespace = &namespace
	if err := actionConfig.Init(kubeConfig, namespace, os.Getenv("HELM_DRIVER"), log.Printf); err != nil {
		return nil, err
	}
	return actionConfig, nil
}

func getHelmReleases() []string {
	actionConfig, _ := getActionConfig(namespace)
	listAction := action.NewList(actionConfig)
	releases, err := listAction.Run()
	checkError(err)

	helmReleasesSlice := make([]string, 0, 10)
	for _, release := range releases {
		if strings.Contains(release.Name, deploymentSubstr) {
			helmReleasesSlice = append(helmReleasesSlice, release.Name)
			debugOutput("Helm release: " + release.Name + " создан: " + strconv.Itoa(ageDeployment(release.Name)) + " ч. назад")
		}
	}
	return helmReleasesSlice
}

func deleteHelmRelease(releaseName string) {
	actionConfig, _ := getActionConfig(namespace)
	listAction := action.NewUninstall(actionConfig)
	_, err := listAction.Run(releaseName)
	checkError(err)
	debugOutput("Helm release " + releaseName + " удалён")
}

func getShortName(deployment string) string {
	return strings.ReplaceAll(deployment, deploymentSubstr, "")
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func getListOfDB() []string {
	debugOutput("Вызов функции получения списка бд")
	psqlconn := fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=disable", dbIP, dbPort, dbUser, dbPassword)

	db, err := sql.Open("postgres", psqlconn)
	checkError(err)

	defer db.Close()
	var (
		name string
	)

	rows, err := db.Query("SELECT datname FROM pg_database WHERE datistemplate = false;")
	checkError(err)

	dbSlice := make([]string, 0, 10)
	for rows.Next() {
		err := rows.Scan(&name)
		checkError(err)
		if !strings.Contains(name, "postgres") {
			dbSlice = append(dbSlice, name)
			debugOutput("База данных: " + name + " создана: " + strconv.Itoa(ageDeployment(deploymentSubstr+name)) + " ч. назад")
		}
	}
	return dbSlice
}

func deleteDeployment(deploymentName string) {
	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	deletePolicy := metav1.DeletePropagationForeground
	if err := deploymentsClient.Delete(context.TODO(), deploymentName, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		panic(err)
	}
	debugOutput("Деплоймент " + deploymentName + " удален")
}

func deleteDatabase(databaseName string) {
	debugOutput("Обрабатываем БД " + databaseName)
	psqlconn := fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=disable", dbIP, dbPort, dbUser, dbPassword)

	db, err := sql.Open("postgres", psqlconn)
	checkError(err)

	defer db.Close()
	sqlQuery := fmt.Sprintf("select pg_terminate_backend(pid) from pg_stat_activity where datname='%s'", databaseName)
	_, err = db.Exec(sqlQuery)
	checkError(err)
	sqlQuery = fmt.Sprintf("drop database \"%s\"", databaseName)
	_, err = db.Exec(sqlQuery)
	checkError(err)
	debugOutput("База данных " + databaseName + " удалена.")
}

func GetKubernetesClient() (*kubernetes.Clientset, *rest.Config) {
	config, err := rest.InClusterConfig()
	checkError(err)

	clientset, err := kubernetes.NewForConfig(config)
	checkError(err)
	return clientset, config
}

func main() {
	clientset, config = GetKubernetesClient()
	setVariables()
	debugOutput("Дебаг режим включён. CronJob запущена")
	printEnvironments()
	gitlabEnvironments := getListOfGitlabJobs()
	helmReleases := getHelmReleases()
	for _, p := range helmReleases {
		if !stringInSlice(getShortName(p), gitlabEnvironments) && ageDeployment(p) > deploymentExpirationHours {
			debugOutput("Deployment отсутствует в Gitlab UI: " + p + " и создан более чем " + strconv.Itoa(deploymentExpirationHours) + " ч. назад или отсутствует. Хелм релиз " + p + " будет удалён.")
			deleteHelmRelease(p)
		}
	}
	if len(dbIP) != 0 {
		listOfDBs := getListOfDB()
		for _, p := range listOfDBs {
			if !stringInSlice(p, gitlabEnvironments) && ageDeployment(deploymentSubstr+p) > deploymentExpirationHours {
				debugOutput("Deployment отсутствует в Gitlab UI: " + p + " и создан более чем " + strconv.Itoa(deploymentExpirationHours) + " ч. назад или отсутствует. База данных " + p + " будет удалена.")
				deleteDatabase(p)
			}
		}
	}
	debugOutput("CronJob завершена")
}
