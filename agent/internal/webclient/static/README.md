The browser client embedded into the agent (see ../webclient.go).

Everything else in this directory is a build artifact, gitignored:
`agent/scripts/stage_webclient.sh` builds `client/web` and copies the files
the agent serves here before `go build`. Without them the agent still builds
and answers `/` with a note that the web client isn't built in.
