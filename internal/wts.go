package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"time"
	"strconv"
)

type Gen3FuseConfig struct {
	LogFilePath string `yaml:"LogFilePath"`

	// Workspace Token Service configuration
	WTSBaseURL          string `yaml:"WTSBaseURL"`
	WTSFenceConnectPath string `yaml:"WTSFenceConnectPath"`
	WTSAccessTokenPath  string `yaml:"WTSAccessTokenPath"`

	// Fence configuration
	FenceBaseURL          string `yaml:"FenceBaseURL"`
	FencePresignedURLPath string `yaml:"FencePresignedURLPath"`

	// Indexd configuration
	IndexdBaseURL          string `yaml:"IndexdBaseURL"`
	IndexdBulkFileInfoPath string `yaml:"IndexdBulkFileInfoPath"`
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
	tokenLifetimeInSeconds := 3600
	requestUrl := fmt.Sprintf(gen3FuseConfig.WTSBaseURL + gen3FuseConfig.WTSAccessTokenPath, tokenLifetimeInSeconds)

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
