package gen3fuse_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"strings"

	gen3fuse "gen3-fuse/api"
)

var normalTestConfig = `FencePath: /user
FencePresignedURLPath: /data/download/%%s
WTSFenceConnectPath: /oauth2/authorization_url
WTSAccessTokenPath: /token?expires=%%d
LogFilePath: ./fuse_log.txt
`

func WriteStringToFile(filename string, filebody string) {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0600)
	file.Truncate(0)
	if err != nil {
		return
	}
	defer file.Close()

	fmt.Fprintf(file, filebody)
}

func CreateDirIfNotExist(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			panic(err)
		}
	}
}

func SetUpTestData(t *testing.T) (gen3FuseConfig gen3fuse.Gen3FuseConfig) {
	WriteStringToFile("normal-config.yaml", normalTestConfig)
	err := gen3FuseConfig.GetGen3FuseConfigFromYaml("normal-config.yaml")
	if err != nil {
		t.Errorf("Error parsing config from yaml: " + err.Error())
	}

	gen3FuseConfig.WTSBaseURL = "http://localhost:8001"

	gen3FuseConfig.Hostname = "https://zakir.planx-pla.net"

	var testManifest = `[
		{
			"uuid": "fbd5b74e-6789-4f42-b88f-f75e72777f5d",
			"subject_id": "10"
		}
	]`
	WriteStringToFile("test-manifest.json", testManifest)

	return gen3FuseConfig
}

func TestEmptyManifest(t *testing.T) {
	var gen3FuseConfig gen3fuse.Gen3FuseConfig
	WriteStringToFile("test-presigned-url-config.yaml", normalTestConfig)
	gen3FuseConfig.GetGen3FuseConfigFromYaml("test-presigned-url-config.yaml")
	gen3FuseConfig.WTSBaseURL = "http://localhost:8001"

	gen3FuseConfig.Hostname = "https://zakir.planx-pla.net"

	manifestBody1 := ""
	WriteStringToFile("test-empty-manifest.json", manifestBody1)

	ctx := context.Background()

	_, _, err := gen3fuse.Mount(ctx, "test-mount-directory/", &gen3FuseConfig, "test-empty-manifest.json")
	defer gen3fuse.Unmount("test-mount-directory/")
	if err != nil {
		t.Errorf("Error mounting: " + err.Error())
		return
	}

	files, err := ioutil.ReadDir("./test-mount-directory/")
	if err != nil {
		t.Errorf("Error listing files in mounted directory: " + err.Error())
		return
	}

	// There should be one directory: ./test-mount-directory/exported_files/
	if len(files) != 1 {
		t.Errorf("Mounted directory is empty, it was supposed to contain one directory.")
		return
	}

	files, err = ioutil.ReadDir("./test-mount-directory/" + files[0].Name())
	if err != nil {
		t.Errorf("Error listing exported_files: " + err.Error())
		return
	}

	// There should be no files, because the manifest is empty.
	if len(files) != 0 {
		t.Errorf("Mounted directory was supposed to be empty, but it contains %d files.", len(files))
		return
	}
}

func TestGetPresignedURL(t *testing.T) {
	gen3FuseConfig := SetUpTestData(t)

	ctx := context.Background()
	g3fs, err := gen3fuse.NewGen3Fuse(ctx, &gen3FuseConfig, "test-manifest.json")

	if g3fs == nil {
		t.Errorf(err.Error())
		return
	}

	DID := "fbd5b74e-6789-4f42-b88f-f75e72777f5d"

	presignedURL, err := g3fs.GetPresignedURL(DID)

	if err != nil {
		t.Errorf("Error was not nil: " + err.Error())
		return
	}

	if len(presignedURL) < 1 {
		t.Errorf("Failed to obtain Presigned URL. ")
		return
	}
}

func TestReadFile(t *testing.T) {
	gen3FuseConfig := SetUpTestData(t)

	ctx := context.Background()

	_, _, err := gen3fuse.Mount(ctx, "test-mount-directory/", &gen3FuseConfig, "test-manifest.json")
	defer gen3fuse.Unmount("test-mount-directory/")
	if err != nil {
		t.Errorf("Error mounting: " + err.Error())
		return
	}

	files, err := ioutil.ReadDir("./test-mount-directory/")
	if err != nil {
		t.Errorf("Error listing files in mounted directory: " + err.Error())
		return
	}

	// There should be one directory: ./test-mount-directory/exported_files/
	if len(files) != 1 {
		t.Errorf("Mounted directory is empty, it was supposed to contain one directory.")
		return
	}

	dirname := files[0].Name()
	files, err = ioutil.ReadDir("./test-mount-directory/" + dirname)
	if err != nil {
		t.Errorf("Error listing exported_files: " + err.Error())
		return
	}

	// There should be 1 file, because the manifest is empty.
	if len(files) != 1 {
		t.Errorf("Mounted directory was expected to hold 1 file, but it contains %d files.", len(files))
		return
	}

	buf, err := ioutil.ReadFile("./test-mount-directory/" + dirname + "/" + files[0].Name())
	if err != nil {
		t.Errorf("Error reading " + files[0].Name() + ": " + err.Error())
		return
	}
	s := string(buf)

	expected_contents := "this file lives in s3://devplanetv1-proj1-databucket-gen3 bucket, with corresponding records \n" + 
	"in the index_record, index_record_url, index_record_url_metadata, index_record_metadata, and index_record_ace tables\n"

	if s != expected_contents {
		t.Errorf("Incorrect contents in file. Expected (%s) \n Found (%s) \n ", expected_contents, s)
		return
	}
}

func TestOpenFileNonexistent(t *testing.T) {
	gen3FuseConfig := SetUpTestData(t)

	ctx := context.Background()

	_, _, err := gen3fuse.Mount(ctx, "test-mount-directory/", &gen3FuseConfig, "test-manifest.json")
	defer gen3fuse.Unmount("test-mount-directory/")
	if err != nil {
		t.Errorf("Error mounting: " + err.Error())
		return
	}

	files, err := ioutil.ReadDir("./test-mount-directory/")
	if err != nil {
		t.Errorf("Error listing files in mounted directory: " + err.Error())
		return
	}

	// There should be one directory: ./test-mount-directory/exported_files/
	if len(files) != 1 {
		t.Errorf("Mounted directory is empty, it was supposed to contain one directory.")
		return
	}

	// Try listing a nonexistent file
	_, err = ioutil.ReadFile("./test-mount-directory/" + files[0].Name() + "/non-existent-file.txt")

	// error should contain "no such file or directory"
	if err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("Was expecting error to contain <no such file or directory>")
		return
	}
}

func TestMain(m *testing.M) {
	// any setup actions go here
	CreateDirIfNotExist("test-mount-directory")

	retCode := m.Run()

	os.Exit(retCode)

	// teardown actions go here
	os.Remove("normal-config.yaml")
	os.Remove("test-empty-manifest.json")
	os.Remove("test-manifest.json")
}