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

# $ENGINE_WORKSPACE
while [[ ! -f /$ENGINE_WORKSPACE/done ]]; do
  echo "not done"
done

fusermount -u /$ENGINE_WORKSPACE
