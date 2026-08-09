#!/bin/sh
# set up environment for both development and testing
# This should be the fist script to run in the test directory

# Note that it is ASSUMED that before all these test directory scripts are used
# 0) Go language package is installed
# 1) gotgt project directory is created
#       and project is cloned with git per project README.md
#       build and build verification can be done with gotgt-dameon.sh
# 2) libiscsi directory is created
#       git clone and build can be done with this shell script
# 
[ ! -n "$LIBISCSIPATH" ] &&  echo "Need LIBISCSIPATH definition" && exit 1 
[ ! -n "$GOTGTPATH" ] && echo "Need GOTGTPATH definition" && exit 2
[ ! -d $LIBISCSIPATH ] && echo "Need $LIBISCSIPATH directory" && exit 3
[ ! -d $GOTGTPATH ] && echo "Need $GOTGTPATH directory" && exit 4
#exit 0

## This system update could induce errors on updating ubuntu content
sudo apt-get update

#Make sure that these packages are installed 
#NOTE: The scripts are tested on Ubuntu, please feel free to generalize to
#      other platforms.

sudo apt-get install automake
sudo apt-get install autogen autoconf libtool

##### Get Go dependency tools/fixes package
# https://github.com/tools/godep
#    Needed once only and again as required.
cd $GOTGTPATH
go get github.com/tools/godep

#
# libc unit testing package, this is required by libiscsi package
sudo apt-get install -y libcunit1 libcunit1-doc libcunit1-dev
#

##### Get libiscsi package and build for testing purposes
cd $LIBISCSIPATH
git clone https://github.com/gostor/libiscsi .
export ISCSITEST=yes
./autogen.sh
# TO-DO/TO-RESOLVE
# autoreconf: configure.ac: not using Gettext
./configure 2>&1 >/dev/null
make 2>&1 >/dev/null

# TO-DO/TO-RESOLVE
# ar: `u' modifier ignored since `D' is the default (see `U')

# check expected binaries for successful build
[ ! -x ./test-tool/iscsi-test-cu ] && echo "./test-tool/iscsi-test-cu not built" && exit 5
[ ! -x ./utils/iscsi-ls ] && echo "./utils/iscsi-ls not built" && exit 6
[ ! -x ./utils/iscsi-inq ] && echo "./utils/iscsi-inq not built" && exit 7
[ ! -x ./utils/iscsi-readcapacity16 ] && echo "./utils/iscsi-readcapacity16 not built" && exit 8

##### Get open-iscsi project package bits
# open-iscsi project
# https://github.com/open-iscsi/open-iscsi
#
# Install open-iscsi package and watch for kernel build
uname -a
echo
sudo apt-get install -y open-iscsi
#    Need to do this only once and re-do it as required.
# For example, /boot initrd.img-4.10.19 kernel version
## Remember the new kernel version for future boot if open-iscsi testing is to
##   to performed.
# TO-DO/TO-RESOLVE
#   cp: cannot stat '/etc/iscsi/initiatorname.iscsi': No such file or directory

## sanity check expected files for successful installation
[ ! -x /sbin/iscsiadm ] && echo "/sbin/iscsiadm not built" && exit 9

exit 0
