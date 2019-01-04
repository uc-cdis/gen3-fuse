package main

import (
	"fmt"
	gen3fuse "gen3-fuse/api"
	"os"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintf(
			os.Stderr,
			"Error: %s incorrect number of args. \nUsage: ./main <url-to-workspace-token-service> <commons-hostname> <path-to-manifest-file> <mount-point>\n",
			"gen3fuse")
		os.Exit(1)
	}

	var gen3FuseConfig gen3fuse.Gen3FuseConfig
	gen3FuseConfig.GetGen3FuseConfigFromYaml("config.yaml")

	gen3FuseConfig.WTSBaseURL = os.Args[1]
	gen3FuseConfig.Hostname = os.Args[2]
	manifestURL := os.Args[3]
	mountPoint := os.Args[4]

	gen3fuse.InitializeApp(&gen3FuseConfig, manifestURL, mountPoint)
}
