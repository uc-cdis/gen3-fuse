#!/bin/bash

# Only the most recent manifest is mounted.
# If new manifests are added while the workspace is running,
# only keep the N latest manifests for each IDP:
MAX_MANIFESTS=5

cleanup() {
  killall gen3-fuse
  cd /data
  for f in $(ls -d)
  do
    echo a $f b $(pwd)
    fusermount -uz $f
    rm -rf $f
  done

  exit 0
}

# _jq ${ENCODED_JSON} ${KEY} returns JSON.KEY
_jq() {
    (base64 -d | jq -r ${2}) <<< ${1}
}

sed -i "s/LogFilePath: \"fuse_log.txt\"/LogFilePath: \"\/data\/_manifest-sync-status.log\"/g" ~/fuse-config.yaml
trap cleanup SIGTERM

declare -A TOKEN_JSON  # requires Bash 4
TOKEN_JSON['default']=$(curl http://workspace-token-service.$NAMESPACE/token/?idp=default 2>/dev/null | jq -r '.token')

while true; do

    # get the list of IDPs the current user is logged into
    EXTERNAL_OIDC=$(curl http://workspace-token-service.$NAMESPACE/external_oidc/?unexpired=true -H "Authorization: bearer ${TOKEN_JSON['default']}" 2>/dev/null | jq -r '.providers')
    IDPS=( "default" )
    BASE_URLS=( "https://$HOSTNAME" )
    for ROW in $(jq -r '.[] | @base64' <<< ${EXTERNAL_OIDC}); do
        IDPS+=( $(_jq ${ROW} .idp) )
        BASE_URLS+=( $(_jq ${ROW} .base_url) )
    done

    for i in "${!IDPS[@]}"; do
        IDP=${IDPS[$i]}
        BASE_URL=${BASE_URLS[$i]}
        echo "Getting manifests for IDP '$IDP' at $BASE_URL"

        resp=$(curl $BASE_URL/manifests/ -H "Authorization: bearer ${TOKEN_JSON[$IDP]}" 2>/dev/null)

        # if access token is expired, get a new one and try again
        if [[ $(jq -r '.error' <<< $resp) =~ 'log' ]]; then
            echo "get new token for IDP '$IDP'"
            TOKEN_JSON[$IDP]=$(curl http://workspace-token-service.$NAMESPACE/token/?idp=$IDP 2>/dev/null | jq -r '.token')
            resp=$(curl $BASE_URL/manifests/ -H "Authorization: bearer ${TOKEN_JSON[$IDP]}" 2>/dev/null)
        fi

        #############################################################################
        ### This code block executes the new PFB handoff flow for cohort analysis. ##
        #############################################################################

        # get the GUID of the most recent cohort
        GUID=$(jq --raw-output .manifests.cohorts[-1].filename <<< $resp)
        if [[ $? != 0 ]]; then
            echo "Manifests endpoints at $BASE_URL/manifests/ did not return JSON. Maybe it's not configured?"
            continue
        fi
        if [[ "$GUID" == "null" ]]; then
            # user doesn't have any manifest
            continue
        fi
        
        # Now retrieve the contents of the file with this GUID
        echo "New GUID: $GUID"
        fence_presigned_url_endpoint="$BASE_URL/user/data/download/$GUID"
        status_code=$(curl --write-out '%{http_code}' -H "Authorization: bearer ${TOKEN_JSON[$IDP]}" --silent --output /dev/null $fence_presigned_url_endpoint)
        presigned_url_to_cohort_PFB=$(curl $fence_presigned_url_endpoint -H "Authorization: bearer ${TOKEN_JSON[$IDP]}" 2>/dev/null)

        if [[ $status_code != 200 ]]; then
            echo "Request to Fence endpoint at $BASE_URL/user/data/download/$GUID failed with status code $status_code."
            echo "Error message: $presigned_url_to_cohort_PFB"
            continue
        fi
        
        echo "Retrieved presigned URL to the cohort: $presigned_url_to_cohort_PFB"

        cohort_PFB_file_contents=$(curl $presigned_url_to_cohort_PFB 2>/dev/null)
        if [[ $? != 0 ]]; then
            echo "Request to presigned URL for cohort PFB at $presigned_url_to_cohort_PFB failed."
            continue
        fi

        echo "Cohort PFB contents: $cohort_PFB_file_contents"

        # one folder per IDP
        DOMAIN=$(awk -F/ '{print $3}' <<< $BASE_URL)
        IDP_DATA_PATH="/data/$DOMAIN"
        
        local_filepath_for_cohort_PFB="pd/data/$BASE_URL/cohort-$GUID.avro"

        echo "$cohort_PFB_file_contents" > $local_filepath_for_cohort_PFB

        

        # Next steps: use pyPFB to parse DIDs from the PFB and mount them using gen3-fuse


        #############################################################################
        ### This code block uses manifest.json's for mounting. It will eventually ###
        ### be deprecated in favor of the new PFB handoff flow. #####################
        #############################################################################

        # get the name of the most recent manifest
        MANIFEST_NAME=$(jq --raw-output .manifests.manifests[-1].filename <<< $resp)
        if [[ $? != 0 ]]; then
            echo "Manifests endpoints at $BASE_URL/manifests/ did not return JSON. Maybe it's not configured?"
            continue
        fi
        if [[ "$MANIFEST_NAME" == "null" ]]; then
            # user doesn't have any manifest
            continue
        fi
        FILENAME=$(sed 's/\.[^.]*$//' <<< $MANIFEST_NAME)

        # one folder per IDP
        DOMAIN=$(awk -F/ '{print $3}' <<< $BASE_URL)
        IDP_DATA_PATH="/data/$DOMAIN"

        # gen3-fuse mounts the files in /data/<hostname> dir
        if [ ! -d $IDP_DATA_PATH/$FILENAME ]; then
            echo mount manifest at $IDP_DATA_PATH/$MANIFEST_NAME
            mkdir -p $IDP_DATA_PATH
            curl $BASE_URL/manifests/file/$MANIFEST_NAME -H "Authorization: Bearer ${TOKEN_JSON[$IDP]}" > /manifest.json
            gen3-fuse -config=/fuse-config.yaml -manifest=/manifest.json -mount-point=$IDP_DATA_PATH/$FILENAME -hostname=$BASE_URL -wtsURL=http://workspace-token-service.$NAMESPACE -wtsIDP=$IDP >/proc/1/fd/1 2>/proc/1/fd/2
        fi

        # get the number of existing manifests. If there are more than
        # MAX_MANIFESTS, delete the oldest one.
        if [ $(df $IDP_DATA_PATH/manifest* | sed '1d' | wc -l) -gt $MAX_MANIFESTS ]; then # remove header line
            OLDDIR=$(df $IDP_DATA_PATH/manifest* | grep manifest | cut -d'/' -f 4 | head -n 1)
            echo unmount old manifest $OLDDIR
            fusermount -u $IDP_DATA_PATH/$OLDDIR; rm -rf $IDP_DATA_PATH/$OLDDIR
        fi
    done
    sleep 10
done
