package gen3fuse

import (
	"gen3-fuse/internal"
)

// expose Gen3Fuse functions from the internal package
var (
	ConnectWithFence = internal.ConnectWithFence
	NewGen3Fuse      = internal.NewGen3Fuse
	InitializeApp    = internal.InitializeApp
	Mount            = internal.Mount
	Unmount          = internal.Unmount
)

type (
	Gen3Fuse       = internal.Gen3Fuse
	Gen3FuseConfig = internal.Gen3FuseConfig
)
