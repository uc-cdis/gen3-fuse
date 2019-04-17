#!/bin/sh
while true; do
    if [ $(df /data/man* | wc -l) -lt 5 ]; then
        TOKEN_JSON=`curl  http://workspace-token-service.$NAMESPACE/token/ | jq -r '.token'`
        MANIFESTEXT=`curl https://$HOSTNAME/manifests/ -H "Authorization: bearer $TOKEN_JSON" | jq --raw-output .manifests[-1].filename`
        if [ $MANIFESTEXT = 'null' ]; then
            # user doens't have any manifest
            continue
        fi
        FILENAME=`echo $MANIFESTEXT | sed 's/\.[^.]*$//'`
        if [ ! -d /data/$FILENAME ]; then
            curl https://$HOSTNAME/manifests/file/$MANIFESTEXT -H "Authorization: Bearer $TOKEN_JSON"  > ~/manifest.json
            gen3fuse ~/fuse-config.yaml ~/manifest.json /data/$FILENAME https://$HOSTNAME http://workspace-token-service.$NAMESPACE
        fi
    else
        OLDDIR=`df /data/manifest* |  grep manifest | cut -d'/' -f 3 | head -n 1`
        fusermount -u /data/$OLDDIR; rm -rf /data/$OLDDIR
    fi
    sleep 5
done