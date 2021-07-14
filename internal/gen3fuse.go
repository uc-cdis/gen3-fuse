package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"
	"errors"

	"bytes"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
)

type Gen3Fuse struct {
	fuseutil.NotImplementedFileSystem

	accessToken string

	DIDs []string

	DIDsToCommonsHostnames map[string]string

	inodes map[fuseops.InodeID]*inodeInfo

	gen3FuseConfig *Gen3FuseConfig

	ExternalIDPTokens map[string]string
}

type ManifestRecord struct {
	CommonsHostname string `json:"commons_url"`
	ObjectId        string `json:"object_id"`
	SubjectId       string `json:"subject_id"`
	Uuid            string `json:"uuid"`
}

type FileInfo struct {
	Filename         string   `json:"file_name"`
	Filesize         uint64   `json:"size"`
	DID              string   `json:"did"`
	URLs             []string `json:"urls"`
	FromExternalHost bool
}

// APIError carries a failure to get a 2XX response
type APIError struct {
	StatusCode int
	URL        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Fail to fetch %v, status code: %v", e.URL, e.StatusCode)
}

var LogFilePath string = "fuse_log.txt"

func NewGen3Fuse(ctx context.Context, gen3FuseConfig *Gen3FuseConfig, manifestFilePath string) (fs *Gen3Fuse, err error) {
	LogFilePath = gen3FuseConfig.LogFilePath

	accessToken, err := GetAccessToken(gen3FuseConfig)
	if err != nil {
		return nil, err
	}

	fs = &Gen3Fuse{
		accessToken:    accessToken,
		gen3FuseConfig: gen3FuseConfig,
	}

	err = fs.LoadDIDsFromManifest(manifestFilePath)
	if err != nil {
		return nil, err
	}

	var didToFileInfo map[string]*FileInfo

	if len(fs.DIDs) == 0 {
		FuseLog(fmt.Sprintf("Warning: no DIDs were obtained from the manifest %v.", manifestFilePath))
	} else {
		didToFileInfo, err = fs.GetFileNamesAndSizes()
		if err != nil {
			return nil, err
		}
	}

	for IDP, _ := range fs.ExternalIDPTokens {
		token, err := GetAccessTokenFromWTSForExternalHost(fs.gen3FuseConfig, IDP)
		if err != nil {
			FuseLog(fmt.Sprintf("Failed to retrieve access token from WTS for External Host IDP %v.", IDP))
			continue
		}
		fs.ExternalIDPTokens[IDP] = token
		// TODO: delete this line
		FuseLog(fmt.Sprintf("\nGot a token for %v - %v", IDP, token))
	}

	fs.inodes = InitializeInodes(didToFileInfo)
	FuseLog("Initialized inodes")
	return fs, nil
}

type inodeInfo struct {
	attributes fuseops.InodeAttributes

	// File or directory?
	dir bool

	// For directories, Children.
	Children []fuseutil.Dirent

	// File name, useful for debugging
	Name string

	// For files, the DID
	DID string

	// For files, the presigned URL
	presignedUrl string

	// Indicates whether the object info is from a source other than Indexd
	FromExternalHost bool

	// For DRS files -- the access URL(s) that yields a presigned URL for the file when given an auth token
	ExternalAccessURLs []string
}

func getFilePathFromURL(urls []string) (result []string, ok bool) {
	validURL := ""
	for _, uri := range urls {
		parsed, err := url.Parse(uri)
		if err == nil && (parsed.Scheme == "s3" || parsed.Scheme == "gcs" || parsed.Scheme == "http" || parsed.Scheme == "https") {
			validURL = uri
			break
		}
		FuseLog(fmt.Sprintf("Skipping url %v, protocol not supported", uri))
	}
	if validURL == "" {
		return nil, false
	}
	u, err := url.Parse(validURL)
	if err != nil {
		FuseLog(fmt.Sprintf("Error parsing out the filename from this URL %s: %s", validURL, err))
		return nil, false
	}

	filePaths := strings.Split(u.Path, "/")
	// return the cloud path without bucket name
	return filePaths[1:len(filePaths)], true
}

