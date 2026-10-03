#!/bin/sh
# Seeds the SeaweedFS admin identity before the S3 gateway starts.
#
# `weed s3` reads its identities from the filer document /etc/iam/identity.json
# and treats an empty set as "authentication off". Lahijan re-publishes that
# document (admin plus every credential it mints) on boot; this only makes sure
# the document exists first, so S3 is never open, even briefly.
set -eu

doc=http://seaweed-filer:8888/etc/iam/identity.json
if curl -sf -o /dev/null "$doc"; then
	echo "iam document present"
	exit 0
fi
printf '{"identities":[{"name":"lahijan_admin","credentials":[{"accessKey":"%s","secretKey":"%s"}],"actions":["Admin","Read","Write","List","Tagging"]}]}' \
	"$SEAWEEDFS_S3_ACCESS_KEY" "$SEAWEEDFS_S3_SECRET_KEY" |
	curl -sf -X PUT -H "Content-Type: application/json" --data-binary @- "$doc"
echo "iam document seeded"
