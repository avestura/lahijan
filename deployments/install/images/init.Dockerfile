# lahijan-init: the one-shot jobs of the self-contained install.
#   - `gen-secrets`  creates the passwords and keys on first start
#   - `seed-iam`     writes the object-storage admin identity before S3 starts
#
# Build context: the repository root.
FROM alpine:3.22
RUN apk add --no-cache curl
COPY deployments/install/images/lahijan-env.sh /usr/local/bin/lahijan-env
COPY deployments/install/images/gen-secrets.sh /usr/local/bin/gen-secrets
COPY deployments/install/images/seed-iam.sh /usr/local/bin/seed-iam
RUN chmod 755 /usr/local/bin/lahijan-env /usr/local/bin/gen-secrets /usr/local/bin/seed-iam
ENTRYPOINT ["/usr/local/bin/lahijan-env"]
CMD ["gen-secrets"]
