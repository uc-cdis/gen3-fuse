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
    # First loop:
    # get the number of manifests in /data dir. If there are less
    # than 6 (external manifests don't count), fetch new manifests.
    if [ $(df /data/man* | sed '1d' | wc -l) -lt 7 ]; then # remove header line, and also consider the existence of manifest sync log
        resp=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        if [[ $(echo $resp | jq -r '.error') =~ 'log' ]]; then
            echo get new token
            TOKEN_JSON=`curl http://workspace-token-service.$NAMESPACE/token/ 2>/dev/null | jq -r '.token'`
            resp=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        fi

        # get the name of the most recent manifest
        MANIFESTEXT=`echo $resp | jq --raw-output .manifests[-1].filename`
        if [ "$MANIFESTEXT" == "null" ]; then
            # user doens't have any manifest
            continue
        fi
        FILENAME=`echo $MANIFESTEXT | sed 's/\.[^.]*$//'`

        # gen3-fuse mounts the files in /data/ dir
        if [ ! -d /data/$FILENAME ]; then
            echo mount manifest $MANIFESTEXT
            curl https://$HOSTNAME/manifests/file/$MANIFESTEXT -H "Authorization: Bearer $TOKEN_JSON" > /manifest.json
            gen3-fuse -config=/fuse-config.yaml -manifest=/manifest.json -mount-point=/data/$FILENAME -hostname=https://$HOSTNAME -wtsURL=http://workspace-token-service.$NAMESPACE >/proc/1/fd/1 2>/proc/1/fd/2
        fi

    # if there are 6 manifests or more, delete the oldest one
    else
        OLDDIR=`df /data/manifest* | grep manifest | cut -d'/' -f 3 | head -n 1`
        echo unmount old manifest $OLDDIR
        fusermount -u /data/$OLDDIR; rm -rf /data/$OLDDIR
    fi

    # Second loop:
    # for each hostname dir in /external_manifests
    for dir in /external_manifests/*; do
        if [ ! -f $dir/manifest.json ] || [ ! -f $dir/credentials.json ]; then
            continue
        fi

        # generate filename from hostname and manifest date of creation
        MANIFEST_HOSTNAME=`basename $dir`
        MANIFEST_DATE=`date -r $dir/manifest.json "+%Y-%m-%dT%H-%M-%S"`
        # echo $dir: hostname is $MANIFEST_HOSTNAME, date is $MANIFEST_DATE
        FILENAME=$MANIFEST_HOSTNAME-manifest-$MANIFEST_DATE

        # gen3-fuse mounts the files in /data/ dir
        if [ ! -d /data/$FILENAME ]; then
            echo mount manifest $FILENAME
            API_KEY=`cat $dir/credentials.json | jq -r .api_key`
            gen3-fuse -config=/fuse-config.yaml -manifest=$dir/manifest.json -mount-point=/data/$FILENAME -hostname=https://$MANIFEST_HOSTNAME -api-key=$API_KEY
        fi
    done

    sleep 5
done