func InitializeInodes(didToFileInfo map[string]*FileInfo) map[fuseops.InodeID]*inodeInfo {
	/*
		Create a file system with a fixed structure described by the manifest
		If you're trying to read this code and understand it, maybe check out the hello world FUSE sample first:
		https://github.com/jacobsa/fuse/blob/master/samples/hellofs/hello_fs.go
	*/

	FuseLog(fmt.Sprintf("\n\ninside InitializeInodes with didToFileInfo : %#v\n\n", didToFileInfo))

	FuseLog("Inside InitializeInodes")
	const (
		rootInode fuseops.InodeID = fuseops.RootInodeID + iota
		byIDDir
		byFilenameDir
		byFilepathDir
	)

	var inodes = map[fuseops.InodeID]*inodeInfo{
		// root inode
		rootInode: &inodeInfo{
			attributes: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  0555 | os.ModeDir,
			},
			dir:  true,
			Name: "root",
			Children: []fuseutil.Dirent{
				fuseutil.Dirent{
					Offset: 1,
					Inode:  byIDDir,
					Name:   "by-guid",
					Type:   fuseutil.DT_Directory,
				},
				fuseutil.Dirent{
					Offset: 2,
					Inode:  byFilenameDir,
					Name:   "by-filename",
					Type:   fuseutil.DT_Directory,
				},
				fuseutil.Dirent{
					Offset: 3,
					Inode:  byFilepathDir,
					Name:   "by-filepath",
					Type:   fuseutil.DT_Directory,
				},
			},
		},
	}

	// Create an inode for each imaginary file
	var inodeID fuseops.InodeID = fuseops.RootInodeID + 4
	inodeIDMap := make(map[string]fuseops.InodeID)
	inodeIDMap["by-guid"] = byIDDir
	inodeIDMap["by-filename"] = byFilenameDir
	inodeIDMap["by-filepath"] = byFilepathDir
	// inode for top level dirs that contains the imaginary files described in the manifest
	topDirs := map[string]fuseops.InodeID{
		"by-id":       byIDDir,
		"by-filename": byFilenameDir,
		"by-filepath": byFilepathDir,
	}
	for name, inode := range topDirs {
		inodes[inode] = &inodeInfo{
			attributes: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  0555 | os.ModeDir,
			},
			dir:      true,
			Name:     name,
			Children: []fuseutil.Dirent{},
		}
	}

	for did, fileInfo := range didToFileInfo {
		if len(fileInfo.URLs) == 0 {
			FuseLog(fmt.Sprintf("Indexd record %s does not seem to have a file associated with it; ignoring it.", did))
			continue
		}

		// inode for by-id file
		// GUIDs can have prefix as folders
		guidPaths := append([]string{"by-guid"}, strings.Split(did, "/")...)
		inodeID = createInodeForDirs(inodes, inodeID, guidPaths, inodeIDMap, did, fileInfo.Filesize)

		inodeID++

		// Try to get the filename from the first URL
		paths, ok := getFilePathFromURL(fileInfo.URLs)
		if fileInfo.FromExternalHost {
			paths = []string{fileInfo.Filename}
			ok = true
		}

		if !ok {
			continue
		}
		filename := paths[len(paths)-1]
		if len(fileInfo.Filename) > 0 {
			filename = fileInfo.Filename
		}
		externalURLs := []string{}
		if fileInfo.FromExternalHost {
			externalURLs = fileInfo.URLs
		}
		createInode(inodes, byFilenameDir, inodeID, filename, did, fileInfo.Filesize, fileInfo.FromExternalHost, externalURLs)
		inodeID++
		paths = append([]string{"by-filepath"}, paths...)
		inodeID = createInodeForDirs(inodes, inodeID, paths, inodeIDMap, did, fileInfo.Filesize)
	}
	return inodes
}

