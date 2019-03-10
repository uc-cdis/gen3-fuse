package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
	"net/http"
	"net/url"
	"strconv"
	"bytes"
	"strings"
)

type Gen3Fuse struct {
	fuseutil.NotImplementedFileSystem

	accessToken string

	DIDs []string

	inodes map[fuseops.InodeID]inodeInfo

	gen3FuseConfig *Gen3FuseConfig
}

type manifestRecord struct {
	ObjectId  string  `json:"object_id"`
	SubjectId string  `json:"subject_id"`
	Uuid       string  `json:"uuid"`
}

type IndexdResponse struct {
	Filename string `json:"file_name"`
	Filesize uint64 `json:"size"`
	DID string `json:"did"`
	URLs []string `json:"urls"`
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

		var structStr string = fmt.Sprintf("%#v", didToFileInfo)
		FuseLog("\n Indexd response: " + structStr + "\n")
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

	// For files, the DID
	DID string

	// For files, the presigned URL
	presignedUrl string
}

func getFileNameFromURL(inputURL string) (result string, ok bool) {
	u, err := url.Parse(inputURL)
	
	if err != nil {
		FuseLog(fmt.Sprintf("Error parsing out the filename from this URL %s: %s", inputURL, err))
		return "", false
	}

	filenameWithSlashes := u.Path
	filename := strings.Replace(filenameWithSlashes, "/", "", -1)
	return filename, true
}

func InitializeInodes(didToFileInfo map[string]*IndexdResponse) map[fuseops.InodeID]inodeInfo {
	/*
		Create a file system with a fixed structure described by the manifest
		If you're trying to read this code and understand it, maybe check out the hello world FUSE sample first:
		https://github.com/jacobsa/fuse/blob/master/samples/hellofs/hello_fs.go
	*/

	const (
		rootInode fuseops.InodeID = fuseops.RootInodeID + iota
		exportedFilesInode
	)
	var inodes = map[fuseops.InodeID]inodeInfo{
		// root inode
		rootInode: inodeInfo{
			attributes: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  0555 | os.ModeDir,
			},
			dir: true,
			Children: []fuseutil.Dirent{
				fuseutil.Dirent{
					Offset: 1,
					Inode:  exportedFilesInode,
					Name:   "exported_files",
					Type:   fuseutil.DT_Directory,
				},
			},
		},
	}

	// Create an inode for each imaginary file
	var filesInManifest = []fuseutil.Dirent{}
	var inodeID fuseops.InodeID = fuseops.RootInodeID + 2
	
	k := 0
	for did, fileInfo := range didToFileInfo {
		filename := fileInfo.Filename

		if fileInfo.Filename == "" && len(fileInfo.URLs) > 0 {
			// Try to get the filename from the first URL
			res, ok := getFileNameFromURL(fileInfo.URLs[0])
			if ok {
				filename = res
			}
		}

		if filename == "" { 
			filename = did
		}

		if filename == "" {
			FuseLog(fmt.Sprintf("Indexd record %s does not seem to have a file associated with it; ignoring it.", did))
			continue
		}
		
		var dirEntry = fuseutil.Dirent{
			Offset: fuseops.DirOffset(k + 1),
			Inode:  inodeID,
			Name:   filename,
			Type:   fuseutil.DT_File,
		}
		filesInManifest = append(filesInManifest, dirEntry)

		// Make a new inode for this manifest DID
		inodes[inodeID] = inodeInfo{
			attributes: fuseops.InodeAttributes{
				Nlink: 1,
				Mode:  0444,
				Size:  fileInfo.Filesize,
			},
			DID: did,
		}

		inodeID += 1
		k += 1
		FuseLog("Added an inode entry to exported_files/")
	}

	// inode for directory that contains the imaginary files described in the manifest
	inodes[exportedFilesInode] = inodeInfo{
		attributes: fuseops.InodeAttributes{
			Nlink: 1,
			Mode:  0555 | os.ModeDir,
		},
		dir:      true,
		Children: filesInManifest,
	}

	return inodes
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

	FuseLog("inside OpenFile")

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
	FuseLog(info.presignedUrl)
	fileBody, err := FetchContentsAtURL(info.presignedUrl)
	
	if err != nil {
		FuseLog("Error fetching file contents: " + err.Error())
		err = fuse.ENOENT
		return err
	}
	
	reader := strings.NewReader(string(fileBody))

	// op.Offset: The offset within the file at which to read.
	// op.Dst: The destination buffer, whose length gives the size of the read.

	op.BytesRead, err = reader.ReadAt(op.Dst, op.Offset)
	FuseLog(string(op.Dst))
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
	requestUrl := fmt.Sprintf(fs.gen3FuseConfig.Hostname + fs.gen3FuseConfig.FencePresignedURLPath, DID)
	FuseLog("GET " + requestUrl)

	req, err := http.NewRequest("GET", requestUrl, nil)
	req.Header.Add("Authorization", "Bearer "+ fs.accessToken)
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
		DIDsWithQuotes = append(DIDsWithQuotes, "\"" + x + "\"")
	}

	postData := "[ " + strings.Join(DIDsWithQuotes, ",") + " ]"

	FuseLog("POST " + requestUrl + "\n" + postData)

	req, err := http.NewRequest("POST", requestUrl, bytes.NewBuffer(  []byte(postData)   ))
	req.Header.Add("Authorization", "Bearer "+ fs.accessToken)
	req.Header.Set("Content-Type", "application/json")

	if err != nil {
		FuseLog(err.Error())
		return nil, err
	}
	resp, err = myClient.Do(req)

	if err != nil {
		FuseLog(err.Error())
		return nil, err
	}

	return resp, nil
}

func (fs *Gen3Fuse) FileInfoFromIndexdResponse(resp *http.Response) (didToFileInfo map[string]*IndexdResponse, err error) { 
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)

	FuseLog("Indexd response: " + bodyString)

	didToFileInfoList := make([]IndexdResponse, 0)
	json.Unmarshal([]byte(bodyString), &didToFileInfoList)

	didToFileInfo = make(map[string]*IndexdResponse, 0)
	for i := 0; i < len(didToFileInfoList); i++ {
		didToFileInfo[didToFileInfoList[i].DID] = &IndexdResponse{ 
			Filesize : didToFileInfoList[i].Filesize, 
			Filename: didToFileInfoList[i].Filename,
			DID: didToFileInfoList[i].DID,
			URLs: didToFileInfoList[i].URLs,
		}
	}

	return didToFileInfo, nil
}

func FetchContentsAtURL(presignedUrl string) (byteContents []byte, err error) {
	FuseLog("\nGET " + presignedUrl)

	resp, err := http.Get(presignedUrl)
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