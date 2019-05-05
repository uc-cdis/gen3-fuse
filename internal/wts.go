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
	WTSBaseURL         string
	WTSAccessTokenPath string `yaml:"WTSAccessTokenPath"`

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

var myClient = &http.Client{Timeout: 20 * time.Second}

func getJson(url string, target interface{}) (err error) {
	r, err := myClient.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if r.StatusCode != 200 {
		return errors.New(strconv.Itoa(r.StatusCode))
	}

	err = json.NewDecoder(r.Body).Decode(target)
	if err != nil {
		return err
	}

	return nil
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
		FuseLog(fmt.Sprintf("Error obtaining access token from the Fence at %v", requestUrl))
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
	requestUrl := fmt.Sprint(gen3FuseConfig.WTSBaseURL + gen3FuseConfig.WTSAccessTokenPath)
	tokenResponse := new(tokenResponse)
	err = getJson(requestUrl, tokenResponse)

	if len(tokenResponse.Token) == 0 || err != nil {
		FuseLog("Error obtaining access token from the workspace token service at " + requestUrl)
		FuseLog(fmt.Sprintf("WTS returned %s, error %v\n", tokenResponse, err.Error()))
		return "", errors.New("Error obtaining access token from the workspace token service at " + requestUrl + ". " + err.Error())
	}
	FuseLog(fmt.Sprintf("Get access token %v", tokenResponse.Token))
	return tokenResponse.Token, nil
}