func createInodeForDirs(inodes map[fuseops.InodeID]*inodeInfo, inodeID fuseops.InodeID, paths []string, inodeIDMap map[string]fuseops.InodeID, did string, filesize uint64) fuseops.InodeID {
	for i := 0; i <= len(paths)-1; i++ {
		filename := paths[i]
		fullpath := strings.Join(paths[0:i+1], "/")
		parentpath := strings.Join(paths[0:i], "/")
		// this folder is already created in another guid lookup
		_, ok := inodeIDMap[fullpath]
		if ok {
			continue
		}
		parentNode, ok := inodeIDMap[parentpath]
		if !ok {
			FuseLog(fmt.Sprintf("Fail to find parent folder %v for %v", parentpath, filename))
			continue
		}
		if i == len(paths)-1 {
			// leaf file
			createInode(inodes, parentNode, inodeID, filename, did, filesize, false, nil)
		} else {
			// intermediate directory
			createInode(inodes, parentNode, inodeID, filename, "", 0, false, nil)
		}
		inodeIDMap[fullpath] = inodeID
		inodeID++
	}
	return inodeID
}

func createInode(inodes map[fuseops.InodeID]*inodeInfo, parentID fuseops.InodeID, inodeID fuseops.InodeID, filename string, did string, filesize uint64, fromExternalHost bool, externalURLs []string) {
	parent, ok := inodes[parentID]
	if !ok {
		panic(fmt.Sprintf("Something went wrong, can't find parent folder for %v, guid %v", filename, did))
	}
	curIDSlice := parent.Children
	offset := fuseops.DirOffset(1)
	if len(curIDSlice) > 0 {
		offset = curIDSlice[len(curIDSlice)-1].Offset + 1
	}
	inodeType := fuseutil.DT_File
	if did == "" {
		inodeType = fuseutil.DT_Directory
	}
	var dirEntry = fuseutil.Dirent{
		Offset: fuseops.DirOffset(offset),
		Inode:  inodeID,
		Name:   filename,
		Type:   inodeType,
	}
	curIDSlice = append(curIDSlice, dirEntry)
	inodes[parentID].Children = curIDSlice
	if did == "" {
		inodes[inodeID] = &inodeInfo{
			attributes: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  0555 | os.ModeDir,
			},
			dir:      true,
			Name:     filename,
			Children: []fuseutil.Dirent{},
		}
	} else {
		inodes[inodeID] = &inodeInfo{
			attributes: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  0444,
				Size:  filesize,
			},
			Name:               filename,
			DID:                did,
			FromExternalHost:   fromExternalHost,
			ExternalAccessURLs: externalURLs,
		}
	}
}

func findChildInode(
	name string,
	Children []fuseutil.Dirent) (inode fuseops.InodeID, err error) {
	for _, e := range Children {
		if e.Name == name {
			inode = e.Inode
			return
		}
	}

	err = fuse.ENOENT
	return
}

func GetIDPForURL(URL string) (IDP string) {
	if strings.Contains(URL, "jcoin") {
		return "jcoin-google"
	}
	if strings.Contains(URL, "healdata") {
		return "externaldata-google"
	}
	return ""
}

func (fs *Gen3Fuse) LoadDIDsFromManifest(manifestFilePath string) (err error) {
	FuseLog(fmt.Sprintf("Inside LoadDIDsFromManifest, loading manifest from %v", manifestFilePath))
	b, err := ioutil.ReadFile(manifestFilePath)
	if err != nil {
		return err
	}

	s := string(b)
	sReplaceNone := strings.Replace(s, "None", "\"\"", -1)
	sReplaceNoneAsBytes := []byte(sReplaceNone)

	manifestJSON := make([]ManifestRecord, 0)
	json.Unmarshal(sReplaceNoneAsBytes, &manifestJSON)

	fs.DIDsToCommonsHostnames = make(map[string]string)
	fs.ExternalIDPTokens = make(map[string]string)
	externalHostnames := make(map[string]string)
	for i := 0; i < len(manifestJSON); i++ {
		fs.DIDs = append(fs.DIDs, manifestJSON[i].ObjectId)
		if len(manifestJSON[i].CommonsHostname) > 0 {
			fs.DIDsToCommonsHostnames[manifestJSON[i].ObjectId] = manifestJSON[i].CommonsHostname
			// Using a map as a set
			externalHostnames[manifestJSON[i].CommonsHostname] = ""
		}
	}

	for k, _ := range externalHostnames {
		IDP := GetIDPForURL(k)
		if len(IDP) > 0 {
			// Initializing the keys of the IDP->Token map
			fs.ExternalIDPTokens[IDP] = ""
		}
	}

	return
}

