package internal_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testConfig = &Gen3FuseConfig{
	WTSBaseURL:             "http://localhost/wts",
	WTSAccessTokenPath:     "/token",
	FencePresignedURLPath:  "/user/data/download/%s",
	FenceAccessTokenPath:   "/user/credentials/api/access_token",
	IndexdBulkFileInfoPath: "/index/bulk/documents",
	Hostname:               "localhost",
}

// roundTripFunc .
type roundTripFunc func(req *http.Request) *http.Response

// RoundTrip .
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

//NewTestClient returns *http.Client with Transport replaced to avoid making real calls
func NewTestClient(fn roundTripFunc) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(fn),
	}
}

// equals fails the test if exp is not equal to act.
func equals(tb testing.TB, exp, act interface{}) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d:\n\n\texp: %#v\n\n\tgot: %#v\033[39m\n\n", filepath.Base(file), line, exp, act)
		tb.FailNow()
	}
}

func TestGetAccessToken(t *testing.T) {
	fn := func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBufferString(`{"token": "OK"}`)),
		}
	}
	myClient.Transport = roundTripFunc(fn)
	token, err := gen3fuse.GetAccessTokenFromWTS(testConfig)
	equals(t, err, nil)
	equals(t, token, "OK")

	failAccessToken := func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: 400,
			Body:       ioutil.NopCloser(bytes.NewBufferString(`{"token": "OK"}`)),
		}
	}
	myClient.Transport = roundTripFunc(failAccessToken)
	token, err = GetAccessTokenFromWTS(testConfig)
	assert.NotEqual(t, err, nil)
	assert.Equal(t, token, "")
}
