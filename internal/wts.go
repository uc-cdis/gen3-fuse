package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"
)

type Gen3FuseConfig struct {
	LogFilePath string `yaml:"LogFilePath"`

	// Workspace Token Service configuration
	WTSBaseURL          string
	WTSFenceConnectPath string `yaml:"WTSFenceConnectPath"`
	WTSAccessTokenPath  string `yaml:"WTSAccessTokenPath"`

	// Fence configuration
	FencePresignedURLPath string `yaml:"FencePresignedURLPath"`
	FenceAccessTokenPath  string `yaml:"FenceAccessTokenPath"`

	// Indexd configuration
	IndexdBulkFileInfoPath string `yaml:"IndexdBulkFileInfoPath"`

	Hostname string

	// An optional parameter the user can provide to retrieve access tokens from Fence
	ApiKey string
}

func NewGen3FuseConfigFromYaml(filename string) (gen3FuseConfig *Gen3FuseConfig, err error) {
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil || yamlFile == nil {
		FuseLog("yamlFile.Get err: " + err.Error())
		return nil, err
	}

	err = yaml.Unmarshal(yamlFile, &gen3FuseConfig)
	if err != nil {
		FuseLog("Unmarshal: " + err.Error())
		return nil, err
	}

	return gen3FuseConfig, nil
}

type fenceConnectResponse struct {
	Success string
	Error   string
}

type tokenResponse struct {
	Token string
}

type fenceAccessTokenResponse struct {
	Token string `json:"access_token"`
}

var myClient = &http.Client{Timeout: 10 * time.Second}

func getJson(url string, target interface{}) (err error, ok bool) {
	r, err := myClient.Get(url)
	if err != nil {
		return err, false
	}
	defer r.Body.Close()

	if r.StatusCode != 200 {
		return errors.New(strconv.Itoa(r.StatusCode)), false
	}

	err = json.NewDecoder(r.Body).Decode(target)
	if err != nil {
		return err, false
	}

	return nil, true
}

func ConnectWithFence(gen3FuseConfig *Gen3FuseConfig) (err error) {
	requestUrl := gen3FuseConfig.WTSBaseURL + gen3FuseConfig.WTSFenceConnectPath
	fenceResponse := new(fenceConnectResponse)

	err, ok := getJson(requestUrl, fenceResponse)
	if err != nil || !ok {
		return err
	}

	if fenceResponse.Success != "connected with fence" {
		FuseLog("Error connecting with fence via the workspace token service at " + requestUrl)
		var fenceResponseStr string = fmt.Sprintf("%#v", fenceResponse)
		FuseLog("The workspace token service came back with: " + fenceResponseStr)
		return errors.New("Error connecting with fence via the workspace token service at " + requestUrl + ". Fence response: " + fenceResponseStr +
			".\n Are you sure the Workspace Token Service is configured correctly?")
	}

	return
}

func GetAccessToken(gen3FuseConfig *Gen3FuseConfig) (accessToken string, err error) {
	if gen3FuseConfig.ApiKey != "" {
		return GetAccessTokenWithApiKey(gen3FuseConfig)
	}

	return GetAccessTokenFromWTS(gen3FuseConfig)
}

func GetAccessTokenWithApiKey(gen3FuseConfig *Gen3FuseConfig) (accessToken string, err error) {
	requestUrl := gen3FuseConfig.Hostname + gen3FuseConfig.FenceAccessTokenPath

	var jsonStr = []byte(fmt.Sprintf(`{"api_key" : "%s"}`, gen3FuseConfig.ApiKey))
	req, err := http.NewRequest("POST", requestUrl, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")
	r, err := myClient.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	if r.StatusCode != 200 {
		fmt.Println("Error obtaining access token from the Fence at " + requestUrl)
		bodyBytes, _ := ioutil.ReadAll(r.Body)
		bodyString := string(bodyBytes)
		FuseLog(bodyString)
		return "", errors.New("Error obtaining access token from Fence at " + requestUrl + ". See log for details.")
	}

	fenceTokenResponse := new(fenceAccessTokenResponse)
	json.NewDecoder(r.Body).Decode(fenceTokenResponse)

	return fenceTokenResponse.Token, nil
}

func GetAccessTokenFromWTS(gen3FuseConfig *Gen3FuseConfig) (accessToken string, err error) {
	tokenLifetimeInSeconds := 3600
	requestUrl := fmt.Sprintf(gen3FuseConfig.WTSBaseURL+gen3FuseConfig.WTSAccessTokenPath, tokenLifetimeInSeconds)
	tokenResponse := new(tokenResponse)
	err, ok := getJson(requestUrl, tokenResponse)
	if err != nil || !ok {
		return "", err
	}

	if len(tokenResponse.Token) == 0 {
		fmt.Println("Error obtaining access token from the workspace token service at " + requestUrl)
		wtsReturned := fmt.Sprintf("WTS returned %s\n", tokenResponse)
		fmt.Printf(wtsReturned)
		return "", errors.New("Error obtaining access token from the workspace token service at " + requestUrl + ". " + wtsReturned)
	}

	return tokenResponse.Token, nil
}