func (fs *Gen3Fuse) patchAttributes(attr *fuseops.InodeAttributes) {
	now := time.Now()
	attr.Atime = now
	attr.Mtime = now
	attr.Crtime = now
}
func (fs *Gen3Fuse) StatFS(
	ctx context.Context,
	op *fuseops.StatFSOp) (err error) {
	return
}

func (fs *Gen3Fuse) LookUpInode(
	ctx context.Context,
	op *fuseops.LookUpInodeOp) (err error) {
	// Find the info for the parent.
	parentInfo, ok := fs.inodes[op.Parent]
	if !ok {
		err = fuse.ENOENT
		return
	}

	// Find the child within the parent.
	childInode, err := findChildInode(op.Name, parentInfo.Children)
	if err != nil {
		return
	}

	// Copy over information.
	op.Entry.Child = childInode
	op.Entry.Attributes = fs.inodes[childInode].attributes

	// Patch attributes.
	fs.patchAttributes(&op.Entry.Attributes)

	return
}

func (fs *Gen3Fuse) GetInodeAttributes(
	ctx context.Context,
	op *fuseops.GetInodeAttributesOp) (err error) {
	// Find the info for this inode.
	info, ok := fs.inodes[op.Inode]
	if !ok {
		err = fuse.ENOENT
		return
	}

	// Copy over its attributes.
	op.Attributes = info.attributes

	// Patch attributes.
	fs.patchAttributes(&op.Attributes)
	return
}

func (fs *Gen3Fuse) OpenDir(
	ctx context.Context,
	op *fuseops.OpenDirOp) (err error) {
	// Allow opening any directory.
	return
}

func (fs *Gen3Fuse) ReadDir(
	ctx context.Context,
	op *fuseops.ReadDirOp) (err error) {
	// Find the info for this inode.
	info, ok := fs.inodes[op.Inode]
	if !ok {
		FuseLog("Error: fs.inodes[op.Inode] returned not ok")
		err = fuse.ENOENT
		return
	}

	if !info.dir {
		FuseLog("Error: info.dir is not set true. So we can't read the directory.")
		var structStr string = fmt.Sprintf("%#v", info)
		FuseLog("\n ReadDir info struct was: " + structStr + "\n")
		err = fuse.EIO
		return
	}

	entries := info.Children

	// Grab the range of interest.
	if op.Offset > fuseops.DirOffset(len(entries)) {
		FuseLog("Error: (op.Offset > fuseops.DirOffset(len(entries)) was false")
		err = fuse.EIO
		return
	}

	entries = entries[op.Offset:]

	// Resume at the specified offset into the array.
	for _, e := range entries {
		n := fuseutil.WriteDirent(op.Dst[op.BytesRead:], e)
		if n == 0 {
			break
		}

		op.BytesRead += n
	}
	return
}

func (fs *Gen3Fuse) OpenFile(
	ctx context.Context,
	op *fuseops.OpenFileOp) (err error) {

	// FuseLog("inside OpenFile")

	info, ok := fs.inodes[op.Inode]
	if !ok {
		err = fuse.ENOENT
		return
	}

	if info.presignedUrl == "" || len(info.presignedUrl) < 3 {
		presignedUrl, err := fs.GetPresignedURL(info)
		if err != nil {
			return err
		}
		info.presignedUrl = presignedUrl
	}

	fs.inodes[op.Inode] = info

	return
}

