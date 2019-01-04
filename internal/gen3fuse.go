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
	"strconv"
	"strings"
)

type Gen3Fuse struct {
	fuseutil.NotImplementedFileSystem
	accessToken string

	DIDs []manifestRecord

	inodes map[fuseops.InodeID]inodeInfo

	gen3FuseConfig *Gen3FuseConfig
}

type manifestRecord struct {
	Object_Id  string
	Subject_Id string
	Uuid       string
}

var LogFilePath string = "fuse_log.txt"

func NewGen3Fuse(ctx context.Context, gen3FuseConfig *Gen3FuseConfig, manifestURL string) (fs *Gen3Fuse, err error) {
	LogFilePath = gen3FuseConfig.LogFilePath

	// Authenticate with Workspace Token Service. TODO: check for failure
	err = ConnectWithFence(gen3FuseConfig)
	if err != nil {
		return nil, err
	}

	accessToken := GetAccessToken(gen3FuseConfig)

	fs = &Gen3Fuse{
		accessToken:    accessToken,
		gen3FuseConfig: gen3FuseConfig,
	}

	var struct_str string = fmt.Sprintf("%#v", gen3FuseConfig)
	FuseLog("\nLoaded Gen3FuseConfig: " + struct_str + "\n")

	b, err := ioutil.ReadFile(manifestURL)
	if err != nil {
		return nil, err
	}

	manifestJSON := make([]manifestRecord, 0)
	json.Unmarshal(b, &manifestJSON)

	fmt.Println("manifestJSON: ", manifestJSON)

	fs.DIDs = append(fs.DIDs, manifestJSON...)

	fs.inodes = InitializeInodes(fs.DIDs)

	return fs, nil
}

type inodeInfo struct {
	attributes fuseops.InodeAttributes

	// File or directory?
	dir bool

	// For directories, children.
	children []fuseutil.Dirent

	// For files, the DID
	DID string

	// For files, the presigned URL
	presignedUrl string

	// For files, the file body (this is ok right?)
	fileBody string // TODO: adjust type to match response body
}

func InitializeInodes(DIDs []manifestRecord) map[fuseops.InodeID]inodeInfo {
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
			children: []fuseutil.Dirent{
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
	var numFiles = len(DIDs)

	for i := 0; i < numFiles; i++ {
		// TODO: make portable between different commons' node type things
		var filename = DIDs[i].Uuid

		var dirEntry = fuseutil.Dirent{
			Offset: fuseops.DirOffset(i + 1),
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
				Size:  6, // An arbitrary non-zero size to ensure that OpenFile is called
			},
			DID: DIDs[i].Uuid,
			fileBody: "", // Contents are only stored in memory during the OpenFile call, and cleared out after the ReadFile call
		}

		inodeID += 1
	}

	// inode for directory that contains the imaginary files described in the manifest
	inodes[exportedFilesInode] = inodeInfo{
		attributes: fuseops.InodeAttributes{
			Nlink: 1,
			Mode:  0555 | os.ModeDir,
		},
		dir:      true,
		children: filesInManifest,
	}

	return inodes
}

func findChildInode(
	name string,
	children []fuseutil.Dirent) (inode fuseops.InodeID, err error) {

	for _, e := range children {
		if e.Name == name {
			inode = e.Inode
			return
		}
	}

	err = fuse.ENOENT
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
	childInode, err := findChildInode(op.Name, parentInfo.children)
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
		err = fuse.ENOENT
		return
	}

	if !info.dir {
		err = fuse.EIO
		return
	}

	entries := info.children

	// Grab the range of interest.
	if op.Offset > fuseops.DirOffset(len(entries)) {
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
		} else {
			info.presignedUrl = presignedUrl
			var fileBody string = FetchContentsAtURL(info.presignedUrl)
			var fileSize = uint64(len(fileBody))
			info.attributes.Size = fileSize
			info.fileBody = fileBody
			fs.inodes[op.Inode] = info
		}
	}

	fs.inodes[op.Inode] = info

	return
}

func (fs *Gen3Fuse) ReadFile(
	ctx context.Context,
	op *fuseops.ReadFileOp) (err error) {

	info, ok := fs.inodes[op.Inode]
	if !ok {
		err = fuse.ENOENT
		return
	}

	reader := strings.NewReader(info.fileBody)

	op.BytesRead, err = reader.ReadAt(op.Dst, op.Offset)

	// Clear out the file contents; we don't want all these files stored in memory
	info.fileBody = ""
	fs.inodes[op.Inode] = info

	// Special case: FUSE doesn't expect us to return io.EOF.
	if err == io.EOF {
		err = nil
	}

	return
}

type presignedURLResponse struct {
	Url string
}

func (fs *Gen3Fuse) GetPresignedURL(DID string) (presignedUrl string, err error) {
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
		fs.accessToken = GetAccessToken(fs.gen3FuseConfig)
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
	FuseLog("Fetching presigned URL failed! \nThe Response code was " + strconv.Itoa(resp.StatusCode)) // +  ". Access token: " + fs.accessToken)
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	FuseLog(bodyString)
	return fuse.EIO
}

func (fs *Gen3Fuse) URLFromSuccessResponseFromFence(resp *http.Response) (presignedUrl string) {
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	FuseLog(bodyString)

	var urlResponse presignedURLResponse
	json.Unmarshal([]byte(bodyString), &urlResponse)
	return urlResponse.Url
}

func (fs *Gen3Fuse) FetchURLResponseFromFence(DID string) (response *http.Response, err error) {
	request_url := fmt.Sprintf(fs.gen3FuseConfig.Hostname+fs.gen3FuseConfig.FencePath+fs.gen3FuseConfig.FencePresignedURLPath, DID)
	FuseLog("GET " + request_url)

	req, err := http.NewRequest("GET", request_url, nil)
	req.Header.Add("Authorization", "Bearer "+fs.accessToken)

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

func FetchContentsAtURL(presignedUrl string) (string_data string) {
	FuseLog("\nGET " + presignedUrl)
	resp, err := http.Get(presignedUrl)
	if err != nil {
		FuseLog(err.Error())
		return ""
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		FuseLog("Error reading file at " + presignedUrl)
		return ""
	}

	return string(data)
}

func FuseLog(message string) {
	file, err := os.OpenFile(LogFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return
	}
	defer file.Close()

	fmt.Fprintf(file, message+"\n")
}
