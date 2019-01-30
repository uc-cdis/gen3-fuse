# gen3-fuse

gen3-fuse is a FUSE implementation built on [jacobsa/fuse](https://github.com/jacobsa/fuse) that serves files hosted in s3 as if they were stored locally. 

We use the [Workspace Token Service](https://github.com/uc-cdis/workspace-token-service) to login to [Fence](https://github.com/uc-cdis/fence), which provides temporary signed URLs to access files stored in s3. This allows the end-user to natively browse files stored in s3 without access to AWS credentials.

## Overview

The gen3fuse program is initialized with a manifest file from a Gen3 Data Commons and a mount point. The Workspace Token Service authenticates the worker node by checking Kubernetes annotations on the machine. Access tokens can then be retrieved from the WTS for use with Fence to obtain presigned URLs.

A manifest.json file contains a list of pointers to data objects stored in s3. It may serve as a cohort of subjects that a researcher
would like to perform analysis on in her workspace.

    [
        {
            "uuid": "fbd5b74e-6789-4f42-b88f-f75e72777f5d",
            "subject_id": "10",
            "object_id" : "7612",
        },
        ...
    ]

This program manipulates file system [inodes](http://www.linfo.org/inode.html) so that the mount point is populated with a directory containing "files" listed in the manifest. When a specific file is opened for reading (OpenFile), the temporary signed URL for that DID is obtained from Fence and stored in memory. When the file is read (ReadFile) the contents at the presigned URL is fetched and delivered to the user. 


## Setup


Clone this repository into your $GOPATH, which might be `~/go/src` or `/usr/local/go.` 
If you're a Mac user, you may need to install [OSXFUSE](https://github.com/osxfuse/osxfuse/releases) before you run the setup below.

Before running this program, you'll need the Workspace Token Service running at some URL, as Gen3Fuse uses that service to authenticate.
If you'd prefer to mock this service for testing purposes, see the testing section below.

You can choose where errors are logged in config.yaml. By default they are logged to ./fuse_log.txt. To setup and mount a directory:
    
    # Clone
    git clone https://github.com/uc-cdis/gen3-fuse.git
    cd gen3-fuse/

    # Switch to this branch
    git checkout initial_fuse

    # Grab submodules
    git submodule init
    git submodule update
    
    # Install dependencies   
    go get -u golang.org/x/crypto/ssh/terminal
    go get -u golang.org/x/sys/unix
    go get -u golang.org/x/net/context
    go get -u gopkg.in/yaml.v2
    go get -u github.com/gorilla/mux

    # Mount a directory (note the fields you need to fill)
	go build main.go
    ./main <workspace-token-service base url> <base url of commons> <path to manifest file> <directory to mount>
    
To safely unmount Gen3Fuse for any reason:
    
    umount -f <mounted directory>

## Running tests and mocking the Workspace Token Service

If you want to test this program locally, you can use the resources in the tests/ folder to mock the Workspace Token Service.

First obtain an api key from your commons, at the /identity endpoint.

Then, in one terminal window, run:

    go build tests/mock_wts_server.go
    ./mock_wts_server <commons url> <api key>

The mock workspace token service should now be running at localhost:8001.

If you've already performed the Gen3Fuse setup instructions listed above, you can just run the following in a separate window:

    go test tests/gen3fuse_test.go

