#!/bin/sh
# Build and Kick off the gotgt daemon

# Assuming the development and testing environment was setup and all 
#   required binaries are up-to-date

# For reading compatibility between these scripts and the automated .yml file
#    TRAVIS_BUILD_DIR=$GOTGTPATH
cd $GOTGTPATH
./autogen.sh
./configure 
## Normally, above steps are done only once and repeated only as required

# Back up through gotgt, gostor, github.com, and src (4 ..'s)
# to avoid GOPATH must be absolute problem.
export GOPATH=`pwd`/../../../..
make

###### run some formatting check and unit testing
export GOPATH=`pwd`/Godeps/_workspace/:$GOPATH
./hack/verify-gofmt.sh
## TO-DO/TO-RESOLVE supply some test files to do more unit testing
go test -v ./pkg/...

### create target json configuration file for testing
### create a flat file for target backend for testing
[ ! -d ${HOME}/.gotgt ] && mkdir ${HOME}/.gotgt
#
echo '{"storages":[{"deviceID":1000,"path":"file:/var/tmp/disk.img","online":true}],' > ${HOME}/.gotgt/config.json
echo '"iscsiportals":[{"id":0,"portal":"127.0.0.1:3260"}],' >> ${HOME}/.gotgt/config.json
echo '"iscsitargets":{"iqn.2016-09.com.gotgt.gostor:example_tgt_0":{"tpgts":{"1":[0]},"luns":{"0":1000}}}}' >> ${HOME}/.gotgt/config.json

[ ! -f /var/tmp/disk.img ] && touch /var/tmp/disk.img
dd if=/dev/zero of=/var/tmp/disk.img bs=1024 count=102400

## kick off the target library daemon for testing purposes
## Note that the grep command would be always in the ps command output
[ `ps -ef | grep "gotgt daemon" | wc -l ` -gt 1 ] && killall gotgt

#./gotgt --help
#./gotgt daemon --help

./gotgt daemon --log debug 1>/dev/null 2>&1 &

# Or watching the daemon
#./gotgt daemon --log debug 1> debug.daemon 2>&1 &
# tail -f debug.daemon

#
sleep 2
ps -ef | grep "gotgt daemon"

##
exit 0