func (fs *Gen3Fuse) ReadFile(
	ctx context.Context,
	op *fuseops.ReadFileOp) (err error) {
	FuseLog("Inside ReadFile")
	info, ok := fs.inodes[op.Inode]
	if !ok {
		err = fuse.ENOENT
		return
	}
	size := int64(len(op.Dst))
	fullsize := int64(info.attributes.Size)
	FuseLog(fmt.Sprintf("get %v with offset %v size %v", info.DID, op.Offset, size))
	fileBody, err := FetchContentsAtURL(info.presignedUrl, op.Offset, size, fullsize)
	if apiErr, ok := err.(*APIError); ok {
		// aws returns 403 when URL is expired
		if apiErr.StatusCode == 403 {
			FuseLog(fmt.Sprintf("Get a fresh url for %v", info.DID))
			presignedURL, urlErr := fs.GetPresignedURL(info)
			if urlErr != nil {
				FuseLog("Error fetching file contents: " + urlErr.Error())
				err = fuse.ENOENT
				return err
			}
			info.presignedUrl = presignedURL
			fileBody, err = FetchContentsAtURL(info.presignedUrl, op.Offset, size, fullsize)
			if err != nil {
				FuseLog("Error re-fetching file contents: " + err.Error())
				err = fuse.ENOENT
				return err
			}
		}
	}
	if err != nil {
		FuseLog("Error fetching file contents: " + err.Error())
		err = fuse.ENOENT
		return err
	}

	reader := strings.NewReader(string(fileBody))

	// op.Offset: The offset within the file at which to read.
	// op.Dst: The destination buffer, whose length gives the size of the read.

	op.BytesRead, err = reader.ReadAt(op.Dst, 0)
	FuseLog("Read " + strconv.Itoa(op.BytesRead) + " bytes")
	if op.BytesRead > 0 {
		return nil
	}

	if err != nil {
		FuseLog("Error reading file: " + err.Error())
	}

	// Special case: FUSE doesn't expect us to return io.EOF.
	if err == io.EOF {
		err = nil
	}

	return
}

type presignedURLResponse struct {
	Url string
}

func (fs *Gen3Fuse) GetPresignedURLFromExternalHost(info *inodeInfo) (presignedUrl string, err error) {
	FuseLog(fmt.Sprintf("Inside GetPresignedURLFromExternalHost with %s", info.DID))
	// The below code talks to the DRS API instead of Fence to get a presigned URL
	DID := info.DID
	if len(info.ExternalAccessURLs) < 1 {
		FuseLog(fmt.Sprintf("Error: The record %v is from an external host, but lacks ExternalAccessURLs.", DID))
		return "", errors.New(fmt.Sprintf("Error: The record %v is from an external host, but lacks ExternalAccessURLs.", DID))
	}
	drsRequestURL := info.ExternalAccessURLs[0]
	FuseLog("GET " + drsRequestURL)

	req, err := http.NewRequest("GET", drsRequestURL, nil)

	accessToken := fs.accessToken
	IDP := GetIDPForURL(drsRequestURL)
	if len(IDP) < 1 {
		FuseLog(fmt.Sprintf("Failed to determine IDP for URL %v", drsRequestURL))
	} else {
		accessToken = fs.ExternalIDPTokens[IDP]
		// TODO: delete this line
		FuseLog(fmt.Sprintf("got an access token for IDP %v: %v", IDP, accessToken))
	}

	req.Header.Add("Authorization", "Bearer "+accessToken)
	req.Header.Add("Accept", "application/json")

	if err != nil {
		FuseLog(err.Error() + " (" + DID + ") ")
		return "", err
	}
	resp, err := myClient.Do(req)

	if err != nil {
		FuseLog(err.Error() + " (" + DID + ") ")
		return "", err
	}

	// TODO: delete this line
	FuseLog(fmt.Sprintf("\n\nresponse from drsRequestURL %v: %v\n", drsRequestURL, resp))
	if resp.StatusCode == 200 {
		return fs.URLFromSuccessResponse(resp), nil
	} else if resp.StatusCode == 401 {
		// refresh the access token and try again just one more time
		FuseLog("Got 401, retrying...")
		accessToken, err = GetAccessTokenFromWTSForExternalHost(fs.gen3FuseConfig, IDP)
		if err != nil {
			return "", err
		}

		FuseLog("GET " + drsRequestURL)
		req, err := http.NewRequest("GET", drsRequestURL, nil)
		req.Header.Add("Authorization", "Bearer "+accessToken)
		req.Header.Add("Accept", "application/json")

		if err != nil {
			FuseLog(err.Error() + " (" + DID + ") ")
			return "", err
		}
		resp, err := myClient.Do(req)

		if err != nil {
			FuseLog(err.Error() + " (" + DID + ") ")
			return "", err
		}

		// TODO: delete this line
		FuseLog(fmt.Sprintf("\n\nresponse from drsRequestURL: %v\n", resp))
		if resp.StatusCode == 200 {
			return fs.URLFromSuccessResponse(resp), nil
		} else {
			FuseLog(fmt.Sprintf("After refreshing the access token, external host %v still returns status code %d when asked for a presigned URL.", drsRequestURL, resp.StatusCode))
			FuseLog("The full error page is below:\n")
			bodyBytes, _ := ioutil.ReadAll(resp.Body)
			bodyString := string(bodyBytes)
			FuseLog(bodyString)
			return "", nil
		}
	}
	return "", nil
}

