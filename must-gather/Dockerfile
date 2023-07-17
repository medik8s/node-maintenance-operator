FROM quay.io/openshift/origin-must-gather:4.12.0 AS builder

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.8-860
RUN microdnf install tar-6.1.15 rsync-3.2.7 jq-1.6 -y && microdnf clean all -y

# Copy must-gather required binaries
COPY --from=builder /usr/bin/oc /usr/bin/oc

# Save original gather script
COPY --from=builder /usr/bin/gather /usr/bin/gather_original
COPY --from=builder /usr/bin/version /usr/bin/version_original

# Copy our scripts
COPY collection-scripts/* /usr/bin/

ENTRYPOINT [ "/usr/bin/gather" ]
