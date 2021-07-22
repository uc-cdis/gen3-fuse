#!/bin/bash

# The below commands allow for enhanced logging.
# All output from this script will be captured in the file at /data/$HOSTNAME/fuse-sidecar-logs.out
# Note that this log file will be visible to the workspace user.
mkdir "/data/$HOSTNAME/"
touch "/data/$HOSTNAME/fuse-sidecar-logs.out"
exec 3>&1 4>&2
trap 'exec 2>&4 1>&3' 0 1 2 3
exec 1>"/data/$HOSTNAME/fuse-sidecar-logs.out" 2>&1

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
WTS_URL="http://workspace-token-service.$NAMESPACE"
WTS_URL=${WTS_OVERRIDE_URL:-"$WTS_URL"}
echo "WTS_URL: $WTS_URL"
WTS_STATUS=$(curl -s -o /dev/null -I -w "%{http_code}" $WTS_URL/_status)
echo "WTS STATUS: $WTS_STATUS"
while [[ ( "$WTS_STATUS" -ne 200 ) ]]; do
    echo "Unable to reach WTS at '$WTS_URL', or WTS is not healthy. Wait 15s and retry."
    sleep 15
    WTS_STATUS=$(curl -s -o /dev/null -I -w "%{http_code}" $WTS_URL/_status)
done
declare -a curlArgs
if [ ! -z $WTS_OVERRIDE_URL ]; then
    curlArgs=('-H' "Authorization: bearer ${ACCESS_TOKEN}")
fi
default_token=$(curl $WTS_URL/token/?idp=default "${curlArgs[@]}" 2>/dev/null | jq -r '.token')
while [[ "$default_token" = null ]]; do
    echo "Unable to get token from '$WTS_URL', or WTS is not healthy. Wait 15s and retry."
    echo $default_token
    sleep 15
    default_token=$(curl $WTS_URL/token/?idp=default "${curlArgs[@]}" 2>/dev/null | jq -r '.token')
done
declare -A TOKEN_JSON  # requires Bash 4
TOKEN_JSON['default']=$default_token
run_sidecar() {
    while true; do
        token=$(curl $WTS_URL/token/?idp=default "${curlArgs[@]}"  2>/dev/null | jq -r '.token')
        if [[ ! -z "$token" ]]; then
            echo "got new token $token"
            TOKEN_JSON['default']=$token
        fi

        echo "got token from WTS: ${TOKEN_JSON['default']}"
        wts_response=$(curl $WTS_URL/token/?idp=default "${curlArgs[@]}" )
        echo "wts response: $wts_response"

        # get the list of IDPs the current user is logged into
        EXTERNAL_OIDC=$(curl $WTS_URL/external_oidc/?unexpired=true -H "Authorization: bearer ${TOKEN_JSON['default']}" 2>/dev/null | jq -r '.providers')
        IDPS=( "default" )
        BASE_URLS=( "https://$HOSTNAME" )

        for ROW in $(jq -r '.[] | @base64' <<< ${EXTERNAL_OIDC}); do
            IDPS+=( $(_jq ${ROW} .idp) )
            BASE_URLS+=( $(_jq ${ROW} .base_url) )
        done
        echo "WTS IDPs: ${IDPS[@]}"

        for i in "${!IDPS[@]}"; do
            IDP=${IDPS[$i]}
            BASE_URL=${BASE_URLS[$i]}

            # one folder per IDP
            DOMAIN=$(awk -F/ '{print $3}' <<< $BASE_URL)
            IDP_DATA_PATH="/data/$DOMAIN"

            if [ ! -d $IDP_DATA_PATH ]; then
                mkdir -p $IDP_DATA_PATH
            fi

            echo "Checking for new exports with IDP '$IDP' at $BASE_URL"

            check_for_new_manifests "$IDP_DATA_PATH" "$NAMESPACE" "$IDP" "$BASE_URL" "$TOKEN_JSON"

            check_for_new_PFB_GUIDs "$IDP_DATA_PATH" "$NAMESPACE" "$IDP" "$BASE_URL" "$TOKEN_JSON"

            # get the number of existing mounted manifests. If there are more than
            # MAX_MANIFESTS, delete the oldest one.
            if [ $(df $IDP_DATA_PATH/manifest* | sed '1d' | wc -l) -gt $MAX_MANIFESTS ]; then # remove header line
                OLDDIR=$(df $IDP_DATA_PATH/manifest* | grep manifest | cut -d'/' -f 4 | head -n 1)
                echo unmount old manifest $OLDDIR
                fusermount -u $IDP_DATA_PATH/$OLDDIR; rm -rf $IDP_DATA_PATH/$OLDDIR
            fi
        done
        sleep 10
    done
}

query_manifest_service() {
    # This function populates a return value in a variable called $resp
    URL=$1

    resp=$(curl $URL -H "Authorization: bearer ${TOKEN_JSON[$IDP]}")
    echo "manifest service response: $resp"

    # if access token is expired, get a new one and try again
    if [[ $(jq -r '.error' <<< $resp) =~ 'log' ]]; then
        echo "Getting new token for IDP '$IDP'"
        TOKEN_JSON[$IDP]=$(curl $WTS_URL/token/?idp=$IDP -H "Authorization: bearer ${TOKEN_JSON['default']}" | jq -r '.token')
        resp=$(curl $URL -H "Authorization: bearer ${TOKEN_JSON[$IDP]}")
        echo "response from the manifest service $URL: $resp"
    fi
}

