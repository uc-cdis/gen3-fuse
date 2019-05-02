#!/bin/bash
sed -i "s/LogFilePath: \"fuse_log.txt\"/LogFilePath: \"\/data\/manifest-sync-status.log\"/g" ~/fuse-config.yaml
while true; do
    if [ $(df /data/man* | wc -l) -lt 5 ]; then
        resp=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        if [[ $(echo $resp | jq -r '.error') =~ 'log' ]]; then
            echo get new token
            TOKEN_JSON=`curl  http://workspace-token-service.$NAMESPACE/token/  2>/dev/null | jq -r '.token'`
            resp=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        fi
        MANIFESTEXT=`echo $resp | jq --raw-output .manifests[-1].filename`
        if [ $MANIFESTEXT = 'null' ]; then
            # user doens't have any manifest
            continue
        fi
        FILENAME=`echo $MANIFESTEXT | sed 's/\.[^.]*$//'`
        if [ ! -d /data/$FILENAME ]; then
            echo mount manifest $MANIFESTEXT
            curl https://$HOSTNAME/manifests/file/$MANIFESTEXT -H "Authorization: Bearer $TOKEN_JSON"  > ~/manifest.json
            gen3-fuse ~/fuse-config.yaml ~/manifest.json /data/$FILENAME https://$HOSTNAME http://workspace-token-service.$NAMESPACE
        fi
    else
        OLDDIR=`df /data/manifest* |  grep manifest | cut -d'/' -f 3 | head -n 1`
        echo unmount old manifest $OLDDIR
        fusermount -u /data/$OLDDIR; rm -rf /data/$OLDDIR
    fi
    sleep 5
done