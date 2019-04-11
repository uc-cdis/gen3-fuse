#!/bin/sh
while true; do
    TOKEN_JSON=`curl  http://workspace-token-service.$NAMESPACE/token/ | jq -r '.token'`
    sleep 1
    MANIFESTEXT=`curl https://$HOSTNAME/manifests/ -H "Authorization: Bearer $TOKEN_JSON" | jq --raw-output .manifests[-1].filename`
    if [ $MANIFESTEXT = 'null' ]; then
        # user doens't have any manifest
        continue
    fi
    FILENAME=`echo $MANIFESTEXT | sed 's/\.[^.]*$//'`
    if [ ! -d /data/$FILENAME ]; then
        curl https://$HOSTNAME/manifests/file/$MANIFESTEXT -H "Authorization: Bearer $TOKEN_JSON"  > ~/manifest.json
        gen3fuse ~/fuse-config.yaml ~/manifest.json /data/$FILENAME https://$HOSTNAME http://workspace-token-service.$NAMESPACE
    fi
done
