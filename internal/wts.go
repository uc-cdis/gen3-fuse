package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"time"
)

type Gen3FuseConfig struct {
	Hostname    string
	LogFilePath string `yaml:"LogFilePath"`

	// Workspace Token Service configuration
	WTSBaseURL          string
	WTSFenceConnectPath string `yaml:"WTSFenceConnectPath"`
	WTSAccessTokenPath  string `yaml:"WTSAccessTokenPath"`

	// Fence configuration
	FencePath             string `yaml:"FencePath"`
	FencePresignedURLPath string `yaml:"FencePresignedURLPath"`
}

func (gc *Gen3FuseConfig) GetGen3FuseConfigFromYaml(filename string) (err error) {
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil || yamlFile == nil {
		FuseLog("yamlFile.Get err: " + err.Error())
		return err
	}

	err = yaml.Unmarshal(yamlFile, gc)
	if err != nil {
		FuseLog("Unmarshal: " + err.Error())
		return err
	}

	return nil
}

type fenceConnectResponse struct {
	Success string
	Error   string
}

type tokenResponse struct {
	Token string
}

var myClient = &http.Client{Timeout: 10 * time.Second}

func getJson(url string, target interface{}) error {
	r, err := myClient.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func ConnectWithFence(gen3FuseConfig *Gen3FuseConfig) (err error) {
	requestUrl := gen3FuseConfig.WTSBaseURL + gen3FuseConfig.WTSFenceConnectPath
	fenceResponse := new(fenceConnectResponse)

	getJson(requestUrl, fenceResponse)

	if fenceResponse.Success != "connected with fence" {
		FuseLog("Error connecting with fence via the workspace token service at " + requestUrl)
		var fenceResponseStr string = fmt.Sprintf("%#v", fenceResponse)
		FuseLog("The workspace token service came back with: " + fenceResponseStr)
		return errors.New("Error connecting with fence via the workspace token service at " + requestUrl + ". Fence response: " + fenceResponseStr + 
			".\n Are you sure the Workspace Token Service is configured correctly?")
	}

	return
}

func GetAccessToken(gen3FuseConfig *Gen3FuseConfig) (access_token string) {
	token_lifetime_in_seconds := 3600
	requestUrl := fmt.Sprintf(gen3FuseConfig.WTSBaseURL+gen3FuseConfig.WTSAccessTokenPath, token_lifetime_in_seconds)

	tokenResponse := new(tokenResponse)
	getJson(requestUrl, tokenResponse)

	if len(tokenResponse.Token) == 0 {
		fmt.Println("Error obtaining access token from the workspace token service at " + requestUrl)
		fmt.Printf("WTS returned %s\n", tokenResponse)
		return
	}

	return tokenResponse.Token
}
