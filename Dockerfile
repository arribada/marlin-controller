FROM goreleaser/goreleaser-cross:v1.23.3@sha256:cc69304a8817dde675f0afe551c380545d5f34444946004961116ff394437a6f AS builder

RUN apt-get update
# TODO: clean up. loopwav still needs portaudio
# RUN apt-get install -y git portaudio19-dev

ARG GOPROXY=direct
ENV GOPATH=/home/marlin
RUN go install github.com/gokrazy/tools/cmd/gok@6bec690fe5cd
WORKDIR /home/marlin

WORKDIR /home/marlin/src/github.com/arribada/marlin-controller/syslogd
COPY ./syslogd .

# WORKDIR /home/marlin/src/github.com/arribada/marlin-controller/kernel.rpi
# COPY ./kernel.rpi .

WORKDIR /home/marlin/src/github.com/arribada/marlin-controller/mike
COPY ./mike .
RUN make build

ARG instance=doesnotexist
RUN echo "building for "$instance
WORKDIR /home/marlin/src/github.com/arribada/marlin-controller/gokrazy/$instance
# lowkey check that $instance is a valid one.
# only copy our instance (and not the whole gokrazy dir), so that
# we stay cached if another instance config changes
COPY ./gokrazy/$instance .
COPY ./gokrazy/secrets .
# if we want to do by host: it would be in /root/.config/gokrazy/hosts/thehostname
WORKDIR /root/.config/gokrazy
COPY ./gokrazy/secrets/http-password.txt .

# keep it as last rule, so we are in right place for other stages
WORKDIR /home/marlin/src/github.com/arribada/marlin-controller
COPY ./Makefile .

FROM builder AS gengaf
ARG instance=doesnotexist
RUN --mount=type=cache,target=/home/marlin/pkg INSTANCE=$instance make gaf2

# To generate full.gaf, and then copy it to host with:
# docker build --target gaf --output=. .
FROM scratch AS gaf
ARG instance=doesnotexist
COPY --from=gengaf /tmp/$instance.gaf /

FROM scratch AS builddir
ARG instance=doesnotexist
COPY --from=gengaf /home/marlin/src/github.com/arribada/marlin-controller/gokrazy/$instance/builddir /

FROM builder AS forceupdate
ARG instance=doesnotexist
# only works at build time with non-ts IP
RUN INSTANCE=$instance make forceupdate2
# tailscale hosts are not resolved, so only works for now if update has been configured by IP
# ENTRYPOINT ["make", "forceupdate2"]

FROM scratch AS artifact
COPY --from=builder /tmp/freeze /
