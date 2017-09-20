package main

import (
	"net/http"
	"os"
	"fmt"
	"github.com/gorilla/mux"
	"encoding/json"
	"bytes"
	"io/ioutil"
	"log"
	"crypto/tls"
	"time"
	"strconv"
	"github.com/satori/go.uuid"
)

func main() {
	var server Server

	server.Start()
}

type Server struct {
	sb *ServiceBroker
}

type bindRequest struct {
	AppGuid string `json:"app_guid"`
}

type permissions struct {
	Actor string `json:"actor"`
	Operations []string `json:"operations"`
}

func (s *Server) Start() {
	router := mux.NewRouter()

	s.sb = &ServiceBroker {
		NameMap: make(map[string]string),
	}

	router.HandleFunc("/v2/catalog", s.sb.Catalog).Methods("GET")
	router.HandleFunc("/v2/service_instances/{service_instance_guid}", s.sb.CreateServiceInstance).Methods("PUT")
	router.HandleFunc("/v2/service_instances/{service_instance_guid}", s.sb.RemoveServiceInstance).Methods("DELETE")
	router.HandleFunc("/v2/service_instances/{service_instance_guid}/service_bindings/{service_binding_guid}", s.sb.Bind).Methods("PUT")
	router.HandleFunc("/v2/service_instances/{service_instance_guid}/service_bindings/{service_binding_guid}", s.sb.UnBind).Methods("DELETE")

	http.Handle("/", router)

	cfPort := os.Getenv("PORT")

	fmt.Println("Server started, listening on port " + cfPort + "...")
	fmt.Println("CTL-C to break out of broker")
	fmt.Println(http.ListenAndServe(":"+cfPort, nil))
}

type ServiceBroker struct {
	NameMap map[string] string
}

func WriteResponse(w http.ResponseWriter, code int, response string) {
	w.WriteHeader(code)
	fmt.Fprintf(w, string(response))
}

func (s *ServiceBroker) Catalog(w http.ResponseWriter, r *http.Request) {
	serviceUUID := uuid.NewV4().String()
	planUUID := uuid.NewV4().String()

	catalog := `{
	"services": [{
		"name": "credhub-read",
		"id": "` + serviceUUID + `",
		"description": "credhub read service for tests",
		"bindable": true,
		"plans": [{
			"name": "credhub-read-plan",
			"id": "` + planUUID + `",
			"description": "credhub read service for tests"
		}]
	}]
}`

	WriteResponse(w, http.StatusOK, catalog)
}

func (s *ServiceBroker) CreateServiceInstance(w http.ResponseWriter, r *http.Request) {
	WriteResponse(w, http.StatusCreated, "{}")
}

func (s *ServiceBroker) RemoveServiceInstance(w http.ResponseWriter, r *http.Request) {
	WriteResponse(w, http.StatusOK, "{}")
}

func (s *ServiceBroker) Bind(w http.ResponseWriter, r *http.Request) {
	body := bindRequest{}
	err := json.NewDecoder(r.Body).Decode(&body)

	if err != nil {
		fmt.Println(err)
	}

	storedJson := map[string]string {
		"user-name": "pinkyPie",
		"password": "rainbowDash",
	}

	permissionJson := permissions{
		Actor: "mtls-app:" + body.AppGuid,
		Operations: []string{"read", "delete"},
	}

	credentialName := strconv.FormatInt(time.Now().UnixNano(), 10)
	pathVariables := mux.Vars(r)

	s.NameMap[pathVariables["service_binding_guid"]] = credentialName
	putData := map[string]interface{}{
		"name":                   credentialName,
		"type":                   "json",
		"value":                  storedJson,
		"additional_permissions": []permissions{permissionJson},
	}

	result, err := makeMtlsRequest(
		os.Getenv("CREDHUB_API")+"/api/v1/data",
		putData,
		"PUT")

	handleError(err)

	responseData := make(map[string]string)

	json.Unmarshal([]byte(result), &responseData)

	credentials := `{
  "credentials": {
    "credhub-ref": "`+ responseData["name"] + `"
  }
}`

	WriteResponse(w, http.StatusCreated, credentials)
}

func (s *ServiceBroker) UnBind(w http.ResponseWriter, r *http.Request) {
	pathVariables := mux.Vars(r)

	credentialName := s.NameMap[pathVariables["service_binding_guid"]]

	_, err := makeMtlsRequest(
		os.Getenv("CREDHUB_API")+"/api/v1/data?name="+credentialName,
		map[string]interface{}{},
		"DELETE")

	handleError(err)

	WriteResponse(w, http.StatusOK, "{}")
}

func makeMtlsRequest(url string, requestData map[string]interface{}, verb string) (string, error) {
	client, err := createMtlsClient()

	jsonValue, err := json.Marshal(requestData)
	handleError(err)

	request, err := http.NewRequest(verb, url,bytes.NewBuffer(jsonValue))
	request.Header.Set("Content-type", "application/json")

	handleError(err)

	resp, err := client.Do(request)

	handleError(err)

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func handleError(err error) {
	if err != nil {
		fmt.Println(err)
		log.Fatal("Fatal", err)
	}
}


func createMtlsClient() (*http.Client, error) {
	clientCertPath := os.Getenv("CF_INSTANCE_CERT")
	clientKeyPath := os.Getenv("CF_INSTANCE_KEY")

	_, err := os.Stat(clientCertPath)
	handleError(err)
	_, err = os.Stat(clientKeyPath)
	handleError(err)

	clientCertificate, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	handleError(err)

	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{clientCertificate},
	}

	transport := &http.Transport{TLSClientConfig: tlsConf}
	client := &http.Client{Transport: transport}

	return client, err
}
