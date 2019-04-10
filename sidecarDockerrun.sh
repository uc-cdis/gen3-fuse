TOKEN_JSON=`curl  http://workspace-token-service.default.svc.cluster.local/token/`
TOKEN=`jq '.token' $TOKEN_JSON`
MANIFEST_LIST=`curl https://emalinowskiv1.planx-pla.net/manifests/ -H "Authorization: Bearer $TOKEN"`
MANIFEST=`jq '.manifests[-1].filename' $MANIFEST_LIST`
curl https://emalinowskiv1.planx-pla.net/manifests/file/$MANIFEST > ~/manifest.json
gen3fuse ~/fuse-config.yaml ~/manifest.json /data/fuse emalinowskiv1 https://emalinowskiv1.planx-pla.net https://emalinowskiv1.planx-pla.net 
#sleep 5h
