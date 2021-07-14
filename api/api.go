package gen3fuse

import (
	"github.com/uc-cdis/gen3-fuse/internal"
)

// expose Gen3Fuse functions from the internal package
var (
	NewGen3Fuse               = internal.NewGen3Fuse
	NewGen3FuseConfigFromYaml = internal.NewGen3FuseConfigFromYaml
	InitializeApp             = internal.InitializeApp
	Mount                     = internal.Mount
	Unmount                   = internal.Unmount
)

type (
	Gen3Fuse       = internal.Gen3Fuse
	Gen3FuseConfig = internal.Gen3FuseConfig
	FileInfo = internal.FileInfo
)
