package main

import (
	"fmt"
	gen3fuse "gen3-fuse/api"
	"os"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintf(os.Stderr, "Error: incorrect number of args. \nUsage: gen3fuse  <path to config yaml file> <path to manifest json file> <directory to mount> <hostname> [<api key>]\n")
		os.Exit(1)
	}

	configFileName := os.Args[1]	
	manifestFilePath := os.Args[2]
	mountPoint := os.Args[3]
	hostname := os.Args[4]
	apiKey := ""
	if len(os.Args) == 6 { 
		apiKey = os.Args[5]
	}

	if _, err := os.Stat(configFileName); os.IsNotExist(err) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "The config yaml file argument provided at %s does not exist. Exiting Gen3Fuse.\n", configFileName)
			os.Exit(1)
		}
	}

	gen3fuse.Unmount(mountPoint)

	if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
	    os.Mkdir(mountPoint, 0777)
	}

	if _, err := os.Stat(manifestFilePath); os.IsNotExist(err) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "The manifest file path provided at %s does not exist. Exiting Gen3Fuse.\n", manifestFilePath)
			os.Exit(1)
		}
	}

	gen3FuseConfig, err := gen3fuse.NewGen3FuseConfigFromYaml(configFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing yaml from %s: %s\n", configFileName, err.Error())
		os.Exit(1)
	}
	gen3FuseConfig.Hostname = hostname
	gen3FuseConfig.ApiKey = apiKey

	gen3fuse.InitializeApp(gen3FuseConfig, manifestFilePath, mountPoint)
}