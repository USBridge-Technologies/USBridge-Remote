#!/bin/bash

set -e
set -x

sudo apt-get install -y python3-pip

# ceph-deploy and ceph
CEPH_RELEASE=quincy

sudo pip install ceph-deploy
ceph-deploy install --release ${CEPH_RELEASE} `hostname`
ceph-deploy pkg --install librados-dev `hostname`
ceph-deploy pkg --install librbd-dev `hostname`
ceph-deploy pkg --install libcephfs-dev `hostname`