mount_manifest() {
    MANIFEST_NAME=$1
    IDP_DATA_PATH=$2
    NAMESPACE=$3
    IDP=$4
    BASE_URL=$5
    TOKEN_JSON=$6
    PATH_TO_MANIFEST=$7

    MOUNT_NAME=$(sed 's/\.[^.]*$//' <<< $MANIFEST_NAME)

    # If the manifest is not present locally, we download it
    if [[ $PATH_TO_MANIFEST == "" ]]; then
        curl $BASE_URL/manifests/file/$MANIFEST_NAME -H "Authorization: Bearer ${TOKEN_JSON[$IDP]}" > /manifest.json
        PATH_TO_MANIFEST=/manifest.json
    fi

    # gen3-fuse mounts the files in /data/<hostname> dir
    if [ ! -d $IDP_DATA_PATH/$MOUNT_NAME ]; then
        echo "Mounting manifest at $IDP_DATA_PATH/$MOUNT_NAME"
        if [ -z $ACCESS_TOKEN ]; then
            if (! gen3-fuse -config=/fuse-config.yaml -manifest=$PATH_TO_MANIFEST -mount-point=$IDP_DATA_PATH/$MOUNT_NAME -hostname=$BASE_URL -wtsURL=$WTS_URL -wtsIDP=$IDP >/proc/1/fd/1 2>/proc/1/fd/2); then
                echo "gen3-fuse failed to mount."
            fi
        else
            if (! gen3-fuse -config=/fuse-config.yaml -manifest=$PATH_TO_MANIFEST -mount-point=$IDP_DATA_PATH/$MOUNT_NAME -hostname=$BASE_URL -wtsURL=$WTS_URL -wtsIDP=$IDP -access-token=$ACCESS_TOKEN >/proc/1/fd/1 2>/proc/1/fd/2); then
                echo "gen3-fuse failed to mount."
            fi
        fi
    fi
}

check_for_new_manifests() {
    IDP_DATA_PATH=$1
    NAMESPACE=$2
    IDP=$3
    BASE_URL=$4
    TOKEN_JSON=$5
    echo "querying manifest service at $BASE_URL/manifests/"
    resp='' # The below function populates this variable
    query_manifest_service $BASE_URL/manifests/


    # get the name of the most recent manifest
    MANIFEST_NAME=$(jq --raw-output .manifests[-1].filename <<< $resp)
    echo "most recent manifest name: $MANIFEST_NAME"
    if [[ $? != 0 ]]; then
        echo "Manifest service endpoint at $BASE_URL/manifests/ did not return JSON. Maybe it's not configured?"
        return
    fi
    if [[ "$MANIFEST_NAME" == "null" ]]; then
        # user doesn't have any manifests
        return
    fi

    mount_manifest "$MANIFEST_NAME" "$IDP_DATA_PATH" "$NAMESPACE" "$IDP" "$BASE_URL" "$TOKEN_JSON" ""
}

check_for_new_PFB_GUIDs() {
    IDP_DATA_PATH=$1
    NAMESPACE=$2
    IDP=$3
    BASE_URL=$4
    TOKEN_JSON=$5

    resp='' # The below function populates this variable
    query_manifest_service $BASE_URL/manifests/cohorts

    # Get the GUID of the most recent cohort
    GUID=$(jq --raw-output .cohorts[-1].filename <<< $resp)
    if [[ $? != 0 ]]; then
        echo "Manifest service endpoint at $BASE_URL/manifests/cohorts/ did not return JSON. Maybe it's not configured?"
        return
    fi
    if [[ "$GUID" == "null" || "$GUID" == "" ]]; then
        # user doesn't have any cohorts
        return
    fi

    # Check if this GUID has been mounted already
    MOUNT_NAME="manifest-$GUID"
    if [[ -d $IDP_DATA_PATH/$MOUNT_NAME ]]; then
        return
    fi

    echo "Got new GUID: $GUID"

    # Now retrieve the contents of the file with this GUID
    fence_presigned_url_endpoint="$BASE_URL/user/data/download/$GUID"
    presigned_url_to_cohort_PFB=$(curl $fence_presigned_url_endpoint -H "Authorization: bearer ${TOKEN_JSON[$IDP]}" 2>/dev/null)

    p_url=$(jq --raw-output .url <<< $presigned_url_to_cohort_PFB)
    if [[ "$p_url" == "null" || "$p_url" == "" || -z $p_url ]]; then
        echo "Request to Fence endpoint at $BASE_URL/user/data/download/$GUID failed."
        echo "Error message: $presigned_url_to_cohort_PFB"
        return
    fi

    local_filepath_for_cohort_PFB="$IDP_DATA_PATH/cohort-$GUID.avro"
    # --create-dirs because GUIDs with prefix contain "/"
    curl $p_url --output $local_filepath_for_cohort_PFB --create-dirs
    if [[ $? != 0 ]]; then
        echo "Request to presigned URL for cohort PFB at $p_url failed."
        return
    fi

    # Next steps: use pyPFB to parse DIDs from the PFB and mount them using gen3-fuse
    PFB_MANIFEST_NAME="manifest-$GUID.json"
    pushd /
    ./pfbToManifest.sh $local_filepath_for_cohort_PFB "$IDP_DATA_PATH/$PFB_MANIFEST_NAME"
    popd
    if [[ $? != 0 ]]; then
        echo "Failed to parse object IDs from $local_filepath_for_cohort_PFB."
        return
    fi

    mount_manifest "$PFB_MANIFEST_NAME" "$IDP_DATA_PATH" "$NAMESPACE" "$IDP" "$BASE_URL" "$TOKEN_JSON" "/$IDP_DATA_PATH/$PFB_MANIFEST_NAME"

    rm "/$IDP_DATA_PATH/$PFB_MANIFEST_NAME"
}

run_sidecar
