package main

import (
	"flag"
	"fmt"
	"os"

	gen3fuse "github.com/uc-cdis/gen3-fuse/api"
)

func main() {
	configFileName := flag.String("config", "", "path to config")
	manifestFilePath := flag.String("manifest", "", "path to manifest")
	mountPoint := flag.String("mount-point", "", "directory to mount")
	hostname := flag.String("hostname", "", "commons domain")
	wtsURL := flag.String("wtsURL", "", "workspace-token-service url")
	apiKey := flag.String("api-key", "", "api key")

	flag.Parse()

	if len(os.Args) < 6 {
		fmt.Fprintln(os.Stderr, `Error: incorrect number of args.
				Usage:
				gen3-fuse \
				-config=<path_to_config> \
				-manifest=<path_to_manifest> \
				-mount-point=<directory_to_mount> \
				-hostname=<commons_domain> \
				-wtsURL=<workspace_token_service_url> \
				-api-key=<api_key>`)
		os.Exit(1)
	}

	// at least one of [apiKey, wtsURL] must be provided
	// apiKey takes precedence if both apiKey and wtsURL provided
	// apiKey only used in the case of testing/using gen3fuse locally
	if *wtsURL == "" && *apiKey == "" {
		fmt.Fprint(os.Stderr, "Neither api key nor workspace-token-service url provided. Exiting gen3-fuse.\n")
		os.Exit(1)
	}

	if _, err := os.Stat(*configFileName); os.IsNotExist(err) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "The config yaml file argument provided at %s does not exist. Exiting gen3-fuse.\n", *configFileName)
			os.Exit(1)
		}
	}

	gen3fuse.Unmount(*mountPoint)

	if _, err := os.Stat(*mountPoint); os.IsNotExist(err) {
		os.Mkdir(*mountPoint, 0777)
	}

	if _, err := os.Stat(*manifestFilePath); os.IsNotExist(err) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "The manifest file path provided at %s does not exist. Exiting Gen3Fuse.\n", *manifestFilePath)
			os.Exit(1)
		}
	}

	gen3FuseConfig, err := gen3fuse.NewGen3FuseConfigFromYaml(*configFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing yaml from %s: %s\n", *configFileName, err.Error())
		os.Exit(1)
	}
	gen3FuseConfig.Hostname = *hostname
	gen3FuseConfig.WTSBaseURL = *wtsURL
	gen3FuseConfig.ApiKey = *apiKey

	gen3fuse.InitializeApp(gen3FuseConfig, *manifestFilePath, *mountPoint)
}