func (fs *Gen3Fuse) GetPresignedURLFromFence(info *inodeInfo) (presignedUrl string, err error) {
	FuseLog(fmt.Sprintf("Inside GetPresignedURLFromFence with %s", info.DID))
	DID := info.DID
	// The below code talks to the Fence microservice (case where info.FromExternalHost == false)
	resp, err := fs.FetchURLResponseFromFence(DID)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return fs.URLFromSuccessResponse(resp), nil
	} else if resp.StatusCode == 401 {
		// refresh the access token and try again just one more time
		FuseLog("Got 401, retrying...")
		fs.accessToken, err = GetAccessToken(fs.gen3FuseConfig)
		if err != nil {
			return "", err
		}
		respRetry, err := fs.FetchURLResponseFromFence(DID)
		if err != nil {
			return "", err
		}

		defer respRetry.Body.Close()

		if resp.StatusCode == 200 {
			return fs.URLFromSuccessResponse(resp), nil
		}
	}

	return "", fs.HandleFenceError(resp)
}

func (fs *Gen3Fuse) GetPresignedURL(info *inodeInfo) (presignedUrl string, err error) {
	FuseLog("Inside GetPresignedURL")
	FuseLog(fmt.Sprintf("\n> with fileInfo: %v", info))
	if info.FromExternalHost {
		rv, err := fs.GetPresignedURLFromExternalHost(info)
		FuseLog("got url from external host")
		FuseLog(rv)
		return rv, err
	} else {
		return fs.GetPresignedURLFromFence(info)
	}
}

func (fs *Gen3Fuse) HandleFenceError(resp *http.Response) (err error) {
	FuseLog("\nFetching presigned URL failed. \nThe Response code was " + strconv.Itoa(resp.StatusCode))

	if resp.StatusCode == 401 {
		FuseLog("Fence denied access based on the authentication provided. \nThis may be due to an improperly configured Workspace Token Service, or an outdated api key.")
	}
	FuseLog("The full error page is below:\n")
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	FuseLog(bodyString)
	return fuse.EIO
}

func (fs *Gen3Fuse) HandleIndexdError(resp *http.Response) (err error) {
	FuseLog("\nFetching file sizes from Indexd failed. \nThe Response code was " + strconv.Itoa(resp.StatusCode))

	if resp.StatusCode == 401 {
		FuseLog("Indexd denied access based on the authentication provided. \nThis may be due to an improperly configured Workspace Token Service, or an outdated api key.")
	}
	FuseLog("The full error page is below:\n")
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	FuseLog(bodyString)
	return fuse.EIO
}

func (fs *Gen3Fuse) FetchURLResponseFromFence(DID string) (response *http.Response, err error) {
	requestUrl := fmt.Sprintf(fs.gen3FuseConfig.Hostname+fs.gen3FuseConfig.FencePresignedURLPath, DID+"?expires_in=900")
	FuseLog("GET " + requestUrl)

	req, err := http.NewRequest("GET", requestUrl, nil)
	req.Header.Add("Authorization", "Bearer "+fs.accessToken)
	req.Header.Add("Accept", "application/json")

	if err != nil {
		FuseLog(err.Error() + " (" + DID + ") ")
		return nil, err
	}
	resp, err := myClient.Do(req)

	if err != nil {
		FuseLog(err.Error() + " (" + DID + ") ")
		return nil, err
	}

	return resp, nil
}

func (fs *Gen3Fuse) URLFromSuccessResponse(resp *http.Response) (presignedUrl string) {
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)

	var urlResponse presignedURLResponse
	json.Unmarshal([]byte(bodyString), &urlResponse)
	return urlResponse.Url
}

