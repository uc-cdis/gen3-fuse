package main

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"log"
	"net/http"
    "time"
    "os"
    "io/ioutil"
    "bytes"
    "fmt"
)

type FenceConnectResponse struct {
	Success string
}

type TokenResponse struct {
	Token string
}

func authUrlHandler(w http.ResponseWriter, r *http.Request) {
	responseBody := FenceConnectResponse{"connected with fence"}

	data, err := json.Marshal(responseBody)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(200)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

type AccessTokenResponse struct {
    AccessToken string `json:"access_token"`
}

var myClient = &http.Client{Timeout: 10 * time.Second}

func retrieveAccessTokenFromTestCommons(commonsUrl string, apiKey string) (accessToken string, err error) {
    message := map[string]interface{}{
        "api_key": apiKey,
    }
    bytesRepresentation, err := json.Marshal(message)

    r, err := myClient.Post(commonsUrl + "/user/credentials/api/access_token","application/json", bytes.NewBuffer(bytesRepresentation))
    if err != nil {
        return "", err
    }
    defer r.Body.Close()

    bodyBytes, _ := ioutil.ReadAll(r.Body)
    bodyString := string(bodyBytes)

    var target AccessTokenResponse
    json.Unmarshal([]byte(bodyString), &target)

    return target.AccessToken, nil
}

func tokenHandler(w http.ResponseWriter, r *http.Request) {
    commonsUrl := os.Args[1]
    apiKey := os.Args[2]
    
    accessToken, err := retrieveAccessTokenFromTestCommons(commonsUrl, apiKey)
    
    if err != nil {
        fmt.Println("Error retrieving access token: " + err.Error())
        os.Exit(1)
    }
	responseBody := TokenResponse{accessToken}

	data, err := json.Marshal(responseBody)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(200)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func main() {
    if len(os.Args) != 3 {
        fmt.Println("Incorrect usage. Please provide command line args for commons_url and api_key.\n")
        fmt.Println("Usage: ./mock_wts_server <commons-url> <api-key>")
        fmt.Println("Example of a commons url -- https://qa-niaid.planx-pla.net")
        fmt.Println("You can generate an api key at commons-url/identity")
        os.Exit(1)
    }
    fmt.Println("\nListening on localhost:8001...")
	router := mux.NewRouter()
	router.HandleFunc("/oauth2/authorization_url", authUrlHandler).Methods("GET")
	router.HandleFunc("/token", tokenHandler).Methods("GET")
	log.Fatal(http.ListenAndServe("localhost:8001", router))
}
