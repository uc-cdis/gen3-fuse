package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

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

	inodes map[fuseops.InodeID]*inodeInfo

	gen3FuseConfig *Gen3FuseConfig
}

type manifestRecord struct {
	ObjectId  string `json:"object_id"`
	SubjectId string `json:"subject_id"`
	Uuid      string `json:"uuid"`
}

type IndexdResponse struct {
	Filename string   `json:"file_name"`
	Filesize uint64   `json:"size"`
	DID      string   `json:"did"`
	URLs     []string `json:"urls"`
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

	var didToFileInfo map[string]*IndexdResponse
	if len(fs.DIDs) == 0 {
		FuseLog("Warning: no DIDs were obtained from the manifest.")
	} else {
		didToFileInfo, err = fs.GetFileNamesAndSizes()
		if err != nil {
			return nil, err
		}

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
}

func getFilePathFromURL(urls []string) (result []string, ok bool) {
	validURL := ""
	for _, uri := range urls {
		parsed, err := url.Parse(uri)
		if err == nil && (parsed.Scheme == "s3" || parsed.Scheme == "gcs") {
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

func InitializeInodes(didToFileInfo map[string]*IndexdResponse) map[fuseops.InodeID]*inodeInfo {
	/*
		Create a file system with a fixed structure described by the manifest
		If you're trying to read this code and understand it, maybe check out the hello world FUSE sample first:
		https://github.com/jacobsa/fuse/blob/master/samples/hellofs/hello_fs.go
	*/
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
		createInode(inodes, byIDDir, inodeID, did, did, fileInfo.Filesize)
		inodeID++

		// Try to get the filename from the first URL
		paths, ok := getFilePathFromURL(fileInfo.URLs)

		if !ok {
			continue
		}
		filename := paths[len(paths)-1]
		createInode(inodes, byFilenameDir, inodeID, filename, did, fileInfo.Filesize)
		inodeID++
		if len(paths) == 1 {
			createInode(inodes, byFilepathDir, inodeID, filename, did, fileInfo.Filesize)
			inodeID++
			continue
		}

		for i := 0; i <= len(paths)-1; i++ {
			filename := paths[i]
			fullpath := strings.Join(paths[0:i+1], "/")
			parentpath := strings.Join(paths[0:i], "/")
			// this folder is already created in another guid lookup
			_, ok := inodeIDMap[fullpath]
			if ok {
				continue
			}
			if i == 0 {
				// root directory, it's parent is byFilepathDir
				createInode(inodes, byFilepathDir, inodeID, filename, "", 0)
			} else if i == len(paths)-1 {
				// leaf file
				parentNode := inodeIDMap[parentpath]
				createInode(inodes, parentNode, inodeID, filename, did, fileInfo.Filesize)
			} else {
				// intermediate directory
				parentNode := inodeIDMap[parentpath]
				createInode(inodes, parentNode, inodeID, filename, "", 0)
			}
			inodeIDMap[fullpath] = inodeID
			inodeID++
		}

	}
	return inodes
}

func createInode(inodes map[fuseops.InodeID]*inodeInfo, parentID fuseops.InodeID, inodeID fuseops.InodeID, filename string, did string, filesize uint64) {
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
			Name: filename,
			DID:  did,
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

func (fs *Gen3Fuse) LoadDIDsFromManifest(manifestFilePath string) (err error) {
	FuseLog("Inside LoadDIDsFromManifest")
	b, err := ioutil.ReadFile(manifestFilePath)
	if err != nil {
		return err
	}

	manifestJSON := make([]manifestRecord, 0)
	json.Unmarshal(b, &manifestJSON)

	for i := 0; i < len(manifestJSON); i++ {
		fs.DIDs = append(fs.DIDs, manifestJSON[i].ObjectId)
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
		presignedUrl, err := fs.GetPresignedURL(info.DID)
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
	FuseLog(fmt.Sprintf("get %v with offset %v size %v", info.presignedUrl, op.Offset, size))
	fileBody, err := FetchContentsAtURL(info.presignedUrl, op.Offset, size)

	if err != nil {
		FuseLog("Error fetching file contents: " + err.Error())
		err = fuse.ENOENT
		return err
	}

	reader := strings.NewReader(string(fileBody))

	// op.Offset: The offset within the file at which to read.
	// op.Dst: The destination buffer, whose length gives the size of the read.

	op.BytesRead, err = reader.ReadAt(op.Dst, 0)
	FuseLog(strconv.Itoa(op.BytesRead))
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

func (fs *Gen3Fuse) GetFileNamesAndSizes() (didToFileInfo map[string]*IndexdResponse, err error) {
	resp, err := fs.FetchBulkSizeResponseFromIndexd()
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return fs.FileInfoFromIndexdResponse(resp)
	} else if resp.StatusCode == 401 {
		// get a new access token, try again just one more time
		FuseLog("Got 401, retrying...")
		fs.accessToken, err = GetAccessToken(fs.gen3FuseConfig)
		if err != nil {
			return nil, err
		}
		respRetry, err := fs.FetchBulkSizeResponseFromIndexd()
		if err != nil {
			return nil, err
		}

		defer respRetry.Body.Close()

		if resp.StatusCode == 200 {
			return fs.FileInfoFromIndexdResponse(resp)
		}
	}

	return nil, fs.HandleIndexdError(resp)
}

func (fs *Gen3Fuse) GetPresignedURL(DID string) (presignedUrl string, err error) {
	FuseLog("Inside GetPresignedURL")
	resp, err := fs.FetchURLResponseFromFence(DID)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return fs.URLFromSuccessResponseFromFence(resp), nil
	} else if resp.StatusCode == 401 {
		// get a new access token, try again just one more time
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
			return fs.URLFromSuccessResponseFromFence(resp), nil
		}
	}

	return "", fs.HandleFenceError(resp)
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
	requestUrl := fmt.Sprintf(fs.gen3FuseConfig.Hostname+fs.gen3FuseConfig.FencePresignedURLPath, DID)
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

func (fs *Gen3Fuse) URLFromSuccessResponseFromFence(resp *http.Response) (presignedUrl string) {
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	FuseLog(bodyString)

	var urlResponse presignedURLResponse
	json.Unmarshal([]byte(bodyString), &urlResponse)
	return urlResponse.Url
}

func (fs *Gen3Fuse) FetchBulkSizeResponseFromIndexd() (resp *http.Response, err error) {
	requestUrl := fs.gen3FuseConfig.Hostname + fs.gen3FuseConfig.IndexdBulkFileInfoPath

	var DIDsWithQuotes []string
	for _, x := range fs.DIDs {
		DIDsWithQuotes = append(DIDsWithQuotes, "\""+x+"\"")
	}

	postData := "[ " + strings.Join(DIDsWithQuotes, ",") + " ]"

	FuseLog("POST " + requestUrl) // + "\n" + postData)

	// Decent timeout because there might be lots of files to list
	timeout := time.Duration(60 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequest("POST", requestUrl, bytes.NewBuffer([]byte(postData)))
	req.Header.Add("Authorization", "Bearer "+fs.accessToken)
	req.Header.Set("Content-Type", "application/json")

	if err != nil {
		FuseLog(err.Error())
		return nil, err
	}
	resp, err = client.Do(req)

	if err != nil {
		FuseLog(err.Error())
		return nil, err
	}

	return resp, nil
}

func (fs *Gen3Fuse) FileInfoFromIndexdResponse(resp *http.Response) (didToFileInfo map[string]*IndexdResponse, err error) {
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)

	// FuseLog("Indexd response: " + bodyString)

	didToFileInfoList := make([]IndexdResponse, 0)
	json.Unmarshal([]byte(bodyString), &didToFileInfoList)

	didToFileInfo = make(map[string]*IndexdResponse, 0)
	for i := 0; i < len(didToFileInfoList); i++ {
		didToFileInfo[didToFileInfoList[i].DID] = &IndexdResponse{
			Filesize: didToFileInfoList[i].Filesize,
			Filename: didToFileInfoList[i].Filename,
			DID:      didToFileInfoList[i].DID,
			URLs:     didToFileInfoList[i].URLs,
		}
	}

	return didToFileInfo, nil
}

func FetchContentsAtURL(presignedUrl string, offset int64, size int64) (byteContents []byte, err error) {
	FuseLog("\nGET " + presignedUrl)

	// Huge timeout because we're about to download a file
	timeout := time.Duration(500 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	req, _ := http.NewRequest("GET", presignedUrl, nil)
	if !(offset == 0 && size == 0) {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+size))
	}
	resp, err := client.Do(req)

	// resp, err := http.Get(presignedUrl)
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
	file, err := os.OpenFile(LogFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return
	}
	defer file.Close()

	fmt.Fprintf(file, message+"\n")
}
