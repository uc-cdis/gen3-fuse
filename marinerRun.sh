#!/bin/bash

gen3-fuse \ # okay
-config=~/fuse-config.yaml \ # okay
-manifest=~/manifest.json \ # TODO
-mount-point=/$COMMONS_DATA \ # okay
-hostname=https://$HOSTNAME \ # okay
-wtsURL=http://workspace-token-service.$GEN3_NAMESPACE \ # okay
>/proc/1/fd/1 2>/proc/1/fd/2
