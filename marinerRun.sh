#!/bin/bash

cleanup() {
  killall gen3-fuse
  cd /$COMMONS_DATA
  for f in `ls -d`
  do
    echo a $f b `pwd`
    fusermount -uz $f
    rm -rf $f
  done

  exit 0
}

# maybe use a more vertically scalable method
# than passing manifest via environment variable
# Q. What if the manifest is very big?
echo "writing manifest"
echo $GEN3FUSE_MANIFEST
echo $GEN3FUSE_MANIFEST > /manifest.json

echo "running gen3-fuse.."
/gen3-fuse \
-config=/config.yaml \
-manifest=/manifest.json \
-mount-point=/$COMMONS_DATA/data \
-hostname=https://$HOSTNAME \
-wtsURL=http://workspace-token-service.$GEN3_NAMESPACE >/proc/1/fd/1 2>/proc/1/fd/2

echo "here is the mounted directory:"
ls -R /$COMMONS_DATA

trap cleanup SIGTERM
if [ $MARINER_COMPONENT == "engine" ]; then
  echo "waiting for engine to finish.."
  while [[ ! -f /$ENGINE_WORKSPACE/workflowRuns/$RUN_ID/done ]]; do
    :
  done
else
  echo "waiting for task to finish.."
  while [[ ! -f $TOOL_WORKING_DIR\done ]]; do
    :
  done
fi

echo "done, unmounting gen3fuse"

cleanup
# fusermount -u  /$COMMONS_DATA

echo "gen3fuse exited successfully"
