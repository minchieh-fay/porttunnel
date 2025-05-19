set -e

MYPWD=$(cd `dirname $0`;pwd)


tag="hub.hitry.io/hitry/tools:porttunnel.0590963"

ARMHOST="${ARMHOST:-10.35.148.59:2375}"
AMDHOST="${AMDHOST:-10.35.146.7:2375}"

echo "ARMHOST=$ARMHOST"

export DOCKER_HOST=$AMDHOST

	

#export DOCKER_CONTENT_TRUST=0

echo "创建并推送manifest"

ssh root@10.35.146.7 docker  manifest create --amend $tag ${tag}_amd64
ssh root@10.35.146.7 docker manifest annotate $tag ${tag}_amd64 --arch amd64
ssh root@10.35.146.7 docker manifest create --amend $tag ${tag}_arm64
ssh root@10.35.146.7 docker manifest annotate $tag ${tag}_arm64 --arch arm64
ssh root@10.35.146.7 docker manifest push --purge $tag


# docker manifest create --amend hub.hitry.io/hitry/tools:porttunnel.0590963 hub.hitry.io/hitry/tools:porttunnel.0590963_amd64