func (fs *Gen3Fuse) GetExternalHostFileInfos(didsWithExternalInfo []string, didToFileInfo map[string]*FileInfo) (didToFileInfoModified map[string]*FileInfo, err error) {
	// Manifest entries with a commons_url field filled out have FileInfo metadata
	// in a location other than Indexd. This function retrieves that metadata.
	// Currently supporting DRS objects with metadata in the JCOIN commons.

	for i := 0; i < len(didsWithExternalInfo); i += 1 {
		did := didsWithExternalInfo[i]
		commonsHostname := fs.DIDsToCommonsHostnames[did]
		// For now, we assume that all entries with a commons_url support the DRS API.
		if commonsHostname[len(commonsHostname)-1:] != "/" {
			commonsHostname = commonsHostname + "/"
		}
		// The data in the agg MDS needs to be standardized...
		if ! (strings.HasPrefix(commonsHostname, "http://") && strings.HasPrefix(commonsHostname, "https://")) {
			commonsHostname = "https://" + commonsHostname
		}

		drsRequestURL := commonsHostname + "ga4gh/drs/v1/objects/" + did
		fmt.Println(drsRequestURL)

		timeout := time.Duration(4 * time.Second)
		client := http.Client{
			Timeout: timeout,
		}

		req, err := http.NewRequest("GET", drsRequestURL, nil)
		req.Header.Set("Content-Type", "application/json")
		if err != nil {
			FuseLog(err.Error())
			continue
		}
		resp, err := client.Do(req)

		if err != nil {
			FuseLog(err.Error())
			continue
		}

		if resp.StatusCode != 200 {
			FuseLog(fmt.Sprintf("Error: Failed to retrieve file info from %s with status code %d", drsRequestURL, resp.StatusCode))
			continue
		}

		defer resp.Body.Close()

		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)

		// get json as a map with interface{}
		jsonMap := make(map[string](interface{}))
		err = json.Unmarshal([]byte(bodyString), &jsonMap)
		if err != nil {
			FuseLog(fmt.Sprintf("ERROR: Failed to unmarshal external host file info JSON, %s", err.Error()))
			return nil, err
		}

		fileInfo := &FileInfo{DID: did, FromExternalHost: true}

		name, ok := jsonMap["name"].(string)
		if ok {
			fileInfo.Filename = name
		}

		size, ok := jsonMap["size"].(float64)
		if ok {
			fileInfo.Filesize = uint64(size)
		}

		// The below code indexes deeper into the DRS API response to obtain access information.
		// Because we unmarshalled the JSON without specifying a fixed structure, we test
		// the type of each nested value before indexing deeper into the value.
		// The result is verbose.
		accessMethods, ok := jsonMap["access_methods"].([](interface{}))
		accessMethodsMap, ok := accessMethods[0].(map[string]interface{})
		accessMethod := ""
		accessURL := ""
		if ok {
			accessMethodTest := accessMethodsMap["type"]
			accessMethod, ok = accessMethodTest.(string)

			accessURLsTest := accessMethodsMap["access_url"]
			accessURLs, ok := accessURLsTest.(map[string]interface{})

			if ok {
				accessURLTest, ok := accessURLs["url"].(string)
				if ok {
					accessURL = accessURLTest
				}
			}
		}

		if len(fileInfo.Filename) < 1 && len(accessURL) > 0 {
			arg := []string{accessURL}
			val, ok := getFilePathFromURL(arg)

			if ok && len(val) > 0 {
				filename := strings.Join(val, "_")
				fileInfo.Filename = filename
			}
		}

		if accessMethod == "s3" {
			uri := drsRequestURL + "/access/s3"
			fileInfo.URLs = []string{uri}
		} else {
			FuseLog(fmt.Sprintf("Found unrecognized access_method in DRS response: %s", accessMethod))
		}

		didToFileInfo[did] = fileInfo
		FuseLog(fmt.Sprintf("\nINFO: fileInfo, %v", fileInfo))
	}
	return didToFileInfo, nil
}

