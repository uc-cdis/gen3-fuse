#!/bin/bash

# maybe use a more vertically scalable method
# than passing manifest via environment variable
# Q. What if the manifest is hella big?
echo $GEN3FUSE_MANIFEST > ~/manifest.json

gen3-fuse \ # okay
-config=~/fuse-config.yaml \ # okay
-manifest=~/manifest.json \ # okay
-mount-point=/$COMMONS_DATA \ # okay
-hostname=https://$HOSTNAME \ # okay
-wtsURL=http://workspace-token-service.$GEN3_NAMESPACE \ # okay
>/proc/1/fd/1 2>/proc/1/fd/2

echo "here is the mounted directory:"
ls -R /$COMMONS_DATA

# $ENGINE_WORKSPACE
while [[ ! -f /$ENGINE_WORKSPACE/done ]]; do
  echo "not done"
done

echo "done, unmounting gen3fuse"

fusermount -u /$COMMONS_DATA

echo "gen3fuse exited successfully"
