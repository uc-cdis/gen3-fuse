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
	WTSIdp             string
	WTSAccessTokenPath string `yaml:"WTSAccessTokenPath"`

	// Fence configuration
	FencePresignedURLPath string `yaml:"FencePresignedURLPath"`
	FenceAccessTokenPath  string `yaml:"FenceAccessTokenPath"`

	// Indexd configuration
	IndexdBulkFileInfoPath string `yaml:"IndexdBulkFileInfoPath"`

	Hostname string

	// An optional parameter the user can provide to retrieve access tokens from Fence
	ApiKey string

	// An optional parameter the user can provide to talk to WTS from outside the k8s cluster
	AccessToken string
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

func getJson(url string, target interface{}, access_token string) (err error) {
	req, _ := http.NewRequest("GET", url, nil)
	// add authorization header to the req
	if access_token != "" {
		req.Header.Add("Authorization", "Bearer "+access_token)
	}
	r, err := myClient.Do(req)
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
	// api key takes precedence
	if gen3FuseConfig.ApiKey != "" {
		return GetAccessTokenWithApiKey(gen3FuseConfig)
	}

	// only consult WTS if no api key provided
	return GetAccessTokenFromWTS(gen3FuseConfig, "")
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
		FuseLog(fmt.Sprintf("Error obtaining access token from Fence at %v", requestUrl))
		bodyBytes, _ := ioutil.ReadAll(r.Body)
		bodyString := string(bodyBytes)
		FuseLog(bodyString)
		return "", errors.New("Error obtaining access token from Fence at " + requestUrl + ". See log for details.")
	}

	fenceTokenResponse := new(fenceAccessTokenResponse)
	json.NewDecoder(r.Body).Decode(fenceTokenResponse)

	return fenceTokenResponse.Token, nil
}

func GetAccessTokenFromWTSForExternalHost(gen3FuseConfig *Gen3FuseConfig, IDP string) (accessToken string, err error) {
	return GetAccessTokenFromWTS(gen3FuseConfig, IDP)
}

func GetAccessTokenFromWTS(gen3FuseConfig *Gen3FuseConfig, idpInput string) (accessToken string, err error) {
	requestUrl := fmt.Sprint(gen3FuseConfig.WTSBaseURL + gen3FuseConfig.WTSAccessTokenPath)
	WTSIdp := gen3FuseConfig.WTSIdp
	if len(idpInput) > 0 {
		WTSIdp = idpInput
	}
	if len(WTSIdp) > 0 {
		requestUrl += "?idp=" + WTSIdp
	}
	tokenResponse := new(tokenResponse)
	access_token := gen3FuseConfig.AccessToken
	err = getJson(requestUrl, tokenResponse, access_token)

	if len(tokenResponse.Token) == 0 || err != nil {
		FuseLog("Error obtaining access token from the workspace token service at " + requestUrl)
		FuseLog(fmt.Sprintf("WTS returned %s, error %v\n", tokenResponse, err.Error()))
		return "", errors.New("Error obtaining access token from the workspace token service at " + requestUrl + ". " + err.Error())
	}
	return tokenResponse.Token, nil
}
