while [ true ]
do
  TOKEN_JSON=`curl  http://workspace-token-service.default.svc.cluster.local/token/ | jq -r '.token'`
  MANIFESTEXT=`curl https://emalinowskiv1.planx-pla.net/manifests/ -H "Authorization: Bearer $TOKEN_JSON" | jq --raw-output .manifests[-1].filename`
  FILENAME=`echo $MANIFESTEXT | sed 's/\.[^.]*$//'`
  curl https://emalinowskiv1.planx-pla.net/manifests/file/$MANIFESTEXT -H "Authorization: Bearer $TOKEN_JSON"  > ~/manifest.json
  gen3fuse ~/fuse-config.yaml ~/manifest.json /data/$FILENAME https://emalinowskiv1.planx-pla.net http://workspace-token-service.default
  sleep 1m
done
