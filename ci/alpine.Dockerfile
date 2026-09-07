ARG ALPINE=alpine:3.24.1

FROM $ALPINE

RUN apk --no-cache --no-progress add tzdata ca-certificates

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/calendar /

USER 65534

ENTRYPOINT [ "/calendar" ]
