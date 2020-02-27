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

sed -i "s/LogFilePath: \"fuse_log.txt\"/LogFilePath: \"\/data\/_manifest-sync-status.log\"/g" ~/fuse-config.yaml
trap cleanup SIGTERM

while true; do

    EXTERNAL_OIDC=`curl http://workspace-token-service.$NAMESPACE/external_oidc/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null | jq -r '.providers'`

    # list of IDPs to get manifests from.
    # only select IDPs the user is logged into
    IDPS=( "default" )
    BASE_URLS=( "https://$HOSTNAME" )
    for row in $(echo "${EXTERNAL_OIDC}" | jq -r '.[] | @base64'); do
        _jq() {
            echo ${row} | base64 -d | jq -r ${1}
        }
        # if user is connected, add IDP info to the lists
        if [ `_jq '.refresh_token_expiration'` != "null" ]; then
            IDPS+=( `_jq '.idp'` )
            BASE_URLS+=( `_jq '.base_url'` )
        fi
    done
    echo $IDPS
    echo $BASE_URLS

    for i in "${!IDPS[@]}"; do  # TODO simplify, only loop IDPs once
        IDP=${IDPS[$i]}
        BASE_URL=${BASE_URLS[$i]}
        echo "getting manifests for IDP '$IDP' at $BASE_URL"

        # one folder per external IDP, except for "default"
        IDP_DATA_PATH="/data"
        if [ $IDP != "default" ]; then
            IDP_DATA_PATH="/data/blah"  # TODO
        fi

        resp=`curl $BASE_URL/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`

        # if access token is expired, get a new one and try again
        if [[ $(echo $resp | jq -r '.error') =~ 'log' ]]; then
            echo "get new token for IDP $IDP"
            TOKEN_JSON=`curl http://workspace-token-service.$NAMESPACE/token/?idp=$IDP 2>/dev/null | jq -r '.token'`
            resp=`curl $BASE_URL/manifests/ -H "Authorization: bearer $TOKEN_JSON" 2>/dev/null`
        fi

        # get the name of the most recent manifest
        MANIFEST_NAME=`echo $resp | jq --raw-output .manifests[-1].filename`
        if [ "$MANIFEST_NAME" == "null" ]; then
            # user doens't have any manifest
            sleep 10
            continue
        fi

        FILENAME=`echo $MANIFEST_NAME | sed 's/\.[^.]*$//'`

        # gen3-fuse mounts the files in /data/ dir
        if [ ! -d $IDP_DATA_PATH/$FILENAME ]; then
            echo mount manifest at $IDP_DATA_PATH/$MANIFEST_NAME
            curl $BASE_URL/manifests/file/$MANIFEST_NAME -H "Authorization: Bearer $TOKEN_JSON" > /manifest.json
            gen3-fuse -config=/fuse-config.yaml -manifest=/manifest.json -mount-point=/data/$FILENAME -hostname=https://$HOSTNAME -wtsURL=http://workspace-token-service.$NAMESPACE -idp=$IDP >/proc/1/fd/1 2>/proc/1/fd/2
        fi

        # get the number of existing manifests. If there are more than 5,
        # delete the oldest one.
        if [ $(df $IDP_DATA_PATH/manifest* | sed '1d' | wc -l) -gt 5 ]; then # remove header line
            OLDDIR=`df $IDP_DATA_PATH/manifest* | grep manifest | cut -d'/' -f 3 | head -n 1`
            echo unmount old manifest $OLDDIR
            fusermount -u /data/$OLDDIR; rm -rf /data/$OLDDIR
        fi
    done
    sleep 10
done
