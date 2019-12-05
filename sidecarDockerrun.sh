#!/bin/bash

cleanup() {
  killall gen3-fuse
  cd /data
  for f in `ls -d`
  do
    echo a $f b `pwd`
    fusermount -uz $f
    rm -rf $f
  done

  exit 0
}

sed -i "s/LogFilePath: \"fuse_log.txt\"/LogFilePath: \"\/data\/manifest-sync-status.log\"/g" ~/fuse-config.yaml
trap cleanup SIGTERM
while true; do
    if [ $(df /data/man* | sed '1d' | wc -l) -lt 7 ]; then # remove header line, and also consider the existence of manifest snyc log
        resp=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        if [[ $(echo $resp | jq -r '.error') =~ 'log' ]]; then
            echo get new token
            TOKEN_JSON=`curl  http://workspace-token-service.$NAMESPACE/token/  2>/dev/null | jq -r '.token'`
            resp=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        fi
        MANIFESTEXT=`echo $resp | jq --raw-output .manifests[-1].filename`
        if [ "$MANIFESTEXT" == "null" ]; then
            # user doens't have any manifest
            continue
        fi
        FILENAME=`echo $MANIFESTEXT | sed 's/\.[^.]*$//'`
        if [ ! -d /data/$FILENAME ]; then
            echo mount manifest $MANIFESTEXT
            curl https://$HOSTNAME/manifests/file/$MANIFESTEXT -H "Authorization: Bearer $TOKEN_JSON"  > /manifest.json
            manifest_contents=`cat /manifest.json`
            echo 'manifest contents: $manifest_contents'
            gen3-fuse -config=/fuse-config.yaml -manifest=/manifest.json -mount-point=/data/$FILENAME -hostname=https://$HOSTNAME -wtsURL=http://workspace-token-service.$NAMESPACE >/proc/1/fd/1 2>/proc/1/fd/2
        fi
    else
        OLDDIR=`df /data/manifest* |  grep manifest | cut -d'/' -f 3 | head -n 1`
        echo unmount old manifest $OLDDIR
        fusermount -u /data/$OLDDIR; rm -rf /data/$OLDDIR
    fi
    sleep 5
done