func (fs *Gen3Fuse) GetFileNamesAndSizes() (didToFileInfo map[string]*FileInfo, err error) {
	indexdRequestURL := fs.gen3FuseConfig.Hostname + fs.gen3FuseConfig.IndexdBulkFileInfoPath
	var DIDsWithIndexdInfo []string
	var DIDsWithFileInfoFromExternalHosts []string
	didToFileInfo = make(map[string]*FileInfo, 0)
	FuseLog(fmt.Sprintf("Getting %v records", len(fs.DIDs)))
	for i := 0; i < len(fs.DIDs); i += 1000 {
		last := i + 1000
		if len(fs.DIDs) < last {
			last = len(fs.DIDs)
		}
		DIDsWithIndexdInfo = []string{}

		for _, x := range fs.DIDs[i:last] {
			if _, ok := fs.DIDsToCommonsHostnames[x]; ok {
				FuseLog(fmt.Sprintf("Added %#v to externals\n", x))
				DIDsWithFileInfoFromExternalHosts = append(DIDsWithFileInfoFromExternalHosts, x)
			} else {
				FuseLog(fmt.Sprintf("Added %#v to indexd list\n", x))
				DIDsWithIndexdInfo = append(DIDsWithIndexdInfo, "\""+x+"\"")
			}
		}

		if len(DIDsWithIndexdInfo) > 0 {
			postData := "[ " + strings.Join(DIDsWithIndexdInfo, ",") + " ]"

			FuseLog(fmt.Sprintf("POST %v with %v records from window %v - %v", indexdRequestURL, len(DIDsWithIndexdInfo), i, last))

			// Decent timeout because there might be lots of files to list
			timeout := time.Duration(60 * time.Second)
			client := http.Client{
				Timeout: timeout,
			}

			req, err := http.NewRequest("POST", indexdRequestURL, bytes.NewBuffer([]byte(postData)))
			req.Header.Set("Content-Type", "application/json")

			if err != nil {
				FuseLog(err.Error())
				return nil, err
			}
			resp, err := client.Do(req)

			if err != nil {
				FuseLog(err.Error())
				return nil, err
			}

			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return nil, fs.HandleIndexdError(resp)

			}
			bodyBytes, _ := ioutil.ReadAll(resp.Body)
			bodyString := string(bodyBytes)

			didToFileInfoList := make([]*FileInfo, 0)
			json.Unmarshal([]byte(bodyString), &didToFileInfoList)

			for _, fileInfo := range didToFileInfoList {
				didToFileInfo[fileInfo.DID] = fileInfo
			}
		}

		if len(DIDsWithFileInfoFromExternalHosts) > 0 {
			// Now get the DRS file infos
			// drsFileInfos := fs.GetExternalHostFileInfos(didsWithExternalInfo)
			didToFileInfo, err = fs.GetExternalHostFileInfos(DIDsWithFileInfoFromExternalHosts, didToFileInfo)

			if err != nil {
				FuseLog(fmt.Sprintf("Error: failed to retrieve external host file infos. %v ", err))
				return nil, err
			}

			FuseLog(fmt.Sprintf("\nnew and updated didToFileInfo: %#v", didToFileInfo))
		}
	}
	return didToFileInfo, nil
}

func FetchContentsAtURL(presignedUrl string, offset int64, size int64, fullsize int64) (byteContents []byte, err error) {

	// Huge timeout because we're about to download a file
	timeout := time.Duration(500 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	req, _ := http.NewRequest("GET", presignedUrl, nil)
	last := offset + size
	if last > fullsize {
		last = fullsize
	}
	if last == offset {
		return []byte{}, nil
	}
	if !(offset == 0 && size == 0) {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, last))
	}
	resp, err := client.Do(req)
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		FuseLog(bodyString)
		return nil, &APIError{resp.StatusCode, presignedUrl}
	}

	if err != nil {
		FuseLog(err.Error())
		return byteContents, err
	}
	defer resp.Body.Close()

	//var structStr string = fmt.Sprintf("%#v", resp.Header["Content-Length"])
	//FuseLog("\n Get size: " + structStr + "\n")

	byteContents, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		FuseLog("Error reading file at " + presignedUrl)
		return byteContents, err
	}

	return byteContents, err
}

func FuseLog(message string) {

	// Log messages to stdout too
	fmt.Println(message)

	file, err := os.OpenFile(LogFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0664)
	if err != nil {
		return
	}
	defer file.Close()

	fmt.Fprintf(file, message+"\n")
}
